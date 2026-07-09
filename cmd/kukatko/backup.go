package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/backupapi"
	"github.com/panbotka/kukatko/internal/config"
)

// errBackupNotConfigured indicates a backup was invoked without a configured S3
// destination (endpoint and bucket).
var errBackupNotConfigured = errors.New(
	"backup not configured: set backup.s3.endpoint and backup.s3.bucket")

// errBackupSameBucket indicates the backup destination is the very bucket the
// originals already live in, which would back up nothing at all. Object storage
// is not a backup, and with no object versioning underneath, the second bucket
// is the only thing between a stray delete and a lost photo — so this fails
// loudly rather than pretending to protect the library.
var errBackupSameBucket = errors.New(
	"backup.s3 points at the primary originals bucket: " +
		"set backup.s3.bucket to a second, independent bucket")

// backupConfigured reports whether an S3 backup destination is configured, i.e.
// both an endpoint and a bucket are set. Callers gate building the backup
// service, starting its scheduler and enabling the trigger on this.
func backupConfigured(cfg *config.Config) bool {
	return cfg.Backup.S3.Endpoint != "" && cfg.Backup.S3.Bucket != ""
}

// buildBackupService assembles the backup service over the configured S3
// destination, the originals source matching the storage backend and a pg_dump
// dumper for the database (pg_dump connects to the DB itself via the DSN, so no
// live pool is needed). It returns (nil, nil) when no destination is configured,
// so the caller can skip the scheduler and mount the API in its unconfigured
// (503) mode.
func buildBackupService(cfg *config.Config) (*backup.Service, error) {
	if !backupConfigured(cfg) {
		return nil, nil //nolint:nilnil // (nil, nil) is the documented "not configured" signal.
	}
	store, err := backup.NewS3Store(backup.S3Options{
		Endpoint:  cfg.Backup.S3.Endpoint,
		Region:    cfg.Backup.S3.Region,
		Bucket:    cfg.Backup.S3.Bucket,
		AccessKey: cfg.Backup.S3.AccessKey,
		SecretKey: cfg.Backup.S3.SecretKey,
		PathStyle: cfg.Backup.S3.PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising S3 backup store: %w", err)
	}
	originals, err := buildBackupOriginals(cfg)
	if err != nil {
		return nil, err
	}
	return backup.New(backup.Config{
		Objects:   store,
		Originals: originals,
		Dumper:    backup.NewPgDumper(cfg.Database.URL),
		Retention: cfg.Backup.Retention,
	}), nil
}

// buildBackupOriginals picks the originals source that matches the configured
// storage backend: the local originals root for "fs", and the primary bucket for
// "r2", whose objects the backup endpoint copies across server-side rather than
// dragging the library down to this host only to upload it again. Pointing the
// backup at the primary bucket itself is rejected with errBackupSameBucket.
func buildBackupOriginals(cfg *config.Config) (backup.OriginalSource, error) {
	if cfg.Storage.Backend != config.StorageBackendR2 {
		return backup.NewDiskOriginals(cfg.Storage.OriginalsPath), nil
	}
	if sameBucket(cfg.Backup.S3, cfg.Storage.R2) {
		return nil, errBackupSameBucket
	}
	source, err := backup.NewS3Store(backup.S3Options{
		Endpoint:  cfg.Storage.R2.Endpoint,
		Region:    cfg.Storage.R2.Region,
		Bucket:    cfg.Storage.R2.Bucket,
		AccessKey: cfg.Storage.R2.AccessKey,
		SecretKey: cfg.Storage.R2.SecretKey,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising primary bucket client: %w", err)
	}
	originals, err := backup.NewBucketOriginals(source, cfg.Storage.R2.Bucket)
	if err != nil {
		return nil, fmt.Errorf("wiring bucket originals: %w", err)
	}
	return originals, nil
}

// sameBucket reports whether the backup destination and the primary originals
// bucket are one and the same, comparing bucket name and endpoint (the same
// bucket name at two different providers is two different buckets). Endpoints
// are compared case-insensitively because a host name is.
func sameBucket(dst config.S3Config, primary config.R2Config) bool {
	return dst.Bucket == primary.Bucket && strings.EqualFold(dst.Endpoint, primary.Endpoint)
}

// buildBackupAPI assembles the admin-only backup HTTP API. A nil service (no
// destination configured) yields an API whose status endpoint reports
// configured=false and whose trigger returns 503. The admin guard is supplied
// via authAPI so backupapi stays decoupled from auth's wiring.
func buildBackupAPI(svc *backup.Service, authAPI *auth.API) *backupapi.API {
	var service backupapi.Service
	// A nil *backup.Service must be passed as a nil interface, not a non-nil
	// interface wrapping a nil pointer, so the API's nil check works.
	if svc != nil {
		service = svc
	}
	return backupapi.NewAPI(backupapi.Config{
		Service:      service,
		RequireAdmin: authAPI.RequireAdmin,
	})
}

// newBackupCmd builds the "backup" subcommand, which runs one full backup
// synchronously (database dump, originals sync, retention prune) and prints the
// resulting counts. It is the ops/cron entry point that does not need the server
// running; the same backup also runs on the configured schedule from `serve` and
// can be triggered via the admin API.
func newBackupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backup",
		Short: "Run a one-off S3 backup (database dump + originals sync)",
		Long: "Stream a pg_dump of the database and incrementally sync the originals to the " +
			"configured S3-compatible bucket, then prune old dumps to the retention limit.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBackup(cmd)
		},
	}
}

// runBackup loads the configuration, builds the backup service and runs one full
// backup pass, printing the resulting counts. It returns errBackupNotConfigured
// when no S3 destination is configured.
func runBackup(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	if !backupConfigured(cfg) {
		return errBackupNotConfigured
	}

	svc, err := buildBackupService(cfg)
	if err != nil {
		return err
	}
	result, err := svc.Run(cmd.Context(), time.Now())
	if err != nil {
		return fmt.Errorf("running backup: %w", err)
	}
	cmd.Printf("backup complete: dump=%s originals uploaded=%d skipped=%d dumps pruned=%d\n",
		result.DumpKey, result.OriginalsUploaded, result.OriginalsSkipped, result.DumpsPruned)
	return nil
}
