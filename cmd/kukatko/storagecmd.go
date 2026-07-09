package main

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storagemigrate"
)

// Flag names of the migrate-to-r2 subcommand, named once so the definitions and
// the reads cannot drift.
const (
	flagDryRun      = "dry-run"
	flagDeleteLocal = "delete-local"
	flagConcurrency = "concurrency"
	flagBatchSize   = "batch-size"
)

// Errors the migrate-to-r2 subcommand ends on.
var (
	// errStorageR2NotConfigured indicates the R2 destination is incomplete. The
	// message names the keys, never their values.
	errStorageR2NotConfigured = errors.New(
		"R2 destination not configured: set storage.r2.endpoint, storage.r2.bucket, " +
			"storage.r2.access_key, storage.r2.secret_key and storage.temp_path")
	// errStorageMigrationFailures indicates the run finished but some photos did
	// not move. Their rows are untouched and their originals are still on disk.
	errStorageMigrationFailures = errors.New("some photos could not be migrated")
)

// storageMigrateToR2Long is the subcommand's help text. It has to carry the
// billing note: the difference between a migration that costs nothing and one
// that costs money is whether it re-uploads what is already there.
const storageMigrateToR2Long = `Copy every catalogued original, and every thumbnail already cached for it,
into the configured Cloudflare R2 bucket; verify each uploaded object against
the size and SHA256 the catalogue holds; then record on the photo's row that
its objects are in the bucket.

Object keys are the file_path values already stored in Postgres, so nothing is
re-keyed: the bucket ends up with the layout the local disk has.

The command is idempotent and resumable. It may be killed at any moment and run
again — a photo whose objects all verified and whose row was committed is
skipped, and an object the bucket already holds byte for byte is never uploaded
a second time. Per-photo failures are collected and reported at the end rather
than aborting the run; a systemic failure (bad credentials, missing bucket)
stops it immediately.

Local originals are removed only with --delete-local, only after the row is
committed, and never for a photo that failed verification. Cached thumbnails
are never removed: they are regenerable from the original.

Billing: R2 charges a Class A operation for every write and includes one million
of them per month free, so a full migration of roughly a hundred thousand
objects is expected to be free. A repeated full re-upload is not. That is why
this command first asks the bucket what it already holds — a Class B operation,
of which ten million a month are free — and writes only what is missing.

Run it while the application still serves originals from the local disk, then
flip storage.backend to "r2" once a dry run reports nothing pending.`

// newStorageCmd builds the "storage" subcommand group, which operates on the
// store that holds original media files rather than on the catalogue describing
// them.
func newStorageCmd() *cobra.Command {
	storageCmd := &cobra.Command{
		Use:   "storage",
		Short: "Operate on the store that holds original media files",
		Long:  "Inspect and migrate the store that holds Kukátko's original media files.",
		Args:  cobra.NoArgs,
	}
	storageCmd.AddCommand(newStorageMigrateToR2Cmd())
	return storageCmd
}

// newStorageMigrateToR2Cmd builds the "storage migrate-to-r2" subcommand: the
// one-off, resumable move of a local library into the R2 bucket.
func newStorageMigrateToR2Cmd() *cobra.Command {
	migrateToR2 := &cobra.Command{
		Use:   "migrate-to-r2",
		Short: "Copy every original and cached thumbnail into the R2 bucket (resumable)",
		Long:  storageMigrateToR2Long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStorageMigrateToR2(cmd)
		},
	}
	flags := migrateToR2.Flags()
	flags.Bool(flagDryRun, false,
		"report how many photos, objects and bytes would move, and change nothing")
	flags.Bool(flagDeleteLocal, false,
		"remove each local original once its objects are verified and its row is committed")
	flags.Int(flagConcurrency, storagemigrate.DefaultConcurrency,
		"how many photos to upload in parallel")
	flags.Int(flagBatchSize, storagemigrate.DefaultBatchSize,
		"how many pending photos to read from the catalogue at a time")
	return migrateToR2
}

// storageMigrateFlags is the parsed flag set of one migrate-to-r2 invocation.
type storageMigrateFlags struct {
	dryRun      bool
	deleteLocal bool
	concurrency int
	batchSize   int
}

// readStorageMigrateFlags parses the subcommand's flags, wrapping any lookup
// failure with the flag it came from.
func readStorageMigrateFlags(cmd *cobra.Command) (storageMigrateFlags, error) {
	var (
		parsed storageMigrateFlags
		err    error
	)
	if parsed.dryRun, err = cmd.Flags().GetBool(flagDryRun); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagDryRun, err)
	}
	if parsed.deleteLocal, err = cmd.Flags().GetBool(flagDeleteLocal); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagDeleteLocal, err)
	}
	if parsed.concurrency, err = cmd.Flags().GetInt(flagConcurrency); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagConcurrency, err)
	}
	if parsed.batchSize, err = cmd.Flags().GetInt(flagBatchSize); err != nil {
		return parsed, fmt.Errorf("reading --%s: %w", flagBatchSize, err)
	}
	return parsed, nil
}

// runStorageMigrateToR2 loads the configuration, opens the database (applying
// migrations, since the resume cursor is a column), builds the migrator and runs
// it to completion, printing progress as it goes and a summary at the end. It
// ends in an error when the run aborted or when any photo failed, so a cron or a
// shell notices.
func runStorageMigrateToR2(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	flags, err := readStorageMigrateFlags(cmd)
	if err != nil {
		return err
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

	migrator, err := buildStorageMigrator(cmd, cfg, db, flags)
	if err != nil {
		return err
	}
	result, runErr := migrator.Run(ctx)
	reportStorageMigration(cmd, result)
	if runErr != nil {
		return fmt.Errorf("migrating originals to R2: %w", runErr)
	}
	if len(result.Failures) > 0 {
		return fmt.Errorf("%w: %d of %d photo(s)", errStorageMigrationFailures,
			len(result.Failures), result.Start.Pending)
	}
	return nil
}

// buildStorageMigrator assembles the migrator over the local originals root, the
// local thumbnail cache and the R2 destination.
func buildStorageMigrator(
	cmd *cobra.Command, cfg *config.Config, db *database.DB, flags storageMigrateFlags,
) (*storagemigrate.Migrator, error) {
	source, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("opening local originals: %w", err)
	}
	destination, err := newR2Destination(cfg)
	if err != nil {
		return nil, err
	}
	migrator, err := storagemigrate.New(storagemigrate.Config{
		Catalogue:   storagemigrate.NewStore(db.Pool()),
		Source:      source,
		Destination: destination,
		CacheDir:    cfg.Storage.CachePath,
		Concurrency: flags.concurrency,
		BatchSize:   flags.batchSize,
		DryRun:      flags.dryRun,
		DeleteLocal: flags.deleteLocal,
		ReportEvery: storagemigrate.DefaultReportEvery,
		Report:      func(snapshot storagemigrate.Snapshot) { printStorageProgress(cmd, snapshot) },
	})
	if err != nil {
		return nil, fmt.Errorf("building the migration: %w", err)
	}
	return migrator, nil
}

// newR2Destination builds the R2 backend the migration writes into, whatever
// storage.backend currently says: the move is meant to run while the application
// still serves originals from the local disk, and only afterwards is the backend
// flipped. No media base URL or signing secret is needed — the migration mints no
// URLs, it only writes objects — so only the four bucket keys and the temp path
// are required here.
func newR2Destination(cfg *config.Config) (*storage.R2, error) {
	r2 := cfg.Storage.R2
	if r2.Endpoint == "" || r2.Bucket == "" || r2.AccessKey == "" ||
		r2.SecretKey == "" || cfg.Storage.TempPath == "" {
		return nil, errStorageR2NotConfigured
	}
	destination, err := storage.NewR2(storage.R2Options{
		Endpoint:  r2.Endpoint,
		Region:    r2.Region,
		Bucket:    r2.Bucket,
		AccessKey: r2.AccessKey,
		SecretKey: r2.SecretKey,
		TempPath:  cfg.Storage.TempPath,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising R2 destination: %w", err)
	}
	return destination, nil
}

// printStorageProgress writes one progress line. This job runs for hours; a
// silent process is a broken one.
func printStorageProgress(cmd *cobra.Command, snapshot storagemigrate.Snapshot) {
	cmd.Printf("  %d photos, %d objects, %s uploaded, %d skipped, %d failed"+
		" — ~%d photos / %s left after %s\n",
		snapshot.Photos, snapshot.Objects, humanBytes(snapshot.Bytes),
		snapshot.Skipped, snapshot.Failed, snapshot.PhotosRemaining,
		humanBytes(snapshot.BytesRemaining), snapshot.Elapsed.Round(time.Second))
}

// reportStorageMigration prints the run's summary and every collected failure.
// The failures come last and one per line, because they are what an operator has
// to act on.
func reportStorageMigration(cmd *cobra.Command, result storagemigrate.Result) {
	if result.DryRun {
		cmd.Printf("dry run: %d of %d photo(s) would move — %d object(s), %s\n",
			result.Photos, result.Start.Total, result.Objects, humanBytes(result.Bytes))
		return
	}
	cmd.Printf("migrated %d of %d pending photo(s) in %s: %d object(s) uploaded (%s), "+
		"%d already present, %d local original(s) removed, %d failed\n",
		result.Photos, result.Start.Pending, result.Elapsed.Round(time.Second),
		result.Objects, humanBytes(result.Bytes), result.Skipped, result.Deleted, result.Failed)
	if len(result.Failures) == 0 {
		return
	}
	cmd.Printf("%d photo(s) failed and were left untouched on disk:\n", len(result.Failures))
	for _, failure := range result.Failures {
		cmd.Printf("  - %s\n", failure)
	}
}

// byteUnits are the binary size units humanBytes steps through.
var byteUnits = []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}

// humanBytes renders a byte count for a human reading a terminal at 3am, in
// binary units with a single decimal (exact for plain bytes).
func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	value := float64(n)
	unit := 0
	for value >= 1024 && unit < len(byteUnits)-1 {
		value /= 1024
		unit++
	}
	return strconv.FormatFloat(value, 'f', 1, 64) + " " + byteUnits[unit]
}
