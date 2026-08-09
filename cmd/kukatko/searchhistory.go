package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/searchhistory"
	"github.com/panbotka/kukatko/internal/searchhistoryapi"
)

// buildSearchHistoryAPI assembles the per-user search-history HTTP API over the
// shared pool: listing what the caller searched for recently, recording a query
// that was just run, and clearing the lot. The read guard is supplied via authAPI
// (so searchhistoryapi stays decoupled from auth's wiring), and every operation is
// scoped to the acting user, so the history is readable and writable by its owner
// alone.
func buildSearchHistoryAPI(db *database.DB, authAPI *auth.API) *searchhistoryapi.API {
	return searchhistoryapi.NewAPI(searchhistoryapi.Config{
		Store:       searchhistory.NewStore(db.Pool()),
		RequireAuth: authAPI.RequireAuth,
	})
}
