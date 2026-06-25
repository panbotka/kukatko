package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
)

// newMigrateCmd builds the "migrate" subcommand, which opens the configured
// database, applies any pending embedded SQL migrations in order, prints a
// summary, and exits. It is the standalone counterpart to the automatic
// migration that "serve" runs on startup.
func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations and exit",
		Long:  "Open the configured database and apply any pending embedded SQL migrations in order.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfigFromFlags(cmd)
			if err != nil {
				return err
			}

			db, err := database.New(cmd.Context(), cfg.Database)
			if err != nil {
				return fmt.Errorf("connecting to database: %w", err)
			}
			defer db.Close()

			applied, err := db.Migrate(cmd.Context())
			if err != nil {
				return fmt.Errorf("applying migrations: %w", err)
			}
			reportMigrations(cmd, applied)
			return nil
		},
	}
}

// reportMigrations prints a human-readable summary of the migrations applied by
// a migrate run to the command's output stream.
func reportMigrations(cmd *cobra.Command, applied []string) {
	if len(applied) == 0 {
		cmd.Println("database is up to date; no migrations applied")
		return
	}
	cmd.Printf("applied %d migration(s):\n", len(applied))
	for _, name := range applied {
		cmd.Printf("  - %s\n", name)
	}
}
