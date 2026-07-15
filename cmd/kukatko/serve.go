package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/obs"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/server"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
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
	logThumbEngine(logger, cfg)

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

	// One tracker shared by the maps proxy (which records every upstream outcome)
	// and the system status (which reports it), so a rejected mapy.com key is
	// visible on the admin dashboard and not only as a grey map.
	mapsHealth := newMapsHealth(cfg)

	apis, bg, err := buildServices(cfg, db, authAPI, reg, mapsHealth)
	if err != nil {
		return err
	}
	apis, backupSvc, err := appendOpsAPIs(cfg, db, authAPI, apis, mapsHealth)
	if err != nil {
		return err
	}

	if err := startBackgroundServices(ctx, cfg, db, bg, backupSvc); err != nil {
		return err
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

// startBackgroundServices builds the optional Wake-on-LAN auto-wake service and
// launches every background goroutine tied to ctx so they stop on shutdown: the
// job worker, the trash retention purge, the auto-wake check loop (inert when
// disabled), and — when configured — the scheduled S3 backup.
func startBackgroundServices(
	ctx context.Context, cfg *config.Config, db *database.DB,
	bg backgroundServices, backupSvc *backup.Service,
) error {
	wakeSvc, err := buildWakeService(cfg, db)
	if err != nil {
		return err
	}
	startWorker(ctx, bg.worker)
	go bg.trash.RunPurge(ctx, trashPurgeInterval)
	go wakeSvc.Run(ctx, wakeCheckInterval)
	if backupSvc != nil {
		go backupSvc.RunSchedule(ctx, cfg.Backup.Schedule)
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

// logThumbEngine logs which thumbnail engine is active. When the vips engine is
// requested it reports whether the vipsthumbnail binary was resolved on PATH; a
// missing binary is a warning because the thumbnailer silently degrades to the
// pure-Go engine, which the operator likely did not intend.
func logThumbEngine(logger *slog.Logger, cfg *config.Config) {
	if !cfg.Thumb.VipsEnabled() {
		logger.Info("thumbnail engine", "engine", config.ThumbEngineGo)
		return
	}
	if thumb.VipsAvailable(cfg.Thumb.VipsBinary) {
		logger.Info("thumbnail engine", "engine", config.ThumbEngineVips, "binary", cfg.Thumb.VipsBinary)
		return
	}
	logger.Warn("thumbnail engine vips requested but vipsthumbnail not found on PATH; using pure-Go",
		"binary", cfg.Thumb.VipsBinary)
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
	mapsHealth *mapy.Health,
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

	systemAPI, err := buildSystemAPI(cfg, db, authAPI, backupSvc, mapsHealth)
	if err != nil {
		return nil, nil, err
	}
	apis = append(apis, server.WithAPI(systemAPI.RegisterRoutes))
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

// backgroundServices bundles the long-running services the serve command starts
// as goroutines tied to the process context: the job worker and the trash
// retention purge. The optional Wake-on-LAN auto-wake is built separately in
// runServe (it needs no API routes).
type backgroundServices struct {
	worker *worker.Worker
	trash  *trash.Service
}

// buildServices assembles every HTTP API group and the background services over a
// shared queue store: upload/ingest, photo browse/curation (with embedding-backed
// similar search), face auto-clustering, per-subject face outlier detection, the
// subject (people) catalogue, the album and label catalogue, the maps proxy and
// GeoJSON feed, the admin jobs and processing APIs, and the image_embed and
// face_detect worker handlers. It returns the server options registering those
// routes plus the background services for the serve command to run.
func buildServices(
	cfg *config.Config, db *database.DB, authAPI *auth.API, reg *metrics.Registry,
	mapsHealth *mapy.Health,
) ([]server.Option, backgroundServices, error) {
	jobStore := jobs.NewStore(db.Pool())
	enqueuer := jobs.NewEnqueuer(jobStore)
	registerJobQueueMetrics(reg, jobStore)
	ingestAPI, err := buildIngest(cfg, db, authAPI, enqueuer, reg)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	embedSvc, vectorStore, embedClient, err := buildEmbedService(cfg, db, enqueuer, reg)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	faceSvc, err := buildFaceService(cfg, db, enqueuer, vectorStore, embedClient)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	matchSvc := buildFaceMatch(cfg, db)
	trashSvc, err := buildTrashService(cfg, db)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	mediaStore, err := newStorage(cfg)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	photoAPI := buildPhotoAPI(cfg, db, authAPI, mediaStore, vectorStore, embedClient, matchSvc, trashSvc, reg)
	clusterAPI, clusterSvc := buildClusterAPI(cfg, db, authAPI, matchSvc)
	mapsAPI, err := buildMapsAPI(cfg, db, authAPI, mapsHealth)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	importSvc, err := buildImportServiceOrNil(cfg, db, enqueuer, reg)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	psMigrate := psMigrateHandlerOrNil(cfg, db, enqueuer, reg)
	jobWorker, jobAPI, processAPI, maintenanceAPI, err := buildJobs(
		cfg, db, jobStore, authAPI, enqueuer, embedSvc, faceSvc, clusterSvc, importSvc, psMigrate, reg)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	opts := slices.Concat([]server.Option{
		server.WithAPI(authAPI.RegisterRoutes),
		server.WithAPI(ingestAPI.RegisterRoutes),
		server.WithAPI(photoAPI.RegisterRoutes),
		server.WithAPI(clusterAPI.RegisterRoutes),
		server.WithAPI(buildBulkAPI(cfg, db, authAPI).RegisterRoutes),
		server.WithAPI(buildCandidatesAPI(cfg, db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildSweepAPI(cfg, db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildDuplicatesAPI(cfg, db, authAPI, vectorStore).RegisterRoutes),
		server.WithAPI(mapsAPI.RegisterRoutes),
		server.WithAPI(jobAPI.RegisterRoutes),
		server.WithAPI(processAPI.RegisterRoutes),
		server.WithAPI(maintenanceAPI.RegisterRoutes),
		// Import history is always mounted (import triggers self-gate).
		server.WithAPI(buildImportAPI(cfg, db, jobStore, authAPI).RegisterRoutes),
	}, readAPIOptions(db, authAPI, mediaStore))
	return opts, backgroundServices{worker: jobWorker, trash: trashSvc}, nil
}

// readAPIOptions builds the server options for the read/curation API groups that
// depend only on the shared pool and the auth guard: per-subject face outliers,
// the people (subject) catalogue, albums and labels, the places browse hierarchy,
// per-user saved searches, the grouped global search and the audit log. Route
// groups mount on distinct paths, so their relative order does not matter.
// Splitting them out keeps buildServices within the function-length limit.
//
// The groups that return photo records take mediaStore, which decides where their
// clients fetch each photo's thumbnail and original.
func readAPIOptions(db *database.DB, authAPI *auth.API, mediaStore storage.Storage) []server.Option {
	return []server.Option{
		server.WithAPI(buildOutlierAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildPeopleAPI(db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildOrganizeAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildFeedbackAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildPlacesAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildSavedSearchAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildGlobalSearchAPI(db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildAuditAPI(db, authAPI).RegisterRoutes),
	}
}

// buildImportServiceOrNil builds the PhotoPrism import service when a source is
// configured, returning (nil, nil) otherwise so the caller mounts import history
// without a trigger.
func buildImportServiceOrNil(
	cfg *config.Config, db *database.DB, enqueuer *jobs.Enqueuer, reg *metrics.Registry,
) (*ppimport.Service, error) {
	if !importConfigured(cfg) {
		return nil, nil //nolint:nilnil // (nil, nil) is the documented "not configured" signal.
	}
	return buildImportService(cfg, db, enqueuer, reg)
}
