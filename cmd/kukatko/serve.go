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

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/server"
	"github.com/panbotka/kukatko/internal/version"
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

			ingestAPI, err := buildIngest(cfg, db, authAPI)
			if err != nil {
				return err
			}

			photoAPI, err := buildPhotoAPI(cfg, db, authAPI)
			if err != nil {
				return err
			}

			jobWorker, jobAPI := buildJobs(cfg, db, authAPI)
			startWorker(ctx, jobWorker)

			addr := net.JoinHostPort(cfg.Web.Host, strconv.Itoa(cfg.Web.Port))
			srv := server.New(addr,
				server.WithAPI(authAPI.RegisterRoutes),
				server.WithAPI(ingestAPI.RegisterRoutes),
				server.WithAPI(photoAPI.RegisterRoutes),
				server.WithAPI(jobAPI.RegisterRoutes),
			)
			cmd.Printf("kukatko %s listening on %s\n", version.Get(), srv.Addr())

			if err = srv.Run(ctx); err != nil {
				return fmt.Errorf("running server: %w", err)
			}
			return nil
		},
	}
}
