package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/obs"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/server"
	"github.com/panbotka/kukatko/internal/trash"
	"github.com/panbotka/kukatko/internal/version"
	"github.com/panbotka/kukatko/internal/worker"
)

// sessionCleanupInterval is how often expired sessions and stale rate-limiter
// keys are purged in the background.
const sessionCleanupInterval = time.Hour

// newServeCmd builds the "serve" subcommand, which starts the HTTP server and
// blocks until the process receives SIGINT or SIGTERM, then shuts down
// gracefully.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		Long:  "Start the kukatko HTTP server and serve the API until interrupted (SIGINT/SIGTERM).",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd)
		},
	}
}

// runServe loads the configuration, opens the database (applying migrations),
// wires the auth subsystem and all HTTP API groups plus the background worker,
// and serves until the process receives SIGINT or SIGTERM.
func runServe(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}

	logger, reg, err := initObservability(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err = db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	registerDBPoolMetrics(reg, db)

	authAPI, err := setupAuth(ctx, cmd, cfg, db)
	if err != nil {
		return err
	}

	apis, jobWorker, trashSvc, err := buildServices(cfg, db, authAPI, reg)
	if err != nil {
		return err
	}
	apis, backupSvc, err := appendOpsAPIs(cfg, db, authAPI, apis)
	if err != nil {
		return err
	}

	startWorker(ctx, jobWorker)
	go trashSvc.RunPurge(ctx, trashPurgeInterval)
	if backupSvc != nil {
		go backupSvc.RunSchedule(ctx, cfg.Backup.Schedule)
	}

	apis = append(apis, observabilityOptions(reg, logger)...)

	addr := net.JoinHostPort(cfg.Web.Host, strconv.Itoa(cfg.Web.Port))
	srv := server.New(addr, apis...)
	cmd.Printf("kukatko %s listening on %s\n", version.Get(), srv.Addr())

	if err = srv.Run(ctx); err != nil {
		return fmt.Errorf("running server: %w", err)
	}
	return nil
}

// initObservability configures structured logging (installing the slog default)
// and, when metrics are enabled, constructs the Prometheus registry. It returns
// the logger handle for the access-log middleware and the registry (nil when
// metrics are disabled). It fails only on an invalid log level.
func initObservability(cfg *config.Config) (*slog.Logger, *metrics.Registry, error) {
	logger, err := obs.Setup(os.Stderr, cfg.Log.Level)
	if err != nil {
		return nil, nil, fmt.Errorf("configuring logging: %w", err)
	}
	var reg *metrics.Registry
	if cfg.Metrics.Enabled {
		reg = metrics.New()
	}
	return logger, reg, nil
}

// registerDBPoolMetrics installs the pgx pool collector on reg, a no-op when
// metrics are disabled (reg nil).
func registerDBPoolMetrics(reg *metrics.Registry, db *database.DB) {
	if reg == nil {
		return
	}
	reg.RegisterDBPool(db.Pool())
}

// setupAuth builds the auth API, bootstraps the initial admin account, and
// starts the background session/rate-limiter cleanup goroutines tied to ctx.
func setupAuth(ctx context.Context, cmd *cobra.Command, cfg *config.Config, db *database.DB) (*auth.API, error) {
	authAPI, authSvc := buildAuth(cfg, db)
	if err := runBootstrap(ctx, cmd, authSvc, cfg.Auth); err != nil {
		return nil, err
	}
	go authSvc.RunCleanup(ctx, sessionCleanupInterval)
	go authAPI.RunMaintenance(ctx, sessionCleanupInterval)
	return authAPI, nil
}

// appendOpsAPIs mounts the always-on backup and restore APIs onto apis. The
// backup API self-reports "not configured" and the restore service is nil (503)
// when no destination is set; the returned backup service drives the scheduler
// (nil when not configured).
func appendOpsAPIs(
	cfg *config.Config, db *database.DB, authAPI *auth.API, apis []server.Option,
) ([]server.Option, *backup.Service, error) {
	backupSvc, err := buildBackupService(cfg)
	if err != nil {
		return nil, nil, err
	}
	apis = append(apis, server.WithAPI(buildBackupAPI(backupSvc, authAPI).RegisterRoutes))

	restoreAPI, err := buildRestoreAPI(cfg, db, authAPI)
	if err != nil {
		return nil, nil, err
	}
	apis = append(apis, server.WithAPI(restoreAPI.RegisterRoutes))
	return apis, backupSvc, nil
}

// observabilityOptions builds the server options that install observability: the
// structured access-log middleware always, plus — when metrics are enabled — the
// request-metrics middleware and the GET /metrics handler. Returning options lets
// the serve command compose them with the API route groups.
func observabilityOptions(reg *metrics.Registry, logger *slog.Logger) []server.Option {
	mws := make([]func(http.Handler) http.Handler, 0, 2)
	mws = append(mws, obs.AccessLog(logger))
	if reg == nil {
		return []server.Option{server.WithMiddleware(mws...)}
	}
	mws = append(mws, reg.Middleware(metrics.RouteLabel))
	return []server.Option{
		server.WithMiddleware(mws...),
		server.WithMetricsHandler(reg.Handler()),
	}
}

// buildServices assembles every HTTP API group and the background worker over a
// shared queue store: upload/ingest, photo browse/curation (with embedding-backed
// similar search), face auto-clustering, per-subject face outlier detection, the
// subject (people) catalogue, the album and label catalogue, the maps proxy and
// GeoJSON feed, the admin jobs and processing APIs, and the image_embed and
// face_detect worker handlers. It returns the server options registering those
// routes plus the worker for the serve command to run.
func buildServices(
	cfg *config.Config, db *database.DB, authAPI *auth.API, reg *metrics.Registry,
) ([]server.Option, *worker.Worker, *trash.Service, error) {
	jobStore := jobs.NewStore(db.Pool())
	enqueuer := jobs.NewEnqueuer(jobStore)
	registerJobQueueMetrics(reg, jobStore)
	ingestAPI, err := buildIngest(cfg, db, authAPI, enqueuer, reg)
	if err != nil {
		return nil, nil, nil, err
	}
	embedSvc, vectorStore, embedClient, err := buildEmbedService(cfg, db, enqueuer, reg)
	if err != nil {
		return nil, nil, nil, err
	}
	faceSvc, err := buildFaceService(cfg, db, enqueuer, vectorStore, embedClient)
	if err != nil {
		return nil, nil, nil, err
	}
	matchSvc := buildFaceMatch(cfg, db)
	trashSvc, err := buildTrashService(cfg, db)
	if err != nil {
		return nil, nil, nil, err
	}
	photoAPI, err := buildPhotoAPI(cfg, db, authAPI, vectorStore, embedClient, matchSvc, trashSvc, reg)
	if err != nil {
		return nil, nil, nil, err
	}
	clusterAPI, clusterSvc := buildClusterAPI(cfg, db, authAPI, matchSvc)
	outlierAPI := buildOutlierAPI(db, authAPI)
	peopleAPI := buildPeopleAPI(db, authAPI)
	organizeAPI := buildOrganizeAPI(db, authAPI)
	bulkAPI := buildBulkAPI(cfg, db, authAPI)
	mapsAPI, err := buildMapsAPI(cfg, db, authAPI)
	if err != nil {
		return nil, nil, nil, err
	}
	var importSvc *ppimport.Service
	if importConfigured(cfg) {
		if importSvc, err = buildImportService(cfg, db, enqueuer, reg); err != nil {
			return nil, nil, nil, err
		}
	}
	psMigrate := psMigrateHandlerOrNil(cfg, db, enqueuer, reg)
	jobWorker, jobAPI, processAPI := buildJobs(
		cfg, jobStore, authAPI, embedSvc, faceSvc, clusterSvc, importSvc, psMigrate, reg)
	opts := []server.Option{
		server.WithAPI(authAPI.RegisterRoutes),
		server.WithAPI(ingestAPI.RegisterRoutes),
		server.WithAPI(photoAPI.RegisterRoutes),
		server.WithAPI(clusterAPI.RegisterRoutes),
		server.WithAPI(outlierAPI.RegisterRoutes),
		server.WithAPI(peopleAPI.RegisterRoutes),
		server.WithAPI(organizeAPI.RegisterRoutes),
		server.WithAPI(bulkAPI.RegisterRoutes),
		server.WithAPI(mapsAPI.RegisterRoutes),
		server.WithAPI(jobAPI.RegisterRoutes),
		server.WithAPI(processAPI.RegisterRoutes),
		// Import history and audit log are always mounted (import triggers self-gate).
		server.WithAPI(buildImportAPI(cfg, db, jobStore, authAPI).RegisterRoutes),
		server.WithAPI(buildAuditAPI(db, authAPI).RegisterRoutes),
	}
	return opts, jobWorker, trashSvc, nil
}
