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
// registration flow, and the HTTP API. The returned API mounts the auth routes;
// the returned Service is used for bootstrap and the background session-cleanup
// loop.
//
// Registration is always wired, because whether it is open is a runtime decision
// an administrator makes in the instance settings, not a deployment one: with it
// switched off the endpoint refuses every caller, and switching it on needs no
// restart. Its mails go through the queue like every other message, so an
// instance with mail disabled registers people and sends nothing.
func buildAuth(cfg *config.Config, db *database.DB) (*auth.API, *auth.Service) {
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
	api := auth.NewAPI(auth.APIConfig{
		Service:       svc,
		Limiter:       limiter,
		Registration:  registration,
		Approval:      approval,
		SecureCookies: cfg.Web.SecureCookies,
	})
	return api, svc
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
