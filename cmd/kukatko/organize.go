package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
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
// changed — curation that lives nowhere but the database.
func buildOrganizeAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, mediaStore storage.Storage, sidecar sidecarScheduler,
) *organizeapi.API {
	store := organize.NewStore(db.Pool())
	return organizeapi.NewAPI(organizeapi.Config{
		Albums:              store,
		Labels:              store,
		Featured:            store,
		FeaturedDefaultDays: cfg.Albums.Featured.DefaultDays,
		Media:               mediaurl.NewBuilder(mediaStore),
		Sidecar:             sidecar,
		RequireAuth:         authAPI.RequireAuth,
		RequireCurator:      authAPI.RequireCurator,
	})
}
