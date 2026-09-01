package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/avatarapi"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

// buildAvatarAPI assembles the subject avatar endpoint: the people store (which
// picture stands for a subject), the photo repository (that picture's record) and
// the renderer that cuts the small square rendition out of a cached thumbnail and
// keeps it in the derived-media cache. The read guard comes from authAPI so
// avatarapi stays decoupled from auth's wiring.
//
// store is the shared originals backend the thumbnailer reads its sources
// through. The thumbnailer is built without the metrics registry, unlike the one
// the media routes use: this one only ever *reads* an existing preview, and the
// rare cache miss it does encode is a repair of somebody else's pruned cache
// rather than thumbnail work worth timing under that name.
func buildAvatarAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, store storage.Storage,
) *avatarapi.API {
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, nil, db)...)
	return avatarapi.NewAPI(avatarapi.Config{
		Subjects:    people.NewStore(db.Pool()),
		Photos:      photos.NewStore(db.Pool()),
		Renderer:    avatar.New(thumbnailer, cfg.Storage.CachePath),
		RequireAuth: authAPI.RequireAuth,
	})
}
