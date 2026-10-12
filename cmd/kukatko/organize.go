package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/storage"
)

// buildOrganizeAPI assembles the album and label catalogue HTTP API over the
// shared pool: listing albums/labels with photo counts, CRUD, album photo
// membership (add/remove/reorder), label attach/detach and the homepage's
// featured albums (whose default length is albums.featured.default_days and
// whose cover thumbnails are minted over mediaStore). Reads use the read guard
// and mutations the curator guard, both supplied via authAPI so organizeapi
// stays decoupled from auth's wiring. An album's or label's photos are browsed
// through the shared photo-list endpoint scoped by ?album= / ?label=. sidecar
// schedules a metadata-sidecar rewrite for each photo whose membership or labels
// changed — curation that lives nowhere but the database. The store reports a
// fresh feature to the album_featured scheduler, which queues the announcement
// job in the feature's transaction.
//
// An album's comment thread writes through the same comments store as a photo's
// and a task's (buildCommentStore, mentions announced), rate-limited from the
// same ratelimit.comment settings in a bucket of its own — one number to tune,
// and a burst of album talk cannot use up a person's allowance on photographs.
func buildOrganizeAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage, sidecar sidecarScheduler,
) *organizeapi.API {
	store := organize.NewStore(db.Pool()).WithFeatureObserver(buildAlbumFeaturedScheduler())
	limit := ratelimit.New(cfg.RateLimit.Comment.RatePerSec, cfg.RateLimit.Comment.Burst)
	return organizeapi.NewAPI(organizeapi.Config{
		Albums:              store,
		Labels:              store,
		Featured:            store,
		Comments:            buildCommentStore(cfg, db),
		CommentThrottle:     limit.KeyedMiddlewareExcept(commentRateKey, auth.RateLimitExempt),
		FeaturedDefaultDays: cfg.Albums.Featured.DefaultDays,
		Media:               mediaurl.NewBuilder(mediaStore),
		Sidecar:             sidecar,
		RequireAuth:         authAPI.RequireAuth,
		RequireCurator:      authAPI.RequireCurator,
	})
}
