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
	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mapy"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/obs"
	"github.com/panbotka/kukatko/internal/placesjob"
	"github.com/panbotka/kukatko/internal/project"
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

	apis, projectAPIs, bg, err := buildServices(cfg, db, authAPI, reg, mapsHealth, geocodeBudget)
	if err != nil {
		return err
	}
	apis, backupSvc, reachChecker, err := appendOpsAPIs(cfg, db, authAPI, apis, projectAPIs, reg, mapsHealth,
		geocodeBudget)
	if err != nil {
		return err
	}

	if err := startBackgroundServices(ctx, cfg, db, reg, bg, backupSvc, reachChecker); err != nil {
		return err
	}
	// One-off, off the startup path: which image model the sidecar actually serves,
	// and whether its vector width still matches what this binary stores.
	go verifyEmbeddingDim(ctx, cfg, logger)

	apis = append(apis, observabilityOptions(reg, logger)...)

	apis = append(apis, requestOptions(cfg, db, logger, projectAPIs)...)

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
// job worker, the trash retention purge, the daily tasks-digest scheduler
// (inert unless the digest and mail are both on), the daily notification
// retention purge scheduler, the auto-wake check loop
// (inert when disabled), the embeddings-reachability probe loop (inert when no
// embedding URL is configured) that backs GET /capabilities, and — when
// configured — the scheduled S3 backup. reg (nil when metrics are off) receives
// the auto-wake loop's box probes.
func startBackgroundServices(
	ctx context.Context, cfg *config.Config, db *database.DB, reg *metrics.Registry,
	bg backgroundServices, backupSvc *backup.Service, reachChecker *reachability.Checker,
) error {
	wakeSvc, err := buildWakeService(cfg, db, reg)
	if err != nil {
		return err
	}
	startWorker(ctx, bg.worker)
	go bg.trash.RunPurge(ctx, trashPurgeInterval)
	go buildTaskDigestScheduler(cfg, db).Run(ctx)
	go buildNotifyPurgeScheduler(db).Run(ctx)
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

// trustedProxies resolves web.trusted_proxies into the matcher the HTTP server
// keys the client address on, and records at startup which of the two regimes is
// active — an operator debugging "why is every request from the same IP" (proxy
// outside the list) or "why did the limiter not fire" (list too wide) should not
// have to read the config to find out.
//
// The value was validated during config.Load, so a parse failure here can only
// mean that guarantee was weakened. It degrades in the safe direction, trusting
// nobody, rather than failing a start that would otherwise succeed.
func trustedProxies(cfg *config.Config, logger *slog.Logger) *clientip.Set {
	set, err := cfg.Web.TrustedProxySet()
	if err != nil {
		logger.Error("web.trusted_proxies did not parse; trusting no proxy, forwarding headers are ignored",
			"error", err)
		return nil
	}
	if set.Empty() {
		logger.Warn("no trusted proxies configured; forwarding headers are ignored " +
			"and every request is attributed to the address it was dialled from")
		return set
	}
	logger.Info("trusted proxies", "networks", cfg.Web.TrustedProxies)
	return set
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
	authAPI, authSvc, err := buildAuth(cfg, db)
	if err != nil {
		return nil, err
	}
	if err := runBootstrap(ctx, cmd, authSvc, cfg.Auth); err != nil {
		return nil, err
	}
	go authSvc.RunCleanup(ctx, sessionCleanupInterval)
	go authAPI.RunMaintenance(ctx, sessionCleanupInterval)
	return authAPI, nil
}

// appendOpsAPIs mounts the always-on backup, restore, storage-move and
// system-status APIs onto apis and puts the capabilities API — project-scoped — into projectAPIs.
// The backup API self-reports "not configured" and the restore service is nil
// (503) when no destination is set; the returned
// backup service drives the scheduler (nil when not configured). It also builds
// the embeddings-reachability checker that both backs GET /capabilities and the
// caller starts as a background loop, returning it for that purpose. It also
// registers the library-content and platform /metrics collectors over the system
// service it builds, so the gauges and the dashboard share one aggregation.
func appendOpsAPIs(
	cfg *config.Config, db *database.DB, authAPI *auth.API, apis []server.Option, projectAPIs *projectAPI,
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

	rekeyAPI, err := buildRekeyAPI(cfg, db, authAPI)
	if err != nil {
		return nil, nil, nil, err
	}
	apis = append(apis, server.WithAPI(rekeyAPI.RegisterRoutes))

	systemAPI, systemSvc, err := buildSystemAPI(cfg, db, authAPI, backupSvc, mapsHealth, geocodeBudget)
	if err != nil {
		return nil, nil, nil, err
	}
	apis = append(apis, server.WithAPI(systemAPI.RegisterRoutes))
	registerLibraryMetrics(reg, systemSvc, cfg.Metrics.LibraryTTL)
	registerPlatformMetrics(reg, systemSvc)

	reachChecker, err := buildReachabilityChecker(cfg, reg)
	if err != nil {
		return nil, nil, nil, err
	}
	projectAPIs.capabilities = buildCapabilitiesAPI(cfg, reachChecker, authAPI).RegisterRoutes
	return apis, backupSvc, reachChecker, nil
}

// requestOptions builds the server options that decide who and where a request
// is: the trusted proxies that may rename the client, auth.WithProject in front
// of every /api/v1 route, which names the request's project before any Require*
// guard runs (the bootstrap project for a route without {project}), and the
// project-scoped API mounted under /api/v1/p/{project} plus its unprefixed
// transition alias (see projectRoutes).
func requestOptions(cfg *config.Config, db *database.DB, logger *slog.Logger, api *projectAPI) []server.Option {
	projects := project.NewStore(db.Pool())
	return []server.Option{
		server.WithTrustedProxies(trustedProxies(cfg, logger)),
		server.WithAPIMiddleware(auth.WithProject(projects)),
		projectRoutes(projects, api),
	}
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
) ([]server.Option, *projectAPI, backgroundServices, error) {
	jobStore := jobs.NewStore(db.Pool())
	enqueuer := jobs.NewEnqueuer(jobStore)
	registerJobQueueMetrics(reg, jobStore)
	// The queue, or no-ops when an export is off (see exportSchedulersFor).
	exports := exportSchedulersFor(cfg, enqueuer)
	uploads, err := buildIngest(cfg, db, authAPI, enqueuer, exports.sidecar, reg)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	embedSvc, vectorStore, embedClient, err := buildEmbedService(cfg, db, enqueuer, reg)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	faceSvc, err := buildFaceService(cfg, db, enqueuer, vectorStore, embedClient)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	matchSvc := buildFaceMatch(cfg, db)
	trashSvc, err := buildTrashService(cfg, db)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	mediaStore, err := newStorage(cfg)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	// One storyboard service for both readers: the photo API answers "is there a
	// scrub preview" (and schedules it), the worker renders what it scheduled.
	storyboardSvc := buildStoryboardService(cfg, db, mediaStore, enqueuer)
	// The places service is built here rather than with the other job handlers
	// because it has two readers: the `places` worker handler and the regeocode
	// rebuild endpoint running its forced path on demand. One service, so a rebuild
	// spends its mapy.com credit against the same budget and limiter the queue does.
	placesSvc, err := buildPlacesServiceOrNil(cfg, db, enqueuer, geocodeBudget, reg)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	rebuilders := photoRebuilders{embed: embedSvc, face: faceSvc, places: placesSvc}
	photoAPI := buildPhotoAPI(cfg, db, authAPI, mediaStore, vectorStore, embedClient, matchSvc,
		trashSvc, exports.sidecar, enqueuer, storyboardSvc, jobStore, enqueuer, rebuilders, reg)
	clusterAPI, clusterSvc := buildClusterAPI(cfg, db, jobStore, authAPI, matchSvc)
	mapsAPI, err := buildMapsAPI(cfg, db, authAPI, mapsHealth)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	jobWorker, jobAPI, processAPI, maintenanceAPI, err := buildJobs(cfg, db, jobStore, authAPI, enqueuer,
		embedSvc, faceSvc, clusterSvc, storyboardSvc, embedClient, reg, placesSvc)
	if err != nil {
		return nil, nil, backgroundServices{}, err
	}
	api := &projectAPI{
		photos: photoAPI.RegisterRoutes, clusters: clusterAPI.RegisterRoutes, maps: mapsAPI.RegisterRoutes,
		bulk:       buildBulkAPI(cfg, db, authAPI, exports.sidecar, enqueuer).RegisterRoutes,
		duplicates: buildDuplicatesAPI(cfg, db, authAPI, vectorStore).RegisterRoutes,
		process:    processAPI.RegisterRoutes, imports: buildImportAPI(db, authAPI).RegisterRoutes,
		upload: uploads.upload, uploadLinks: uploads.uploadLinks, users: authAPI.RegisterDirectoryRoutes,
	}
	opts := slices.Concat([]server.Option{ // the instance-wide groups; the rest mount with projectRoutes
		server.WithAPI(authAPI.RegisterInstanceRoutes), server.WithAPI(uploads.public),
		server.WithAPI(jobAPI.RegisterRoutes), server.WithAPI(maintenanceAPI.RegisterRoutes),
	}, discoveryAPIOptions(api, cfg, db, authAPI, mediaStore, matchSvc),
		readAPIOptions(api, cfg, db, authAPI, mediaStore, exports))
	return opts, api, backgroundServices{worker: jobWorker, trash: trashSvc}, nil
}

// discoveryAPIOptions fills api with the project-scoped groups that need the
// config, the pool, the auth guard and the media store together: the
// editor-only discovery APIs riding the vector indexes (per-subject candidates,
// the recognition sweep, collection expansion, the review game and
// repeated-marker review), the MCP server that lets an AI agent drive the
// library, and — the one read here open to everybody — the people index's
// subject avatars, which are in this group because rendering one needs the
// config's derived-media cache path. It returns the one instance-wide group
// built from the same parts: the profile pictures, which cut the same square
// for an account (GET /users/{uid}/avatar, /auth/picture). The review game and
// repeated-marker review additionally reuse the photo API's facematch service so
// their face writes go through the one assign state machine; the MCP server is
// off unless mcp.enabled is set.
func discoveryAPIOptions(
	api *projectAPI, cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
	matchSvc *facematch.Service,
) []server.Option {
	api.candidates = buildCandidatesAPI(cfg, db, authAPI, mediaStore).RegisterRoutes
	api.sweep = buildSweepAPI(cfg, db, authAPI, mediaStore).RegisterRoutes
	api.expand = buildExpandAPI(cfg, db, authAPI, mediaStore).RegisterRoutes
	api.review = buildReviewAPI(cfg, db, authAPI, mediaStore, matchSvc).RegisterRoutes
	// Repeated-marker review shares the same assign state machine, so a marker
	// detached there is detached exactly as it is on the photo detail.
	api.dupMarkers = buildDupMarkersAPI(db, authAPI, matchSvc).RegisterRoutes
	// The MCP server mounts nothing but its 404 unless mcp.enabled is set.
	api.mcp = buildMCPAPI(cfg, db, authAPI, mediaStore).RegisterRoutes
	api.avatars = buildAvatarAPI(cfg, db, authAPI, mediaStore).RegisterRoutes
	// The same crop, for an account rather than a person: it shares the
	// avatar renderer and its cache, so a photo cut for one is not cut twice.
	return []server.Option{server.WithAPI(buildUserPicAPI(cfg, db, authAPI, mediaStore).RegisterRoutes)}
}

// readAPIOptions fills api with the project-scoped read/curation groups that
// need little more than the shared pool and the auth guard: per-subject face
// outliers, the people (subject) catalogue and the genealogy over it, albums
// and labels, feedback, the places browse hierarchy, the task queue, per-user
// saved searches and search history, the announcement banner, the instance
// settings, the returning-reader digest, the notification inbox and the
// grouped global search, and a project admin's member management and
// invitation links. It returns the instance-wide groups built alongside: the
// push subscriptions (the other half of the notification API), the audit log,
// the system administrator's project and account management, the public
// invitation page and accept, and the sign-in screen's anonymous instance
// facts (GET /settings/public).
// Splitting them out keeps buildServices within the function-length limit.
//
// The groups that return photo records take mediaStore, which decides where
// their clients fetch each photo's thumbnail and original. The instance
// settings take cfg for one boolean: whether mail is enabled, which their
// instance-level public response reports so the screens around registration
// promise an e-mail only where one is sent.
func readAPIOptions(
	api *projectAPI, cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
	exports exportSchedulers,
) []server.Option {
	api.outliers = buildOutlierAPI(db, authAPI).RegisterRoutes
	api.people = buildPeopleAPI(db, authAPI, mediaStore, exports.family).RegisterRoutes
	// The family routes hang off the same /subjects prefix, flat like the
	// outlier one, so they mount alongside rather than under peopleapi.
	api.family = buildFamilyAPI(db, authAPI, exports.family).RegisterRoutes
	api.organize = buildOrganizeAPI(cfg, db, authAPI, mediaStore, exports.sidecar).RegisterRoutes
	api.feedback = buildFeedbackAPI(db, authAPI).RegisterRoutes
	api.places = buildPlacesAPI(db, authAPI).RegisterRoutes
	api.tasks = buildPhotoTaskAPI(cfg, db, authAPI).RegisterRoutes
	api.savedSearches = buildSavedSearchAPI(db, authAPI).RegisterRoutes
	api.searchHistory = buildSearchHistoryAPI(db, authAPI).RegisterRoutes
	api.announcement = buildAnnouncementAPI(db, authAPI).RegisterRoutes
	settingsAPI := buildSettingsAPI(cfg, db, authAPI)
	api.settings = settingsAPI.RegisterRoutes
	api.members = buildMemberAPI(cfg, db, authAPI).RegisterRoutes
	invites := buildInviteAPI(cfg, db, authAPI)
	api.invites = invites.RegisterRoutes
	api.whatsNew = buildWhatsNewAPI(db, authAPI).RegisterRoutes
	api.globalSearch = buildGlobalSearchAPI(db, authAPI, mediaStore).RegisterRoutes
	notifications := buildNotificationAPI(cfg, db, authAPI, mediaStore)
	api.notifications = notifications.RegisterInboxRoutes
	return []server.Option{
		server.WithAPI(notifications.RegisterPushRoutes),
		server.WithAPI(buildAuditAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(buildProjectsAPI(db, authAPI).RegisterRoutes),
		server.WithAPI(invites.RegisterPublicRoutes),
		server.WithAPI(settingsAPI.RegisterInstanceRoutes),
	}
}
