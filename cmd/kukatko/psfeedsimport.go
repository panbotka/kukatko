package main

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/psfeeds"
	"github.com/panbotka/kukatko/internal/psfeedsimport"
	"github.com/panbotka/kukatko/internal/vectors"
	"github.com/panbotka/kukatko/internal/worker"
)

// psFeedsConfigured reports whether the photo-sorter feeds import is enabled, i.e.
// a feeds API base URL is set. Callers gate building the import command, its job
// handler and its HTTP trigger on this.
func psFeedsConfigured(cfg *config.Config) bool {
	return cfg.Import.PhotoSorter.BaseURL != ""
}

// newPSFeedsService assembles the photo-sorter feeds importer over the shared
// pool and a read-only HTTP feeds client: the import-run store, the photo
// catalogue (to resolve targets by photoprism_uid), the embeddings/faces store
// and the people store (to materialise subjects and markers). The caller must
// ensure the feeds import is configured (psFeedsConfigured) first.
func newPSFeedsService(cfg *config.Config, db *database.DB) (*psfeedsimport.Service, error) {
	client, err := psfeeds.New(psfeeds.Config{
		BaseURL: cfg.Import.PhotoSorter.BaseURL,
		Token:   cfg.Import.PhotoSorter.Token,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising photo-sorter feeds client: %w", err)
	}
	pool := db.Pool()
	return psfeedsimport.New(psfeedsimport.Config{
		Feeds:    client,
		Photos:   photos.NewStore(pool),
		Vectors:  vectors.NewStore(pool),
		People:   people.NewStore(pool),
		Runs:     importer.NewStore(pool),
		PageSize: cfg.Import.PhotoSorter.PageSize,
	}), nil
}

// runPSFeedsImport builds the feeds importer and runs one full enrichment pass. It
// is shared by the CLI command and the background ps_feeds_import job handler.
func runPSFeedsImport(ctx context.Context, cfg *config.Config, db *database.DB) (psfeedsimport.Result, error) {
	svc, err := newPSFeedsService(cfg, db)
	if err != nil {
		return psfeedsimport.Result{}, err
	}
	result, err := svc.Import(ctx)
	if err != nil {
		return psfeedsimport.Result{}, fmt.Errorf("running photo-sorter feeds import: %w", err)
	}
	return result, nil
}

// psFeedsHandlerOrNil returns the ps_feeds_import worker handler when the feeds
// import is configured, or nil otherwise. Keeping the gate here keeps
// buildServices' branch count down.
func psFeedsHandlerOrNil(cfg *config.Config, db *database.DB) worker.HandlerFunc {
	if !psFeedsConfigured(cfg) {
		return nil
	}
	return func(ctx context.Context, _ jobs.Job) error {
		if _, err := runPSFeedsImport(ctx, cfg, db); err != nil {
			return fmt.Errorf("running photo-sorter feeds import: %w", err)
		}
		return nil
	}
}

// reportPSFeedsImport prints a human-readable summary of a feeds import run.
func reportPSFeedsImport(printf func(format string, a ...any), result psfeedsimport.Result) {
	printf("photosorter feeds import run %d: imported=%d skipped=%d failed=%d\n",
		result.RunID, result.Counts.Imported, result.Counts.Skipped, result.Counts.Failed)
}
