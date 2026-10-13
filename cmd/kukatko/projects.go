package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projectapi"
)

// buildProjectsAPI assembles the system administrator's instance-level project
// and account management (/projects, /users) over the shared pool. Every route
// is behind authAPI's RequireMaintainer, and every mutation is audited by the
// store that makes it, in the same transaction.
func buildProjectsAPI(db *database.DB, authAPI *auth.API) *projectapi.API {
	return projectapi.NewAPI(projectapi.Config{
		Projects:          project.NewStore(db.Pool()),
		Accounts:          auth.NewStore(db.Pool()),
		RequireMaintainer: authAPI.RequireMaintainer,
	})
}
