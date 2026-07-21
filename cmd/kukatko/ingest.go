package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/thumb"
)

// buildIngest assembles the upload/ingest subsystem: the configured original
// store, the thumbnailer, the photo repository, and the HTTP API. The upload
// route reuses the auth subsystem's write guard (editors and admins) supplied
// via authAPI. enqueuer is the shared persistent-queue adapter, so a freshly
// uploaded photo immediately gets its image_embed and face_detect jobs queued.
// sidecar queues its metadata-sidecar job too, so a photo is described on disk
// from the moment it is catalogued rather than only once someone edits it; it is
// nil when the sidecar export is switched off.
func buildIngest(
	cfg *config.Config, db *database.DB, authAPI *auth.API, enqueuer ingest.JobEnqueuer,
	sidecar ingest.SidecarEnqueuer, reg *metrics.Registry,
) (*ingest.API, error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
	photoStore := photos.NewStore(db.Pool())

	svc := ingest.New(ingest.Config{
		Storage:     store,
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Enqueuer:    enqueuer,
		Sidecar:     sidecar,
		Duplicate:   cfg.Duplicate,
		MaxFileSize: cfg.Upload.MaxFileSizeBytes(),
		MaxPixels:   cfg.Thumb.MaxPixels,
	})
	uploadLimit := ratelimit.New(cfg.RateLimit.Upload.RatePerSec, cfg.RateLimit.Upload.Burst)
	return ingest.NewAPI(svc, authAPI.RequireWrite, uploadLimit.Middleware), nil
}
