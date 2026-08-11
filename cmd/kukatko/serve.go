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
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/obs"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/reachability"
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
	// visible on the admin dashboard and not only as a grey map. The geocode
	// credit budget is shared the same way: the places job spends against it, the
	// status and /metrics report what is left.
	mapsHealth := newMapsHealth(cfg)
	geocodeBudget := newGeocodeBudget(cfg)
	geocodeBudgetMetrics(reg, geocodeBudget)

	apis, bg, err := buildServices(cfg, db, authAPI, reg, mapsHealth, geocodeBudget)
	if err != nil {
		return err
	}
	apis, backupSvc, reachChecker, err := appendOpsAPIs(cfg, db, authAPI, apis, reg, mapsHealth, geocodeBudget)
	if err != nil {
		return err
	}

	if err := startBackgroundServices(ctx, cfg, db, bg, backupSvc, reachChecker); err != nil {
		return err
	}
	// One-off, off the startup path: which image model the sidecar actually serves,
	// and whether its vector width still matches what this binary stores.
	go verifyEmbeddingDim(ctx, cfg, logger)

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
// disabled), the embeddings-reachability probe loop (inert when no embedding URL
// is configured) that backs GET /capabilities, and — when configured — the
// scheduled S3 backup.
func startBackgroundServices(
	ctx context.Context, cfg *config.Config, db *database.DB,
	bg backgroundServices, backupSvc *backup.Service, reachChecker *reachability.Checker,
) error {
	wakeSvc, err := buildWakeService(cfg, db)
	if err != nil {
		return err
	}
	startWorker(ctx, bg.worker)
	go bg.trash.RunPurge(ctx, trashPurgeInterval)
	go wakeSvc.Run(ctx, wakeCheckInterval)
	go reachChecker.Run(ctx, capabilitiesCheckInterval)
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

// appendOpsAPIs mounts the always-on backup, restore, system-status and
// capabilities APIs onto apis. The backup API self-reports "not configured" and
// the restore service is nil (503) when no destination is set; the returned
// backup service drives the scheduler (nil when not configured). It also builds
// the embeddings-reachability checker that both backs GET /capabilities and the
// caller starts as a background loop, returning it for that purpose. It also
// registers the library-content /metrics collector over the system service it
// builds, so the gauges and the dashboard share one aggregation.
func appendOpsAPIs(
	cfg *config.Config, db *database.DB, authAPI *auth.API, apis []server.Option,
	reg *metrics.Registry, mapsHealth *mapy.Health, geocodeBudget *placesjob.WindowBudget,
) ([]server.Option, *backup.Service, *reachability.Checker, error) {
	backupSvc, err := buildBackupService(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	apis = append(apis, server.WithAPI(buildBackupAPI(backupSvc, authAPI).RegisterRoutes))

	restoreAPI, err := buildRestoreAPI(cfg, db, authAPI)
	if err != nil {
		return nil, nil, nil, err
	}
	apis = append(apis, server.WithAPI(restoreAPI.RegisterRoutes))

	systemAPI, systemSvc, err := buildSystemAPI(cfg, db, authAPI, backupSvc, mapsHealth, geocodeBudget)
	if err != nil {
		return nil, nil, nil, err
	}
	apis = append(apis, server.WithAPI(systemAPI.RegisterRoutes))
	registerLibraryMetrics(reg, systemSvc, cfg.Metrics.LibraryTTL)

	reachChecker, err := buildReachabilityChecker(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	apis = append(apis, server.WithAPI(buildCapabilitiesAPI(reachChecker, authAPI).RegisterRoutes))
	return apis, backupSvc, reachChecker, nil
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
	mapsHealth *mapy.Health, geocodeBudget *placesjob.WindowBudget,
) ([]server.Option, backgroundServices, error) {
	jobStore := jobs.NewStore(db.Pool())
	enqueuer := jobs.NewEnqueuer(jobStore)
	registerJobQueueMetrics(reg, jobStore)
	// The sidecar scheduler every mutating API enqueues through: the real queue
	// enqueuer, or a no-op when the metadata sidecar export is switched off.
	sidecarSched := sidecarSchedulerFor(cfg, enqueuer)
	ingestAPI, err := buildIngest(cfg, db, authAPI, enqueuer, sidecarSched, reg)
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
	// One storyboard service for both readers: the photo API answers "is there a
	// scrub preview" (and schedules it), the worker renders what it scheduled.
	storyboardSvc := buildStoryboardService(cfg, db, mediaStore, enqueuer)
	photoAPI := buildPhotoAPI(cfg, db, authAPI, mediaStore, vectorStore, embedClient, matchSvc,
		trashSvc, sidecarSched, storyboardSvc, reg)
	clusterAPI, clusterSvc := buildClusterAPI(cfg, db, authAPI, matchSvc)
	mapsAPI, err := buildMapsAPI(cfg, db, authAPI, mapsHealth)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	jobWorker, jobAPI, processAPI, maintenanceAPI, err := buildJobs(cfg, db, jobStore, authAPI, enqueuer,
		embedSvc, faceSvc, clusterSvc, storyboardSvc, reg, geocodeBudget)
	if err != nil {
		return nil, backgroundServices{}, err
	}
	opts := slices.Concat([]server.Option{
		server.WithAPI(authAPI.RegisterRoutes),
		server.WithAPI(ingestAPI.RegisterRoutes),
		server.WithAPI(photoAPI.RegisterRoutes),
		server.WithAPI(clusterAPI.RegisterRoutes),
		server.WithAPI(buildBulkAPI(cfg, db, authAPI, sidecarSched).RegisterRoutes),
		server.WithAPI(buildDuplicatesAPI(cfg, db, authAPI, vectorStore).RegisterRoutes),
		server.WithAPI(mapsAPI.RegisterRoutes),
		server.WithAPI(jobAPI.RegisterRoutes),
		server.WithAPI(processAPI.RegisterRoutes),
		server.WithAPI(maintenanceAPI.RegisterRoutes),
		server.WithAPI(buildImportAPI(db, authAPI).RegisterRoutes),
	}, discoveryAPIOptions(cfg, db, authAPI, mediaStore, matchSvc), readAPIOptions(db, authAPI, mediaStore, sidecarSched))
	return opts, backgroundServices{worker: jobWorker, trash: trashSvc}, nil
}

// readAPIOptions builds the server options for the read/curation API groups that
// depend only on the shared pool and the auth guard: per-subject face outliers,
// the people (subject) catalogue, albums and labels, the places browse hierarchy,
// per-user saved searches and search history, the announcement banner, the
// returning-reader digest, the grouped global search and the audit log. Route
// groups mount on distinct paths, so their relative order does not matter.
// Splitting them out keeps buildServices within the function-length limit.
//
// The groups that return photo records take mediaStore, which decides where their
// clients fetch each photo's thumbnail and original.
// discoveryAPIOptions builds the server options for the API groups that need the
// config, the pool, the auth guard and the media store together: the editor-only
// discovery APIs riding the vector indexes (per-subject candidates, the
// recognition sweep, collection expansion, the review game and repeated-marker
// review) and the MCP server that lets an AI agent drive the library. The review
// game and repeated-marker review additionally reuse the photo API's facematch
// service so their face writes go through the one assign state machine; the MCP
// server is off unless mcp.enabled is set.
func discoveryAPIOptions(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
	matchSvc *facematch.Service,
) []server.Option {
	return []server.Option{
		server.WithAPI(buildCandidatesAPI(cfg, db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildSweepAPI(cfg, db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildExpandAPI(cfg, db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildReviewAPI(cfg, db, authAPI, mediaStore, matchSvc).RegisterRoutes),
		// Repeated-marker review shares the same assign state machine, so a marker
		// detached there is detached exactly as it is on the photo detail.
		server.WithAPI(buildDupMarkersAPI(db, authAPI, matchSvc).RegisterRoutes),
		// The MCP server mounts nothing unless mcp.enabled is set.
		server.WithAPI(buildMCPAPI(cfg, db, authAPI, mediaStore).RegisterRoutes),
	}
}

func readAPIOptions(
	db *database.DB, authAPI *auth.API, mediaStore storage.Storage, sidecar sidecarScheduler,
) []server.Option {
	return []server.Option{
		server.WithAPI(buildOutlierAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildPeopleAPI(db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildOrganizeAPI(db, authAPI, sidecar).RegisterRoutes),
		server.WithAPI(buildFeedbackAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildPlacesAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildSavedSearchAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildSearchHistoryAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildAnnouncementAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildWhatsNewAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildGlobalSearchAPI(db, authAPI, mediaStore).RegisterRoutes),
		server.WithAPI(buildAuditAPI(db, authAPI).RegisterRoutes),
	}
}
