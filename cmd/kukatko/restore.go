package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/restoreapi"
)

// errRestoreNotConfigured indicates a restore was invoked without a configured
// S3 source (endpoint and bucket), reusing the backup destination config.
var errRestoreNotConfigured = errors.New(
	"restore not configured: set backup.s3.endpoint and backup.s3.bucket")

// errRestoreNotConfirmed indicates a destructive database restore was invoked
// without the explicit confirmation flag.
var errRestoreNotConfirmed = errors.New(
	"refusing to restore the database without --yes: this overwrites all current data")

// newRestoreCmd builds the "restore" command tree: the disaster-recovery
// counterpart to "backup". Its children list the dumps available in the bucket,
// restore the database from a chosen dump, download missing originals, and
// verify the restored catalogue against the originals on disk. They are the
// ops entry points used to rebuild a machine from an S3 backup; none need the
// server running.
func newRestoreCmd() *cobra.Command {
	restoreCmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore the database and originals from an S3 backup",
		Long: "Disaster-recovery counterpart to `backup`: list available database dumps, " +
			"restore one via pg_restore (streamed from S3), download missing originals, and " +
			"verify the restored catalogue against the originals on disk.",
	}
	restoreCmd.AddCommand(
		newRestoreListCmd(),
		newRestoreDBCmd(),
		newRestoreOriginalsCmd(),
		newRestoreVerifyCmd(),
	)
	return restoreCmd
}

// newRestoreListCmd builds "restore list", which prints the database dumps
// available in the bucket, newest first.
func newRestoreListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the database dumps available in the bucket",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRestoreList(cmd)
		},
	}
}

// newRestoreDBCmd builds "restore db", which restores the database from a dump
// in the bucket (the latest by default, or the one named by --dump), then
// re-applies migrations. It is destructive — it overwrites the current database
// — so it requires the explicit --yes flag.
func newRestoreDBCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Restore the database from a dump (DESTRUCTIVE: overwrites all data)",
		Long: "Stream a pg_dump archive from the bucket into pg_restore, overwriting the " +
			"current database, then re-apply migrations (idempotent). Requires --yes.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRestoreDB(cmd)
		},
	}
	cmd.Flags().String("dump", "", "dump object key to restore (default: the most recent dump)")
	cmd.Flags().Bool("yes", false, "confirm the destructive restore (required)")
	cmd.Flags().Bool("verify", false, "run the integrity check against the originals on disk after restoring")
	return cmd
}

// newRestoreOriginalsCmd builds "restore originals", which downloads every
// original in the bucket that is not already present on disk at the same key and
// size. It is safe to interrupt and re-run.
func newRestoreOriginalsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "originals",
		Short: "Download missing originals from the bucket (skips those already present)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRestoreOriginals(cmd)
		},
	}
}

// newRestoreVerifyCmd builds "restore verify", which reconciles the catalogue in
// the database against the originals on disk and reports counts and mismatches.
func newRestoreVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Report catalogue/originals integrity (photos in DB vs originals on disk)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRestoreVerify(cmd)
		},
	}
}

// restoreConfig loads the configuration and verifies an S3 source is configured,
// returning errRestoreNotConfigured otherwise.
func restoreConfig(cmd *cobra.Command) (*config.Config, error) {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return nil, err
	}
	if !backupConfigured(cfg) {
		return nil, errRestoreNotConfigured
	}
	return cfg, nil
}

// buildRestoreAPI assembles the admin-only restore HTTP API (list dumps + run
// the integrity check). When no S3 destination is configured the service is nil,
// so the endpoints report 503; otherwise the service is wired over the bucket,
// the catalogue and the on-disk originals. The destructive database restore is
// intentionally not exposed over HTTP — it lives only in `kukatko restore db`.
func buildRestoreAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) (*restoreapi.API, error) {
	var svc restoreapi.Service
	if backupConfigured(cfg) {
		objects, err := buildRestoreObjects(cfg)
		if err != nil {
			return nil, err
		}
		svc = backup.NewRestoreService(backup.RestoreConfig{
			Objects:   objects,
			Photos:    photos.NewStore(db.Pool()),
			Originals: backup.NewDiskOriginals(cfg.Storage.OriginalsPath),
		})
	}
	return restoreapi.NewAPI(restoreapi.Config{
		Service:      svc,
		RequireAdmin: authAPI.RequireAdmin,
	}), nil
}

// buildRestoreObjects assembles the S3 object store for restore over the
// configured backup destination (reused as the restore source).
func buildRestoreObjects(cfg *config.Config) (backup.ObjectStore, error) {
	store, err := backup.NewS3Store(backup.S3Options{
		Endpoint:  cfg.Backup.S3.Endpoint,
		Region:    cfg.Backup.S3.Region,
		Bucket:    cfg.Backup.S3.Bucket,
		AccessKey: cfg.Backup.S3.AccessKey,
		SecretKey: cfg.Backup.S3.SecretKey,
		PathStyle: cfg.Backup.S3.PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising S3 restore source: %w", err)
	}
	return store, nil
}

// runRestoreList lists the available dumps and prints them newest first.
func runRestoreList(cmd *cobra.Command) error {
	cfg, err := restoreConfig(cmd)
	if err != nil {
		return err
	}
	objects, err := buildRestoreObjects(cfg)
	if err != nil {
		return err
	}
	svc := backup.NewRestoreService(backup.RestoreConfig{Objects: objects})
	dumps, err := svc.ListDumps(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing dumps: %w", err)
	}
	if len(dumps) == 0 {
		cmd.Println("no database dumps found in the bucket")
		return nil
	}
	cmd.Printf("%d dump(s) available (newest first):\n", len(dumps))
	for _, dump := range dumps {
		cmd.Printf("  %s  (%d bytes)\n", dump.Key, dump.Size)
	}
	return nil
}

// runRestoreDB restores the database from the chosen dump, re-applies migrations,
// and optionally runs the integrity check. It requires the --yes confirmation.
func runRestoreDB(cmd *cobra.Command) error {
	cfg, err := restoreConfig(cmd)
	if err != nil {
		return err
	}
	confirmed, err := cmd.Flags().GetBool("yes")
	if err != nil {
		return fmt.Errorf("reading --yes flag: %w", err)
	}
	if !confirmed {
		return errRestoreNotConfirmed
	}
	dumpKey, err := cmd.Flags().GetString("dump")
	if err != nil {
		return fmt.Errorf("reading --dump flag: %w", err)
	}

	objects, err := buildRestoreObjects(cfg)
	if err != nil {
		return err
	}
	svc := backup.NewRestoreService(backup.RestoreConfig{
		Objects:  objects,
		Restorer: backup.NewPgRestorer(cfg.Database.URL),
	})
	restored, err := svc.RestoreDatabase(cmd.Context(), dumpKey)
	if err != nil {
		return fmt.Errorf("restoring database: %w", err)
	}
	cmd.Printf("database restored from %s\n", restored)

	if err := reapplyMigrations(cmd, cfg); err != nil {
		return err
	}
	cmd.Println("migrations re-applied (idempotent)")

	verify, err := cmd.Flags().GetBool("verify")
	if err != nil {
		return fmt.Errorf("reading --verify flag: %w", err)
	}
	if verify {
		return runRestoreVerify(cmd)
	}
	cmd.Println("next: `kukatko restore originals` to download originals, then `kukatko restore verify`")
	return nil
}

// reapplyMigrations opens the database and applies any pending migrations after a
// restore. The restored dump already contains the schema, so this is normally a
// no-op, but it guarantees the schema matches this binary's expectations.
func reapplyMigrations(cmd *cobra.Command, cfg *config.Config) error {
	db, err := database.New(cmd.Context(), cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err := db.Migrate(cmd.Context()); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}
	return nil
}

// runRestoreOriginals downloads missing originals from the bucket and prints how
// many were downloaded versus skipped.
func runRestoreOriginals(cmd *cobra.Command) error {
	cfg, err := restoreConfig(cmd)
	if err != nil {
		return err
	}
	objects, err := buildRestoreObjects(cfg)
	if err != nil {
		return err
	}
	svc := backup.NewRestoreService(backup.RestoreConfig{
		Objects:   objects,
		Originals: backup.NewDiskOriginals(cfg.Storage.OriginalsPath),
	})
	res, err := svc.RestoreOriginals(cmd.Context())
	if err != nil {
		return fmt.Errorf("restoring originals: %w", err)
	}
	cmd.Printf("originals restore complete: downloaded=%d skipped=%d\n", res.Downloaded, res.Skipped)
	return nil
}

// runRestoreVerify runs the integrity check and prints the counts and any
// mismatches between the catalogue and the originals on disk.
func runRestoreVerify(cmd *cobra.Command) error {
	cfg, err := restoreConfig(cmd)
	if err != nil {
		return err
	}
	db, err := database.New(cmd.Context(), cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()

	objects, err := buildRestoreObjects(cfg)
	if err != nil {
		return err
	}
	svc := backup.NewRestoreService(backup.RestoreConfig{
		Objects:   objects,
		Photos:    photos.NewStore(db.Pool()),
		Originals: backup.NewDiskOriginals(cfg.Storage.OriginalsPath),
	})
	report, err := svc.Verify(cmd.Context())
	if err != nil {
		return fmt.Errorf("verifying integrity: %w", err)
	}
	printVerifyReport(cmd, report)
	return nil
}

// printVerifyReport prints a human-readable integrity report, listing a bounded
// sample of any mismatches so a large discrepancy does not flood the terminal.
func printVerifyReport(cmd *cobra.Command, report backup.VerifyReport) {
	cmd.Printf("photos in DB:       %d\n", report.PhotosInDB)
	cmd.Printf("files in DB:        %d\n", report.FilesInDB)
	cmd.Printf("originals on disk:  %d\n", report.OriginalsOnDisk)
	if report.Consistent {
		cmd.Println("integrity: OK (catalogue and originals are consistent)")
		return
	}
	cmd.Printf("integrity: MISMATCH (missing on disk=%d, extra on disk=%d)\n",
		len(report.MissingOnDisk), len(report.ExtraOnDisk))
	printSample(cmd, "missing on disk (catalogued but no file)", report.MissingOnDisk)
	printSample(cmd, "extra on disk (file but no catalogue row)", report.ExtraOnDisk)
}

// printSampleLimit caps how many mismatch entries are printed per category.
const printSampleLimit = 20

// printSample prints up to printSampleLimit entries from keys under a heading,
// noting how many more were omitted.
func printSample(cmd *cobra.Command, heading string, keys []string) {
	if len(keys) == 0 {
		return
	}
	cmd.Printf("  %s:\n", heading)
	shown := keys
	if len(shown) > printSampleLimit {
		shown = shown[:printSampleLimit]
	}
	for _, key := range shown {
		cmd.Printf("    - %s\n", key)
	}
	if len(keys) > len(shown) {
		cmd.Printf("    … and %d more\n", len(keys)-len(shown))
	}
}
