package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/avatar"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/userpic"
	"github.com/panbotka/kukatko/internal/userpicapi"
)

// buildUserPicAPI assembles the profile picture endpoints: the chain that
// decides what stands for an account (an uploaded picture, a picked photo, the
// linked person's face, nothing) and the renderer that cuts the square for the
// two of those that are a photograph.
//
// It builds its own thumbnailer for the same reason buildAvatarAPI does — this
// path only ever reads an existing preview, and the rare cache miss it encodes
// is a repair of a pruned cache rather than thumbnail work worth timing — and it
// shares the renderer's cache directory, so a picked photo already cut for a
// subject avatar is not cut twice.
func buildUserPicAPI(
	cfg *config.Config, db *database.DB, authAPI *auth.API, store storage.Storage,
) *userpicapi.API {
	pool := db.Pool()
	thumbnailer := thumb.New(store, cfg.Storage.CachePath, thumbOptions(cfg, nil, db)...)
	photoStore := photos.NewStore(pool)
	return userpicapi.NewAPI(userpicapi.Config{
		Pictures: userpic.NewService(userpic.Config{
			Pictures: userpic.NewStore(pool),
			Users:    auth.NewStore(pool),
			Subjects: people.NewStore(pool),
			Photos:   photoStore,
		}),
		Photos:      photoStore,
		Renderer:    avatar.New(thumbnailer, cfg.Storage.CachePath),
		RequireAuth: authAPI.RequireAuth,
	})
}
