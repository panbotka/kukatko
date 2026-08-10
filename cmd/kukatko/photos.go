package main

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/metrics"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/places"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storyboardjob"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/thumbjob"
	"github.com/panbotka/kukatko/internal/vectors"
)

// buildFaceMatch assembles the face-matching service (face↔marker IoU matching,
// the assignment state machine and identity suggestions) over the shared pool. It
// is shared by the photo faces endpoints and the auto-clustering service, which
// reuses its assignment state machine to name a whole cluster.
func buildFaceMatch(cfg *config.Config, db *database.DB) *facematch.Service {
	return facematch.New(facematch.Config{
		Photos:                photos.NewStore(db.Pool()),
		Faces:                 vectors.NewStore(db.Pool()),
		People:                people.NewStore(db.Pool()),
		IoUThreshold:          cfg.Faces.IoUThreshold,
		SuggestionLimit:       cfg.Faces.SuggestionLimit,
		SuggestionMaxDistance: cfg.Faces.SuggestionMaxDistance,
		MinFaceSize:           cfg.Faces.MinFaceSize,
	})
}

// buildPhotoAPI assembles the photo browse/curation subsystem: the configured
// original store and thumbnailer (for media serving), the photo repository, and
// the HTTP API. Read endpoints reuse the auth subsystem's RequireAuth guard,
// metadata and archive endpoints its RequireWrite guard, the permanent trash
// operations (purge one, empty the trash) its RequireAdmin guard (destroying
// originals is tightened above write), and media endpoints its
// RequireAuthOrDownloadToken guard (cookie or download token) — all supplied via
// authAPI so the photoapi package stays decoupled from auth's wiring. similar is
// the shared vector store backing the similar-photos endpoint and the semantic
// half of search; embedder is the sidecar client that embeds query text for
// semantic and hybrid search. faceSvc backs the /photos/{uid}/faces endpoints.
// store is the shared originals backend, which also decides whether the media
// routes stream bytes or redirect to signed edge URLs. storyboards backs the
// video scrub-preview routes (status + sprite) and is the same service the worker
// renders through.
func buildPhotoAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, store storage.Storage,
	similar photoapi.SimilarSearcher, embedder photoapi.TextEmbedder, faceSvc *facematch.Service,
	purger photoapi.Purger, sidecar sidecarScheduler, storyboards *storyboardjob.Service,
	reg *metrics.Registry,
) *photoapi.API {
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, reg)...)
	photoStore := photos.NewStore(db.Pool())
	organizeStore := organize.NewStore(db.Pool())
	// The detail endpoint resolves a photo's uploader UID to a display name via
	// the auth store; keep it behind photoapi.UserResolver so the package stays
	// decoupled from auth's wiring.
	userStore := auth.NewStore(db.Pool())
	// The regenerate-thumbnail action reuses the thumbnail job's regeneration
	// logic (thumbnailer + original decoder) so a stale/broken thumbnail can be
	// rebuilt on demand without duplicating the pipeline.
	regenerator := thumbjob.New(thumbjob.Config{
		Photos:      photoStore,
		Thumbnailer: thumbnailer,
		Decoder:     thumbjob.NewStorageDecoder(store),
	})
	// A nil interface (not a typed nil pointer) when stacking is disabled, so the
	// photoapi nil check answers 503 on the manual stacking routes.
	var stacker photoapi.Stacker
	if s := buildStacksServiceOrNil(cfg, db); s != nil {
		stacker = s
	}
	commentLimit := ratelimit.New(cfg.RateLimit.Comment.RatePerSec, cfg.RateLimit.Comment.Burst)

	return photoapi.NewAPI(photoapi.Config{
		Store:       photoStore,
		Storage:     store,
		Thumbnailer: thumbnailer,
		Regenerator: regenerator,
		Sidecar:     sidecar,
		Audit:       audit.NewStore(db.Pool()),
		Similar:     similar,
		Embedder:    embedder,
		Faces:       faceSvc,
		Favorites:   organizeStore,
		Ratings:     organizeStore,
		Organizer:   organizeStore,
		Users:       userStore,
		// The detail response carries the photo's cached place. This is a read of
		// the photo_places cache the `places` job fills — the detail endpoint never
		// geocodes, so opening a photo costs no mapy.com credit.
		Places:  places.NewStore(db.Pool()),
		Purger:  purger,
		Stacker: stacker,
		// Per-photo comment threads. Writing one is open to every authenticated
		// role (viewers included), so the throttle keys on the user rather than
		// the client IP — see photoapi.handleCreateComment.
		Comments:         comments.NewStore(db.Pool()),
		Storyboards:      storyboards,
		CommentRateLimit: commentLimit.KeyedMiddleware(commentRateKey),
		RetentionDays:    cfg.Trash.RetentionDays,
		VideoTranscode:   cfg.Video.Transcode,
		RequireAuth:      authAPI.RequireAuth,
		RequireWrite:     authAPI.RequireWrite,
		RequireAdmin:     authAPI.RequireAdmin,
		RequireDownload:  authAPI.RequireAuthOrDownloadToken,
	})
}

// commentRateKey is the rate-limit bucket key for comment creation: the acting
// user's UID, read from the auth context the guard has already populated (the
// limiter is mounted inside it). An unauthenticated request cannot reach the
// handler, so the empty fallback is unreachable in production and merely keeps
// the key function total.
func commentRateKey(r *http.Request) string {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		return user.UID
	}
	return ""
}
