package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/peopleapi"
	"github.com/panbotka/kukatko/internal/photos"
)

// buildPeopleAPI assembles the subject (people/pet/other) catalogue HTTP API over
// the shared pool: listing subjects with photo counts, fetching/editing a single
// subject, and paging a subject's photos. Reads use the read guard and mutations
// the write guard, both supplied via authAPI so peopleapi stays decoupled from
// auth's wiring.
func buildPeopleAPI(db *database.DB, authAPI *auth.API) *peopleapi.API {
	return peopleapi.NewAPI(peopleapi.Config{
		Subjects:     people.NewStore(db.Pool()),
		Photos:       photos.NewStore(db.Pool()),
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
	})
}
