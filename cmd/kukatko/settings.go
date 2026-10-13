package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/settingsapi"
)

// buildSettingsAPI assembles the settings HTTP API over the shared pool. Under
// /p/{project}: anybody may learn the project's name and whether it takes
// registrations (its registration page asks before there is an account), any
// member reads the project's first-sign-in welcome text, and the project's
// administrator reads and replaces the full record including the readable
// registration secret, and reads the project's usage beside its limits.
// Instance-level: an anonymous caller learns whether this instance can run a
// passkey ceremony and whether it sends mail at all.
//
// The two instance facts come from the process rather than the settings row:
// the sign-in screen decides what to offer before anybody is signed in, and
// GET /capabilities — which carries the same passkey flag for the rest of the
// app — is behind auth. The mail flag is cfg.Mail.Enabled, the same switch that
// decides whether buildMailServiceOrNil builds a real SMTP sender, so the
// screens around registration promise an e-mail exactly where one is actually
// coming.
//
// The guards are supplied via authAPI so settingsapi stays decoupled from
// auth's wiring, and an update is audited in the same transaction as the change
// by the store.
func buildSettingsAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *settingsapi.API {
	return settingsapi.NewAPI(settingsapi.Config{
		Store:                settings.NewStore(db.Pool()),
		Usage:                project.NewStore(db.Pool()),
		Passkeys:             authAPI.PasskeysEnabled(),
		Mail:                 cfg.Mail.Enabled,
		RequireActiveProject: auth.RequireActiveProject,
		RequireAuth:          authAPI.RequireAuth,
		RequireAdmin:         authAPI.RequireAdmin,
	})
}
