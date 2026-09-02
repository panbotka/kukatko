package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/settings"
)

// buildAuth assembles the auth subsystem from configuration and the database:
// the store, the session service, the login rate limiter, the self-service
// registration flow, the password-reset flow, the passkey flow, and the HTTP
// API. The returned API mounts the auth routes;
// the returned Service is used for bootstrap and the background session-cleanup
// loop. It fails only on a passkey relying party the WebAuthn library refuses.
//
// Registration is always wired, because whether it is open is a runtime decision
// an administrator makes in the instance settings, not a deployment one: with it
// switched off the endpoint refuses every caller, and switching it on needs no
// restart. Its mails go through the queue like every other message, so an
// instance with mail disabled registers people and sends nothing.
func buildAuth(cfg *config.Config, db *database.DB) (*auth.API, *auth.Service, error) {
	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{
		TTL:         cfg.Auth.SessionTTL,
		MaxLifetime: cfg.Auth.SessionMaxLifetime,
	})
	limiter := auth.NewLimiter(cfg.Auth.LoginRateLimit, cfg.Auth.LoginRateWindow)
	mail := mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: cfg.Mail.Enabled})
	registration := auth.NewRegistration(auth.RegistrationConfig{
		Service:  svc,
		Settings: settings.NewStore(db.Pool()),
		Mail:     mail,
	})
	approval := auth.NewApproval(auth.ApprovalConfig{
		Service:   svc,
		Mail:      mail,
		SignInURL: signInURL(cfg.Mail.BaseURL),
	})
	passwordReset := auth.NewPasswordReset(auth.PasswordResetConfig{
		Service:  svc,
		Mail:     mail,
		LinkBase: passwordResetURL(cfg.Mail.BaseURL),
	})
	// Passkeys are wired only when this instance is a relying party at all;
	// otherwise the flow stays nil and the endpoints answer "not available",
	// which is what "cleanly off" means here.
	var passkeys *auth.Passkeys
	if rp := cfg.Passkey(); rp.Enabled {
		var err error
		if passkeys, err = buildPasskeys(rp, svc); err != nil {
			return nil, nil, err
		}
	}
	api := auth.NewAPI(auth.APIConfig{
		Service:       svc,
		Limiter:       limiter,
		Registration:  registration,
		Approval:      approval,
		PasswordReset: passwordReset,
		Passkeys:      passkeys,
		SecureCookies: cfg.Web.SecureCookies,
	})
	return api, svc, nil
}

// buildPasskeys assembles the WebAuthn sign-in flow for the resolved relying
// party rp, which the caller has already established this instance has (see
// config.Config.Passkey — an instance with neither auth.passkey.rp_id/origins nor
// the mail.base_url they fall back to simply does not offer passkeys, and the
// endpoints say so).
//
// A configured relying party the WebAuthn library refuses *is* a failure, and
// deliberately a startup one: silently degrading to "no passkeys" would leave an
// operator who configured the feature staring at an interface that never offers
// it, and — worse, once anybody has registered a key — at authenticators that
// have quietly stopped working.
func buildPasskeys(rp config.RelyingParty, svc *auth.Service) (*auth.Passkeys, error) {
	passkeys, err := auth.NewPasskeys(auth.PasskeysConfig{
		Service:       svc,
		RPID:          rp.ID,
		RPDisplayName: rp.DisplayName,
		Origins:       rp.Origins,
	})
	if err != nil {
		return nil, fmt.Errorf("building the passkey relying party: %w", err)
	}
	return passkeys, nil
}

// signInPath is the frontend route of the sign-in screen (see web/src/App.tsx).
const signInPath = "/login"

// signInURL builds the address the approval mail points at from this instance's
// public URL, tolerating a trailing slash. An empty base yields an empty URL —
// mail.base_url is required whenever mail is enabled, so the only instances that
// reach that case are the ones that send nothing anyway, and inventing a host
// would send somebody to the wrong one.
func signInURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + signInPath
}

// passwordResetPath is the frontend route where somebody follows a reset link
// and chooses a new password; the token is the last path segment.
const passwordResetPath = "/password-reset"

// passwordResetURL builds the base of a reset link from this instance's public
// URL, tolerating a trailing slash. An empty base yields an empty one, which
// makes the auth package fall back to the site-relative path — the honest answer
// for an instance nobody told its own address, and still a link an administrator
// can paste after their host.
func passwordResetURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + passwordResetPath
}

// runBootstrap creates the initial admin account if the users table is empty and
// bootstrap credentials are configured, reporting the outcome to the operator. A
// missing-credentials case on an empty database is logged as a warning rather
// than treated as an error.
func runBootstrap(ctx context.Context, cmd *cobra.Command, svc *auth.Service, authCfg config.AuthConfig) error {
	outcome, err := svc.Bootstrap(ctx, authCfg.BootstrapAdminUsername, authCfg.BootstrapAdminPassword)
	if err != nil {
		return fmt.Errorf("bootstrapping admin user: %w", err)
	}
	switch outcome {
	case auth.BootstrapCreated:
		cmd.Printf("created bootstrap admin user %q\n", authCfg.BootstrapAdminUsername)
	case auth.BootstrapSkippedNoCredentials:
		log.Print("warning: no users exist and no bootstrap admin is configured; " +
			"set auth.bootstrap_admin_username and auth.bootstrap_admin_password")
	case auth.BootstrapSkippedHasUsers:
		// Users already exist; nothing to do.
	}
	return nil
}
