package main

import (
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/settingsapi"
)

// buildSettingsAPI assembles the instance-settings HTTP API over the shared pool:
// an anonymous caller learns only whether self-service registration is open and
// whether this instance can run a passkey ceremony (the sign-in screen decides
// what to offer before anybody is signed in, and GET /capabilities — which
// carries the same passkey flag for the rest of the app — is behind auth), any
// signed-in user reads the first-sign-in welcome text, and an administrator reads
// and replaces the full record including the readable registration secret. The
// read guard and the admin guard are supplied via authAPI so settingsapi stays
// decoupled from auth's wiring, and an update is audited in the same transaction
// as the change by the store.
func buildSettingsAPI(db *database.DB, authAPI *auth.API) *settingsapi.API {
	return settingsapi.NewAPI(settingsapi.Config{
		Store:        settings.NewStore(db.Pool()),
		Passkeys:     authAPI.PasskeysEnabled(),
		RequireAuth:  authAPI.RequireAuth,
		RequireAdmin: authAPI.RequireAdmin,
	})
}
