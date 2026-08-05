package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/dupmarkersapi"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/people"
)

// buildDupMarkersAPI assembles the "one person marked more than once on a photo"
// curation API over the shared pool: the read-only finder (repeated valid face
// markers of named subjects, minus the groups a user already settled via the
// feedback store) plus the two repairs behind it.
//
// Both repairs reuse existing write paths rather than adding their own —
// detaching a marker goes through the same facematch service the photo detail and
// the review game use, and the invalid flag through people's audited store method
// — so a fix made here is indistinguishable from the same fix made anywhere else,
// audit entry included. There is no config switch: the query is cheap and the
// findings are always mistakes. The guards come via authAPI so dupmarkersapi
// stays decoupled from auth's wiring.
func buildDupMarkersAPI(
	db *database.DB, authAPI *auth.API, matchSvc *facematch.Service,
) *dupmarkersapi.API {
	svc := dupmarkers.New(dupmarkers.Config{
		Markers:    dupmarkers.NewStore(db.Pool()),
		Dismissals: feedback.NewStore(db.Pool()),
	})
	return dupmarkersapi.NewAPI(dupmarkersapi.Config{
		Service:      svc,
		Markers:      people.NewStore(db.Pool()),
		Assigner:     matchSvc,
		RequireAuth:  authAPI.RequireAuth,
		RequireWrite: authAPI.RequireWrite,
	})
}
