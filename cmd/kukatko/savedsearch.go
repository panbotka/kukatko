package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/savedsearch"
	"github.com/panbotka/kukatko/internal/savedsearchapi"
)

// buildSavedSearchAPI assembles the per-user saved-search ("smart album") HTTP API
// over the shared pool: a signed-in user creating, listing, reading, editing and
// deleting their own named filter/search definitions. Every endpoint uses the read
// guard supplied via authAPI (so savedsearchapi stays decoupled from auth's
// wiring), and each operation is scoped to the acting user.
func buildSavedSearchAPI(db *database.DB, authAPI *auth.API) *savedsearchapi.API {
	return savedsearchapi.NewAPI(savedsearchapi.Config{
		Store:       savedsearch.NewStore(db.Pool()),
		RequireAuth: authAPI.RequireAuth,
	})
}
