package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/settingsapi"
)

// buildSettingsAPI assembles the instance-settings HTTP API over the shared pool:
// an anonymous caller learns three facts, any signed-in user reads the
// first-sign-in welcome text, and an administrator reads and replaces the full
// record including the readable registration secret.
//
// The three anonymous facts are whether self-service registration is open,
// whether this instance can run a passkey ceremony, and whether it sends mail at
// all. The last two come from the process rather than the settings row: the
// sign-in screen decides what to offer before anybody is signed in, and
// GET /capabilities — which carries the same passkey flag for the rest of the
// app — is behind auth. The mail flag is cfg.Mail.Enabled, the same switch that
// decides whether buildMailServiceOrNil builds a real SMTP sender, so the screens
// around registration promise an e-mail exactly where one is actually coming.
//
// The read guard and the admin guard are supplied via authAPI so settingsapi
// stays decoupled from auth's wiring, and an update is audited in the same
// transaction as the change by the store.
func buildSettingsAPI(cfg *config.Config, db *database.DB, authAPI *auth.API) *settingsapi.API {
	return settingsapi.NewAPI(settingsapi.Config{
		Store:        settings.NewStore(db.Pool()),
		Passkeys:     authAPI.PasskeysEnabled(),
		Mail:         cfg.Mail.Enabled,
		RequireAuth:  authAPI.RequireAuth,
		RequireAdmin: authAPI.RequireAdmin,
	})
}
