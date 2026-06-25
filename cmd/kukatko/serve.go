package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

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
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			srv := server.New(server.DefaultAddr)
			cmd.Printf("kukatko %s listening on %s\n", version.Get(), srv.Addr())

			if err := srv.Run(ctx); err != nil {
				return fmt.Errorf("running server: %w", err)
			}
			return nil
		},
	}
}
