package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
)

// newMigrateCmd builds the "migrate" subcommand, which opens the configured
// database, applies any pending embedded SQL migrations in order, prints a
// summary, and exits. It is the standalone counterpart to the automatic
// migration that "serve" runs on startup.
func newMigrateCmd() *cobra.Command {
	migrateCmd := &cobra.Command{
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
	return migrateCmd
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

// schemaMigrator is the slice of *database.DB a side-effect migration needs:
// the read-only look at what is pending and the migration itself.
type schemaMigrator interface {
	Pending(ctx context.Context) ([]string, error)
	Migrate(ctx context.Context) ([]string, error)
}

// migrateAsSideEffect brings the schema up to date for a subcommand whose job is
// something else (a scan, an import, a restore repair…). It behaves exactly like
// db.Migrate, except that when the database is behind it first says so on
// stderr: an operator who runs such a command with a new binary against a
// database an older server is still serving is about to change that server's
// schema under it (docs/OPERATIONS.md, "Upgrading"), and should not learn that
// from a broken login page. An up-to-date database prints nothing.
func migrateAsSideEffect(ctx context.Context, cmd *cobra.Command, db schemaMigrator) error {
	pending, err := db.Pending(ctx)
	if err != nil {
		return fmt.Errorf("checking pending migrations: %w", err)
	}
	if len(pending) > 0 {
		cmd.PrintErrf("applying %d pending migration(s) (%s … %s) before running this command\n",
			len(pending), pending[0], pending[len(pending)-1])
	}
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}
