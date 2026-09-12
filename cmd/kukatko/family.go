package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyapi"
)

// buildFamilyAPI assembles the genealogy HTTP API over the shared pool: one
// subject's immediate relations, the tree walked up or down from them, recording
// and removing a relation, and editing a family row. Reads use the read guard and
// mutations the write guard, both supplied via authAPI so familyapi stays
// decoupled from auth's wiring.
func buildFamilyAPI(db *database.DB, authAPI *auth.API) *familyapi.API {
	return familyapi.NewAPI(familyapi.Config{
		Store:        family.NewStore(db.Pool()),
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
	})
}
