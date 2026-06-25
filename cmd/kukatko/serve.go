package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/server"
	"github.com/panbotka/kukatko/internal/version"
)

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

			addr := net.JoinHostPort(cfg.Web.Host, strconv.Itoa(cfg.Web.Port))
			srv := server.New(addr)
			cmd.Printf("kukatko %s listening on %s\n", version.Get(), srv.Addr())

			if err = srv.Run(ctx); err != nil {
				return fmt.Errorf("running server: %w", err)
			}
			return nil
		},
	}
}
