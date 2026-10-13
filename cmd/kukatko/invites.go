package main

import (
	"strings"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/invite"
	"github.com/panbotka/kukatko/internal/inviteapi"
	"github.com/panbotka/kukatko/internal/ratelimit"
)

// buildInviteAPI assembles the invitation links over the shared pool: a project
// admin's management of their project's invites (/p/{project}/invites, behind
// RequireAdmin) and the instance-level public page and accept
// (/invite/{code}), throttled per client IP by ratelimit.invite. An invite's
// URL is built from mail.base_url, the instance's public address; without one
// it is a path relative to the host.
func buildInviteAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *inviteapi.API {
	return inviteapi.NewAPI(inviteapi.Config{
		Store:           invite.NewStore(db.Pool()),
		BaseURL:         strings.TrimSpace(cfg.Mail.BaseURL),
		RequireAdmin:    authAPI.RequireAdmin,
		OptionalAuth:    authAPI.OptionalAuth,
		RequireSignedIn: authAPI.RequireSignedIn,
		IPLimit:         ratelimit.New(cfg.RateLimit.Invite.RatePerSec, cfg.RateLimit.Invite.Burst),
	})
}

// inviteGate builds what lets an invitation link stand in for the shared
// registration secret (and the approval) at POST /auth/register.
func inviteGate(db *database.DB) *inviteapi.RegistrationGate {
	return inviteapi.NewRegistrationGate(invite.NewStore(db.Pool()))
}
