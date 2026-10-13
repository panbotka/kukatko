package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/memberapi"
	"github.com/panbotka/kukatko/internal/project"
)

// buildMemberAPI assembles a project admin's management of their project's
// members (/p/{project}/members) over the shared pool. Every route is behind
// authAPI's RequireAdmin, every mutation is audited by the project store in
// the same transaction, and an approval schedules the "you may come in" mail —
// naming the project and linking to its page — through the queue on that same
// transaction, so an instance with mail off approves and sends nothing.
func buildMemberAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *memberapi.API {
	baseURL := cfg.Mail.BaseURL
	return memberapi.NewAPI(memberapi.Config{
		Members:      project.NewStore(db.Pool()),
		Accounts:     auth.NewStore(db.Pool()),
		Mail:         mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: cfg.Mail.Enabled}),
		ProjectURL:   func(slug string) string { return projectURL(baseURL, slug) },
		RequireAdmin: authAPI.RequireAdmin,
	})
}
