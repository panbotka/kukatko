package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/uploadlink"
	"github.com/panbotka/kukatko/internal/uploadlinkapi"
)

// buildAuth assembles the auth subsystem from configuration and the database:
// the store, the session service, the login rate limiter, the self-service
// registration flow, the password-reset flow, the passkey flow, and the HTTP
// API — which forgets a browser's push subscription when it signs out, and
// names each enterable project's picture on the chooser. The
// returned API mounts the auth routes;
// the returned Service is used for bootstrap and the background session-cleanup
// loop. It fails only on a passkey relying party the WebAuthn library refuses.
//
// Registration is always wired, because whether it is open is a runtime decision
// an administrator makes in the instance settings, not a deployment one: with it
// switched off the endpoint refuses every caller, and switching it on needs no
// restart. Its mails go through the queue like every other message, so an
// instance with mail disabled registers people and sends nothing. The
// administrators' notice goes through the shared delivery path
// (buildDeliverer), to whichever channels each of them opted into.
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
		Projects: project.NewStore(db.Pool()),
		Notify:   buildDeliverer(cfg, db),
	})
	rename := auth.NewRename(auth.RenameConfig{
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
	passkeys, err := resolvePasskeys(cfg.Passkey(), svc, slog.Default())
	if err != nil {
		return nil, nil, err
	}
	covers, err := buildProjectCovers(cfg, db)
	if err != nil {
		return nil, nil, err
	}
	api := auth.NewAPI(auth.APIConfig{
		Service:       svc,
		Limiter:       limiter,
		Registration:  registration,
		UploadLinks:   uploadLinkGate(cfg, db),
		Invites:       inviteGate(db),
		Rename:        rename,
		PasswordReset: passwordReset,
		Passkeys:      passkeys,
		SecureCookies: cfg.Web.SecureCookies,
		PushDevices:   push.NewStore(db.Pool()),
		Covers:        covers,
	})
	return api, svc, nil
}

// uploadLinkGate builds what lets a live upload link stand in for the shared
// registration secret. It reschedules the sidecars of the photos a registration
// claims through its own queue adapter over the shared pool — the auth subsystem
// is built before the services and owns no enqueuer of its own.
func uploadLinkGate(cfg *config.Config, db *database.DB) *uploadlinkapi.RegistrationGate {
	sidecar := sidecarSchedulerFor(cfg, jobs.NewEnqueuer(jobs.NewStore(db.Pool())))
	return uploadlinkapi.NewRegistrationGate(uploadlink.NewStore(db.Pool()), sidecar, nil)
}

// resolvePasskeys decides whether this instance offers passkeys and builds the
// flow when it does. A nil flow (with a nil error) is "passkeys off": the
// endpoints answer "not available" and GET /capabilities says the same.
//
// It is off when the configuration resolved no relying party at all, and also
// when the relying party was merely derived from mail.base_url (rp.Derived) and
// the WebAuthn library refuses it — an IP address or a single-label host, as a
// LAN or staging deployment has. The operator configured nothing about
// passkeys there, so refusing to start would punish them for a feature they
// never asked for; one WARN says why passkeys are off and how to turn them on.
// A relying party the operator did configure still fails startup (see
// buildPasskeys).
func resolvePasskeys(rp config.RelyingParty, svc *auth.Service, logger *slog.Logger) (*auth.Passkeys, error) {
	if !rp.Enabled {
		return nil, nil //nolint:nilnil // a nil flow is the documented "passkeys off"
	}
	passkeys, err := buildPasskeys(rp, svc)
	if err == nil {
		return passkeys, nil
	}
	if !rp.Derived {
		return nil, err
	}
	logger.Warn("passkeys are off: the relying party derived from mail.base_url is not usable "+
		"(a passkey needs a domain name, not an IP address or a single-label host); "+
		"set auth.passkey.rp_id and auth.passkey.origins to offer them",
		"rp_id", rp.ID, "error", err)
	return nil, nil //nolint:nilnil // a nil flow is the documented "passkeys off"
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
	// The error goes back unwrapped: auth.NewPasskeys already names the step
	// ("auth: building the passkey relying party: …"), and saying it twice is
	// what the startup WARN used to print.
	return auth.NewPasskeys(auth.PasskeysConfig{ //nolint:wrapcheck // the callee names the step
		Service:       svc,
		RPID:          rp.ID,
		RPDisplayName: rp.DisplayName,
		Origins:       rp.Origins,
	})
}

// signInPath is the frontend route of the sign-in screen (see web/src/App.tsx).
const signInPath = "/login"

// signInURL builds the address the rename mail points at from this instance's
// public URL, tolerating a trailing slash. An empty base yields an empty URL —
// mail.base_url is required whenever mail is enabled, so the only instances that
// reach that case are the ones that send nothing anyway, and inventing a host
// would send somebody to the wrong one.
func signInURL(baseURL string) string {
	return instanceURL(baseURL, signInPath)
}

// projectURL builds the address of the project slug's page (/p/<slug>/), which
// the member approval mail links to, from this instance's public URL on the
// same terms as signInURL: an empty base yields an empty URL.
func projectURL(baseURL, slug string) string {
	return instanceURL(baseURL, "/p/"+url.PathEscape(slug)+"/")
}

// instanceURL appends the frontend route path to this instance's public URL,
// tolerating surrounding whitespace and a trailing slash on the base. An empty
// base (or one that is only a slash) yields an empty URL rather than a guessed
// host.
func instanceURL(baseURL, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + path
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
	return instanceURL(baseURL, passwordResetPath)
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
