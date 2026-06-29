package main

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/psimport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

// psImportConfigured reports whether the photo-sorter migration is enabled, i.e.
// a read-only DSN is set. Callers gate building the migration command, its job
// handler and its HTTP trigger on this.
func psImportConfigured(cfg *config.Config) bool {
	return cfg.Import.PhotoSorter.DSN != ""
}

// newPSImportService assembles the photo-sorter migration over the shared pool
// and an opened photo-sorter reader: the import-run store, the photo catalogue,
// the embeddings/faces store, on-disk storage and thumbnailer, the
// album/label/people catalogues and the job enqueuer (to cover photos
// photo-sorter never embedded or detected).
func newPSImportService(
	cfg *config.Config, db *database.DB, reader psimport.Source, enqueuer psimport.Enqueuer, reg *metrics.Registry,
) (*psimport.Service, error) {
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	pool := db.Pool()
	return psimport.New(psimport.Config{
		Source:      reader,
		Runs:        importer.NewStore(pool),
		Photos:      photos.NewStore(pool),
		Vectors:     vectors.NewStore(pool),
		People:      people.NewStore(pool),
		Albums:      organize.NewStore(pool),
		Labels:      organize.NewStore(pool),
		Storage:     store,
		Thumbnailer: thumb.New(store, cfg.Storage.CachePath, thumbOptions(reg)...),
		Enqueuer:    enqueuer,
		PageSize:    cfg.Import.PhotoSorter.PageSize,
		Metrics:     importObserver(reg),
	}), nil
}

// runPSMigration opens a read-only photo-sorter reader, builds the migration
// service and runs one full migration pass, closing the reader afterwards. It is
// shared by the CLI command and the background ps_migrate job handler so the
// photo-sorter connection pool lives only for the duration of a migration.
func runPSMigration(
	ctx context.Context, cfg *config.Config, db *database.DB, enqueuer psimport.Enqueuer, reg *metrics.Registry,
) (psimport.Result, error) {
	reader, err := photosorter.New(ctx, photosorter.Config{DSN: cfg.Import.PhotoSorter.DSN})
	if err != nil {
		return psimport.Result{}, fmt.Errorf("connecting to photo-sorter database: %w", err)
	}
	defer reader.Close()

	svc, err := newPSImportService(cfg, db, reader, enqueuer, reg)
	if err != nil {
		return psimport.Result{}, err
	}
	result, err := svc.Migrate(ctx)
	if err != nil {
		return psimport.Result{}, fmt.Errorf("running photo-sorter migration: %w", err)
	}
	return result, nil
}

// psMigrateHandlerOrNil returns the ps_migrate worker handler when the migration
// is configured, or nil otherwise. Keeping the gate here keeps buildServices'
// branch count down.
func psMigrateHandlerOrNil(
	cfg *config.Config, db *database.DB, enqueuer psimport.Enqueuer, reg *metrics.Registry,
) worker.HandlerFunc {
	if !psImportConfigured(cfg) {
		return nil
	}
	return newPSMigrateHandler(cfg, db, enqueuer, reg)
}

// newPSMigrateHandler returns the worker handler for ps_migrate jobs. It opens a
// fresh photo-sorter reader per run (migrations are rare, admin-triggered) so no
// photo-sorter connection pool is held for the process lifetime.
func newPSMigrateHandler(
	cfg *config.Config, db *database.DB, enqueuer psimport.Enqueuer, reg *metrics.Registry,
) worker.HandlerFunc {
	return func(ctx context.Context, _ jobs.Job) error {
		if _, err := runPSMigration(ctx, cfg, db, enqueuer, reg); err != nil {
			return fmt.Errorf("running photo-sorter migration: %w", err)
		}
		return nil
	}
}

// reportPSMigration prints a human-readable summary of a migration run.
func reportPSMigration(printf func(format string, a ...any), result psimport.Result) {
	printf("photosorter migration run %d: imported=%d updated=%d skipped=%d failed=%d\n",
		result.RunID, result.Counts.Imported, result.Counts.Updated,
		result.Counts.Skipped, result.Counts.Failed)
}
