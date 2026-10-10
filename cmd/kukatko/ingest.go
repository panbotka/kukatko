package main

import (
	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/uploadlink"
	"github.com/panbotka/kukatko/internal/uploadlinkapi"
)

// buildIngest assembles the upload/ingest subsystem: the configured original
// store, the thumbnailer, the photo repository, and the HTTP API. The upload
// route reuses the auth subsystem's curator guard (curators and above) supplied
// via authAPI. enqueuer is the shared persistent-queue adapter, so a freshly
// uploaded photo immediately gets its image_embed and face_detect jobs queued.
// sidecar queues its metadata-sidecar job too, so a photo is described on disk
// from the moment it is catalogued rather than only once someone edits it; it is
// nil when the sidecar export is switched off.
//
// The three remaining switchable jobs are resolved from cfg here rather than
// passed in, because nothing else needs them: the text recognition of a freshly
// uploaded still (never a video), the reverse geocode of a photo that arrives
// with coordinates — which is what makes its place fill in on its own — and the
// streaming encode of a freshly uploaded video (never a still). Each is a nil
// interface when its feature is off, so nothing is queued for a handler that is
// not registered.
//
// The processing service is what a re-upload of an already-catalogued file
// schedules the photo's missing steps through, so a photo whose first upload was
// cut short is completed by sending it again.
func buildIngest(
	cfg *config.Config, db *database.DB, authAPI *auth.API, enqueuer *jobs.Enqueuer,
	sidecar ingest.SidecarEnqueuer, reg *metrics.Registry,
) (func(chi.Router), error) {
	store, err := newStorage(cfg)
	if err != nil {
		return nil, err
	}
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg, db)...)
	photoStore := photos.NewStore(db.Pool())

	svc := ingest.New(ingest.Config{
		Storage:      store,
		Photos:       photoStore,
		Thumbnailer:  thumbnailer,
		Enqueuer:     enqueuer,
		Sidecar:      sidecar,
		OCR:          ocrEnqueuerOrNil(cfg, enqueuer),
		Places:       placesEnqueuerOrNil(cfg, enqueuer),
		HLS:          hlsEnqueuerOrNil(cfg, enqueuer),
		Pending:      buildProcessingService(cfg, db, jobs.NewStore(db.Pool()), enqueuer),
		Duplicate:    cfg.Duplicate,
		MaxFileSize:  cfg.Upload.MaxFileSizeBytes(),
		MaxPixels:    cfg.Thumb.MaxPixels,
		DecodeBudget: processDecodeBudget(cfg),
	})
	uploadLimit := ratelimit.New(cfg.RateLimit.Upload.RatePerSec, cfg.RateLimit.Upload.Burst)
	// Throttled by client IP, except for a request bearing an API token an admin
	// marked unlimited — an agent importing a shoebox of scans is the case the
	// limiter's burst was never meant to stop.
	ingestAPI := ingest.NewAPI(svc, authAPI.RequireCurator, uploadLimit.MiddlewareExcept(auth.RateLimitExempt))
	linkAPI := buildUploadLinkAPI(cfg, db, authAPI, svc, sidecar)
	return func(r chi.Router) {
		ingestAPI.RegisterRoutes(r)
		linkAPI.RegisterRoutes(r)
	}, nil
}

// buildUploadLinkAPI assembles the upload links: the curators' management
// routes and the public page + upload behind a short link. It shares the
// curator upload's pipeline (svc), so a file sent through a link is ingested,
// deduplicated and post-processed exactly like any other upload, and the
// sidecar scheduler, so a photo filed into a link's albums is described anew.
func buildUploadLinkAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, svc *ingest.Service,
	sidecar ingest.SidecarEnqueuer,
) *uploadlinkapi.API {
	return uploadlinkapi.NewAPI(uploadlinkapi.Config{
		Store:             uploadlink.NewStore(db.Pool()),
		Ingest:            svc,
		Sidecar:           sidecar,
		RequireCurator:    authAPI.RequireCurator,
		OptionalAuth:      authAPI.OptionalAuth,
		IPLimit:           ratelimit.New(cfg.RateLimit.UploadLink.RatePerSec, cfg.RateLimit.UploadLink.Burst),
		LinkLimit:         ratelimit.New(cfg.RateLimit.UploadLinkPerLink.RatePerSec, cfg.RateLimit.UploadLinkPerLink.Burst),
		MaxFileSize:       cfg.UploadLinks.MaxFileSizeBytes(),
		MaxUploadsPerLink: cfg.UploadLinks.MaxUploadsPerLink,
		DefaultDays:       cfg.UploadLinks.DefaultDays,
		MaxDays:           cfg.UploadLinks.MaxDays,
		SecureCookies:     cfg.Web.SecureCookies,
	})
}
