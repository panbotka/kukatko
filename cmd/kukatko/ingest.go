package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// buildIngest assembles the upload/ingest subsystem: the on-disk original
// store, the thumbnailer, the photo repository, and the HTTP API. The upload
// route reuses the auth subsystem's write guard (editors and admins) supplied
// via authAPI. The job enqueuer defaults to the no-op hook until the persistent
// job queue exists (see ingest.NopEnqueuer).
func buildIngest(cfg *config.Config, db *database.DB, authAPI *auth.API) (*ingest.API, error) {
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath)
	photoStore := photos.NewStore(db.Pool())

	svc := ingest.New(ingest.Config{
		Storage:     store,
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Duplicate:   cfg.Duplicate,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
	})
	return ingest.NewAPI(svc, authAPI.RequireWrite), nil
}
