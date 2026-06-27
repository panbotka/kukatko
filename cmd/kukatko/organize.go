package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/organizeapi"
)

// buildOrganizeAPI assembles the album and label catalogue HTTP API over the
// shared pool: listing albums/labels with photo counts, CRUD, album photo
// membership (add/remove/reorder) and label attach/detach. Reads use the read
// guard and mutations the write guard, both supplied via authAPI so organizeapi
// stays decoupled from auth's wiring. An album's or label's photos are browsed
// through the shared photo-list endpoint scoped by ?album= / ?label=.
func buildOrganizeAPI(db *database.DB, authAPI *auth.API) *organizeapi.API {
	store := organize.NewStore(db.Pool())
	return organizeapi.NewAPI(organizeapi.Config{
		Albums:       store,
		Labels:       store,
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
	})
}
