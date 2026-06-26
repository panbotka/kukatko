package main

import (
	"context"
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
)

// buildAuth assembles the auth subsystem from configuration and the database:
// the store, the session service, the login rate limiter, and the HTTP API. The
// returned API mounts the auth routes; the returned Service is used for
// bootstrap and the background session-cleanup loop.
func buildAuth(cfg *config.Config, db *database.DB) (*auth.API, *auth.Service) {
	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{
		TTL:         cfg.Auth.SessionTTL,
		MaxLifetime: cfg.Auth.SessionMaxLifetime,
	})
	limiter := auth.NewLimiter(cfg.Auth.LoginRateLimit, cfg.Auth.LoginRateWindow)
	api := auth.NewAPI(auth.APIConfig{
		Service:       svc,
		Limiter:       limiter,
		SecureCookies: cfg.Web.SecureCookies,
	})
	return api, svc
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
