package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/importapi"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ppimport"
	"github.com/panbotka/kukatko/internal/storage"
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
	cfg *config.Config, db *database.DB, enqueuer ppimport.Enqueuer,
) (*ppimport.Service, error) {
	client, err := photoprism.New(photoprism.Config{
		BaseURL: cfg.Import.PhotoPrism.BaseURL,
		Token:   cfg.Import.PhotoPrism.Token,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising photoprism client: %w", err)
	}
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	pool := db.Pool()
	return ppimport.New(ppimport.Config{
		Client:      client,
		Runs:        importer.NewStore(pool),
		Photos:      photos.NewStore(pool),
		Storage:     store,
		Thumbnailer: thumb.New(store, cfg.Storage.CachePath),
		Albums:      organize.NewStore(pool),
		Labels:      organize.NewStore(pool),
		People:      people.NewStore(pool),
		Enqueuer:    enqueuer,
		PageSize:    cfg.Import.PhotoPrism.PageSize,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
	}), nil
}

// buildImportAPI assembles the admin-only HTTP trigger that enqueues a pp_import
// job on the shared queue store. The admin guard is supplied via authAPI so
// importapi stays decoupled from auth's wiring.
func buildImportAPI(store *jobs.Store, authAPI *auth.API) *importapi.API {
	return importapi.NewAPI(importapi.Config{
		Queue:        store,
		RequireAdmin: authAPI.RequireAdmin,
	})
}
