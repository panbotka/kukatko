package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/whatsnew"
	"github.com/panbotka/kukatko/internal/whatsnewapi"
)

// buildWhatsNewAPI assembles GET /whats-new over the shared pool: the digest of
// what happened in the library since the caller's previous visit, readable by
// every signed-in role. The read guard is supplied via authAPI so whatsnewapi
// stays decoupled from auth's wiring, and the store keeps the per-account visit
// bookkeeping the digest is measured from.
func buildWhatsNewAPI(db *database.DB, authAPI *auth.API) *whatsnewapi.API {
	return whatsnewapi.NewAPI(whatsnewapi.Config{
		Store:       whatsnew.NewStore(db.Pool()),
		RequireAuth: authAPI.RequireAuth,
	})
}
