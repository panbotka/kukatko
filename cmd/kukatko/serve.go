package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/server"
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

	authAPI, authSvc := buildAuth(cfg, db)
	if err := runBootstrap(ctx, cmd, authSvc, cfg.Auth); err != nil {
		return err
	}
	go authSvc.RunCleanup(ctx, sessionCleanupInterval)
	go authAPI.RunMaintenance(ctx, sessionCleanupInterval)

	apis, jobWorker, err := buildServices(cfg, db, authAPI)
	if err != nil {
		return err
	}
	startWorker(ctx, jobWorker)

	addr := net.JoinHostPort(cfg.Web.Host, strconv.Itoa(cfg.Web.Port))
	srv := server.New(addr, apis...)
	cmd.Printf("kukatko %s listening on %s\n", version.Get(), srv.Addr())

	if err = srv.Run(ctx); err != nil {
		return fmt.Errorf("running server: %w", err)
	}
	return nil
}

// buildServices assembles every HTTP API group and the background worker over a
// shared queue store: upload/ingest, photo browse/curation (with embedding-backed
// similar search), face auto-clustering, per-subject face outlier detection, the
// subject (people) catalogue, the album and label catalogue, the admin jobs and
// processing APIs, and the image_embed and face_detect worker handlers. It
// returns the server options registering those routes plus the worker for the
// serve command to run.
func buildServices(
	cfg *config.Config, db *database.DB, authAPI *auth.API,
) ([]server.Option, *worker.Worker, error) {
	jobStore := jobs.NewStore(db.Pool())
	enqueuer := jobs.NewEnqueuer(jobStore)

	ingestAPI, err := buildIngest(cfg, db, authAPI, enqueuer)
	if err != nil {
		return nil, nil, err
	}
	embedSvc, vectorStore, embedClient, err := buildEmbedService(cfg, db, enqueuer)
	if err != nil {
		return nil, nil, err
	}
	faceSvc, err := buildFaceService(cfg, db, enqueuer, vectorStore, embedClient)
	if err != nil {
		return nil, nil, err
	}
	matchSvc := buildFaceMatch(cfg, db)
	photoAPI, err := buildPhotoAPI(cfg, db, authAPI, vectorStore, embedClient, matchSvc)
	if err != nil {
		return nil, nil, err
	}
	clusterAPI, clusterSvc := buildClusterAPI(cfg, db, authAPI, matchSvc)
	outlierAPI := buildOutlierAPI(db, authAPI)
	peopleAPI := buildPeopleAPI(db, authAPI)
	organizeAPI := buildOrganizeAPI(db, authAPI)
	bulkAPI := buildBulkAPI(cfg, db, authAPI)
	jobWorker, jobAPI, processAPI := buildJobs(cfg, jobStore, authAPI, embedSvc, faceSvc, clusterSvc)

	return []server.Option{
		server.WithAPI(authAPI.RegisterRoutes),
		server.WithAPI(ingestAPI.RegisterRoutes),
		server.WithAPI(photoAPI.RegisterRoutes),
		server.WithAPI(clusterAPI.RegisterRoutes),
		server.WithAPI(outlierAPI.RegisterRoutes),
		server.WithAPI(peopleAPI.RegisterRoutes),
		server.WithAPI(organizeAPI.RegisterRoutes),
		server.WithAPI(bulkAPI.RegisterRoutes),
		server.WithAPI(jobAPI.RegisterRoutes),
		server.WithAPI(processAPI.RegisterRoutes),
	}, jobWorker, nil
}
