package main

import (
	"fmt"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// buildIngest assembles the upload/ingest subsystem: the on-disk original
// store, the thumbnailer, the photo repository, and the HTTP API. The upload
// route reuses the auth subsystem's write guard (editors and admins) supplied
// via authAPI. enqueuer is the shared persistent-queue adapter, so a freshly
// uploaded photo immediately gets its image_embed and face_detect jobs queued.
func buildIngest(
	cfg *config.Config, db *database.DB, authAPI *auth.API, enqueuer ingest.JobEnqueuer, reg *metrics.Registry,
) (*ingest.API, error) {
	store, err := storage.NewFS(cfg.Storage.OriginalsPath)
	if err != nil {
		return nil, fmt.Errorf("initialising originals storage: %w", err)
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
	photoStore := photos.NewStore(db.Pool())

	svc := ingest.New(ingest.Config{
		Storage:     store,
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Enqueuer:    enqueuer,
		Duplicate:   cfg.Duplicate,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
	})
	uploadLimit := ratelimit.New(cfg.RateLimit.Upload.RatePerSec, cfg.RateLimit.Upload.Burst)
	return ingest.NewAPI(svc, authAPI.RequireWrite, uploadLimit.Middleware), nil
}
