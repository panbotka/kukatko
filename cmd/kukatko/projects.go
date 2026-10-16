package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projectapi"
)

// buildProjectsAPI assembles the system administrator's instance-level project
// and account management (/projects, /users) over the shared pool. Every route
// is behind authAPI's RequireMaintainer, and every mutation is audited by the
// store that makes it, in the same transaction.
//
// A project set up for somebody else (POST /projects with a first admin) mails
// its admin through the queue on the creating transaction, with the
// set-password link on this instance's password-reset page and the project's
// own address — so an instance with mail off sets everything up, sends nothing
// and hands the administrator the link instead.
func buildProjectsAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *projectapi.API {
	store := auth.NewStore(db.Pool())
	baseURL := cfg.Mail.BaseURL
	setup := auth.NewProjectSetup(auth.ProjectSetupConfig{
		Service:    auth.NewService(store, auth.SessionPolicy{}),
		Mail:       mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: cfg.Mail.Enabled}),
		LinkBase:   passwordResetURL(baseURL),
		ProjectURL: func(slug string) string { return projectURL(baseURL, slug) },
	})
	return projectapi.NewAPI(projectapi.Config{
		Projects:          project.NewStore(db.Pool()),
		Accounts:          store,
		Setup:             setup,
		RequireMaintainer: authAPI.RequireMaintainer,
	})
}
