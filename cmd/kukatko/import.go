package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/importapi"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/thumb"
)

// importConfigured reports whether the PhotoPrism import is enabled, i.e. a base
// URL is set. Callers gate building the import service and registering its job
// handler and HTTP trigger on this.
func importConfigured(cfg *config.Config) bool {
	return cfg.Import.PhotoPrism.BaseURL != ""
}

// buildImportService assembles the PhotoPrism import pipeline over the shared
// pool: the read-only PhotoPrism client, the import-run store, the photo
// catalogue, on-disk storage and thumbnailer, the album/label/people catalogues
// and the job enqueuer. The caller must ensure the import is configured
// (importConfigured) before calling; an empty base URL yields a client error.
func buildImportService(
	cfg *config.Config, db *database.DB, enqueuer ppimport.Enqueuer, reg *metrics.Registry,
) (*ppimport.Service, error) {
	client, err := photoprism.New(photoprism.Config{
		BaseURL: cfg.Import.PhotoPrism.BaseURL,
		Token:   cfg.Import.PhotoPrism.Token,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising photoprism client: %w", err)
	}
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	pool := db.Pool()
	return ppimport.New(ppimport.Config{
		Client:      client,
		Runs:        importer.NewStore(pool),
		Photos:      photos.NewStore(pool),
		Storage:     store,
		Thumbnailer: thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...),
		Albums:      organize.NewStore(pool),
		Labels:      organize.NewStore(pool),
		People:      people.NewStore(pool),
		Enqueuer:    enqueuer,
		PageSize:    cfg.Import.PhotoPrism.PageSize,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
		Metrics:     importObserver(reg),
	}), nil
}

// buildImportAPI assembles the HTTP API for imports: the run-history endpoint
// (always registered) and the pp_import/ps_migrate triggers, which are
// registered only for configured sources. Triggers enqueue onto the shared queue
// store; history reads the import_runs table over the shared pool. The maintainer
// guard is supplied via authAPI so importapi stays decoupled from auth's wiring;
// imports are an operations capability at the top of the ladder.
func buildImportAPI(cfg *config.Config, db *database.DB, store *jobs.Store, authAPI *auth.API) *importapi.API {
	importLimit := ratelimit.New(cfg.RateLimit.Import.RatePerSec, cfg.RateLimit.Import.Burst)
	return importapi.NewAPI(importapi.Config{
		Queue:             store,
		Runs:              importer.NewStore(db.Pool()),
		RequireMaintainer: authAPI.RequireMaintainer,
		RateLimit:         importLimit.Middleware,
		EnablePhotoPrism:  importConfigured(cfg),
		EnablePhotoSorter: psImportConfigured(cfg),
		EnableFeeds:       psFeedsConfigured(cfg),
	})
}
