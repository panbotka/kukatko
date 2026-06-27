package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
)

// errPSMigrateNotConfigured indicates the photo-sorter migration was invoked
// without a configured DSN.
var errPSMigrateNotConfigured = errors.New(
	"photo-sorter migration not configured: set import.photosorter.dsn (KUKATKO_IMPORT_PHOTOSORTER_DSN)")

// newMigrateCmd builds the "migrate" subcommand, which opens the configured
// database, applies any pending embedded SQL migrations in order, prints a
// summary, and exits. It is the standalone counterpart to the automatic
// migration that "serve" runs on startup. Its "photosorter" child runs the
// one-off, read-only photo-sorter data migration synchronously.
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
	migrateCmd.AddCommand(&cobra.Command{
		Use:   "photosorter",
		Short: "Migrate data from a photo-sorter database",
		Long: "Read-only, idempotent migration from a photo-sorter PostgreSQL database: " +
			"photos (matched or copied), embeddings and faces transferred 1:1, plus subjects, " +
			"markers, albums, labels, edits and perceptual hashes. Resumes from the last " +
			"successful run's watermark.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigratePhotoSorter(cmd)
		},
	})
	return migrateCmd
}

// runMigratePhotoSorter loads the configuration, opens the database (applying
// migrations), runs one full photo-sorter migration pass and prints the run id
// and counts. It returns errPSMigrateNotConfigured when the DSN is unset.
func runMigratePhotoSorter(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	if !psImportConfigured(cfg) {
		return errPSMigrateNotConfigured
	}

	ctx := cmd.Context()
	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	result, err := runPSMigration(ctx, cfg, db, jobs.NewEnqueuer(jobs.NewStore(db.Pool())))
	if err != nil {
		return fmt.Errorf("running photo-sorter migration: %w", err)
	}
	reportPSMigration(cmd.Printf, result)
	return nil
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
