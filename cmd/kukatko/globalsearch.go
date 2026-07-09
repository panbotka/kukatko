package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/globalsearchapi"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// buildGlobalSearchAPI assembles the grouped global-search HTTP API over the
// shared pool: a signed-in user querying albums, labels, people and photos in one
// request for the navbar quick-results and the search page. It reuses the album,
// label, subject and photo stores (the last via the existing full-text search),
// and takes the read guard from authAPI so globalsearchapi stays decoupled from
// auth's wiring. mediaStore decides where a client fetches the matched photos'
// thumbnails and originals.
func buildGlobalSearchAPI(
	db *database.DB, authAPI *auth.API, mediaStore storage.Storage,
) *globalsearchapi.API {
	return globalsearchapi.NewAPI(globalsearchapi.Config{
		Organizer:   organize.NewStore(db.Pool()),
		People:      people.NewStore(db.Pool()),
		Photos:      photos.NewStore(db.Pool()),
		Storage:     mediaStore,
		RequireAuth: authAPI.RequireAuth,
	})
}
