// Package settingsapi exposes a project's settings over HTTP with several
// audiences, and one rule that shapes all of them: the registration secret is
// stored readable so the project's administrator can read it back, so it must
// never reach anyone else.
//
// That is why the responses are separate wire types built explicitly, rather
// than one record filtered per role — a field added to settings.Settings then
// has to be given an audience on purpose instead of leaking into whichever
// payload happens to embed it. Every route but one lives under the project
// prefix /p/{project} and acts on the project auth.ProjectFromContext names:
//
//	GET /settings/public    RequireActiveProject  the project's name and whether it takes registrations
//	GET /settings/welcome   RequireAuth           only the project's first-sign-in welcome Markdown
//	GET /settings/footer    RequireAuth           only the project's footer line
//	GET /settings           RequireAdmin          the full record, secret included
//	PUT /settings           RequireAdmin          replaces the values (the footer line when sent)
//	GET /settings/usage     RequireAdmin          what the project occupies and its two limits
//
// and one is instance-level (RegisterInstanceRoutes):
//
//	GET /settings/public    anonymous             whether this instance runs passkeys and sends mail
//
// Both public endpoints are deliberately unauthenticated: the sign-in screen
// has to know whether it can run a passkey ceremony and whether the instance
// sends any mail at all before anybody is signed in, and a project's
// registration page has to know whether that project takes registrations. Each
// answers what the screen asking it would show anyway.
//
// The project one is the instance's only anonymous answer keyed by a slug, so
// it is built not to be a slug oracle. It answers 404 "project not found" —
// byte for byte what RequireActiveProject says for an unknown or deactivated
// slug — also for an active project whose registration is closed, and it is
// throttled per client IP like the invite page. The trade-off: a project
// reveals its existence and its name to anybody who guesses its slug only
// while its registration is open, which is exactly when its registration page
// exists to greet strangers. A closed project's own registration page can no
// longer say "closed" in particular; it says "does not exist or takes no
// registrations", the same sentence a mistyped link gets.
//
// The welcome text is the project's, while whether a person has seen a welcome
// (users.welcome_seen_at) stays per account: somebody who joins a second
// project is not greeted again.
//
// The footer line is the project's too — the sentence its pages end with,
// typically who runs the library. It is read by any member (the shell draws it
// on every page of the project) and never anonymously: an instance page, an
// anonymous upload-link page and a project one may not enter show no line.
//
// The guards and the stores are injected so the package stays decoupled from
// auth's wiring and the concrete stores, and is unit-testable with fakes. An
// update is audited in the same transaction as the change by the store.
package settingsapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/ratelimit"
	"github.com/panbotka/kukatko/internal/settings"
)

// maxBodyBytes caps the update request body. The welcome text is Markdown a
// person reads on their first sign-in, so it is prose rather than a document;
// 64 KiB is ample and keeps a malformed or hostile request cheap.
const maxBodyBytes = 64 << 10

// Store is the subset of settings.Store the endpoints need. It is an interface
// so the handlers depend on behaviour rather than the concrete store, keeping
// them unit-testable with fakes.
type Store interface {
	// Get returns the project's current settings, including the registration
	// secret in readable form.
	Get(ctx context.Context, projectUID string) (settings.Settings, error)
	// Set replaces the project's settings, auditing the change in the same
	// transaction, and returns the persisted record.
	Set(
		ctx context.Context, projectUID string, in settings.Update, actorUID string, entry audit.Entry,
	) (settings.Settings, error)
}

// UsageSource reads how much of the instance a project occupies. It is
// satisfied by *project.Store.
type UsageSource interface {
	// Usage returns the project's photo count and the summed size of its
	// originals.
	Usage(ctx context.Context, uid string) (project.Usage, error)
}

// API exposes the settings endpoints over HTTP. The route guards are supplied
// by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	store                Store
	usage                UsageSource
	passkeys             bool
	mail                 bool
	ipLimit              *ratelimit.Limiter
	requireActiveProject func(http.Handler) http.Handler
	requireAuth          func(http.Handler) http.Handler
	requireAdmin         func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the settings read and update operations.
	Store Store
	// Usage backs the admin's read-only usage block (GET /settings/usage).
	Usage UsageSource
	// Passkeys reports whether this instance has a WebAuthn relying party
	// configured. It is a static fact about the deployment (auth.API.PasskeysEnabled),
	// not a stored setting, and it travels on the public response because the
	// sign-in screen has to decide whether to offer passkey sign-in while nobody
	// is signed in yet — GET /capabilities, which carries the same flag for the
	// rest of the app, is behind RequireAuth and cannot answer that question.
	Passkeys bool
	// Mail reports whether this instance really sends transactional mail — that
	// is, whether config's mail.enabled put an SMTP sender behind mailer.Sender
	// rather than the no-op one. It travels on the public response because the
	// screens that promise an e-mail (a registration was received, an account is
	// waiting for approval) are the screens nobody is signed in on, and a promise
	// an instance with mail off can never keep is worse than no promise at all.
	Mail bool
	// IPLimit throttles the project's anonymous public settings per client IP,
	// so nobody walks a dictionary of slugs against it; nil throttles nothing.
	IPLimit *ratelimit.Limiter
	// RequireActiveProject admits anybody, signed in or not, into an existing,
	// active project (auth.RequireActiveProject) — the guard of the project's
	// public settings, which its registration page reads.
	RequireActiveProject func(http.Handler) http.Handler
	// RequireAuth guards the welcome-text endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireAdmin guards the full record and the update.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg. A nil limiter becomes a disabled one, so the
// route never branches on whether throttling is configured.
func NewAPI(cfg Config) *API {
	a := &API{
		store:                cfg.Store,
		usage:                cfg.Usage,
		passkeys:             cfg.Passkeys,
		mail:                 cfg.Mail,
		ipLimit:              cfg.IPLimit,
		requireActiveProject: cfg.RequireActiveProject,
		requireAuth:          cfg.RequireAuth,
		requireAdmin:         cfg.RequireAdmin,
	}
	if a.ipLimit == nil {
		a.ipLimit = ratelimit.New(0, 0)
	}
	return a
}

// RegisterRoutes mounts the project's settings endpoints onto r, which the
// caller has scoped to one project (/api/v1/p/{project}):
//
//	GET /settings/public    IP limit + RequireActiveProject  {"project_name":"…","registration_enabled":true}
//	GET /settings/welcome   RequireAuth                      {"welcome_markdown":"…"}
//	GET /settings/footer    RequireAuth                      {"footer_text":"…"}
//	GET /settings           RequireAdmin                     the full record, secret included
//	PUT /settings           RequireAdmin                     replaces the values (footer_text optional)
//	GET /settings/usage     RequireAdmin                     photos, originals bytes and the two limits
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.With(a.ipLimit.Middleware, a.requireActiveProject).Get("/public", a.handleProjectPublic)
		r.With(a.requireAuth).Get("/welcome", a.handleWelcome)
		r.With(a.requireAuth).Get("/footer", a.handleFooter)
		r.With(a.requireAdmin).Get("/usage", a.handleUsage)
		r.With(a.requireAdmin).Get("/", a.handleGet)
		r.With(a.requireAdmin).Put("/", a.handleSet)
	})
}

// RegisterInstanceRoutes mounts the one instance-level settings endpoint onto
// r, which the caller has scoped under the API base path (for example
// /api/v1):
//
//	GET /settings/public    (no guard)     {"passkeys_enabled":…,"mail_enabled":…}
func (a *API) RegisterInstanceRoutes(r chi.Router) {
	r.Get("/settings/public", a.handleInstancePublic)
}

// handleInstancePublic writes the two facts about the instance an anonymous
// caller is allowed to learn: whether the sign-in screen should offer to sign
// in with a passkey, and whether this instance sends mail at all — the latter
// decides whether the registration and pending-approval screens may promise an
// e-mail. Neither is stored; both are facts about the process.
func (a *API) handleInstancePublic(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, instancePublicResponse{PasskeysEnabled: a.passkeys, MailEnabled: a.mail})
}

// handleProjectPublic writes what the project's registration page must know
// before anybody has an account: the project's name, to greet with. It answers
// only while the project takes self-service registrations; a project with
// registration closed gets the 404 RequireActiveProject already gave an
// unknown or deactivated slug, word for word, so the answer never tells a
// stranger that a slug exists (see the package comment).
func (a *API) handleProjectPublic(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PublicProjectFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusNotFound, projectNotFoundMessage)
		return
	}
	current, err := a.store.Get(r.Context(), p.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	if !current.RegistrationEnabled {
		writeError(w, http.StatusNotFound, projectNotFoundMessage)
		return
	}
	writeJSON(w, http.StatusOK, projectPublicResponse{
		ProjectName:         p.Name,
		RegistrationEnabled: current.RegistrationEnabled,
	})
}

// handleUsage writes how much of the instance the project occupies next to
// the two limits the system administrator set on it (null for "no limit"). It
// is read-only here: only the system administrator changes a limit, through
// /projects.
func (a *API) handleUsage(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.ProjectFromContext(r.Context())
	if !ok || a.usage == nil {
		writeError(w, http.StatusInternalServerError, "reading usage failed")
		return
	}
	usage, err := a.usage.Usage(r.Context(), p.UID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading usage failed")
		return
	}
	writeJSON(w, http.StatusOK, usageResponse{
		Photos:         usage.Photos,
		OriginalsBytes: usage.OriginalsBytes,
		MaxFileBytes:   p.MaxFileBytes,
		MaxTotalBytes:  p.MaxTotalBytes,
	})
}

// handleWelcome writes the first-sign-in greeting for any signed-in user. An
// unset greeting is an empty string, not a 404 — the client decides whether an
// empty greeting is worth showing.
func (a *API) handleWelcome(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context(), requestProject(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	writeJSON(w, http.StatusOK, welcomeResponse{WelcomeMarkdown: current.WelcomeMarkdown})
}

// handleFooter writes the project's footer line for any signed-in member. No
// line is an empty string, not a 404: the shell then draws only the link to the
// source code.
func (a *API) handleFooter(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context(), requestProject(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	writeJSON(w, http.StatusOK, footerResponse{FooterText: current.FooterText})
}

// handleGet writes the full settings record, registration secret included. The
// route is behind RequireAdmin: reading the secret back is the whole point of
// storing it unhashed, and the reason nobody below the project's administrator
// may.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context(), requestProject(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(current))
}

// handleSet replaces the settings and writes the persisted record. Enabling
// registration with a blank secret is a 400, and so is a footer line that is
// too long or not a single line.
func (a *API) handleSet(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSet(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	entry := a.auditEntry(r, user.UID, in)
	saved, err := a.store.Set(r.Context(), requestProject(r), in, user.UID, entry)
	if err != nil {
		writeSetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(saved))
}

// auditEntry builds the audit entry for an update, stamping the acting user plus
// the request's client IP and User-Agent onto the settings action. The details
// record the new registration flag, whether a secret is now set and whether
// there is a welcome text — enough to read the trail, without copying the secret
// or the greeting into a permanent record — and, when the update writes the
// footer line, the line itself: it is public to every member anyway. The store writes the returned entry
// inside the update's transaction. The route is guarded by RequireAdmin, so a
// principal is present in production; an absent principal yields an empty actor
// UID (stored as NULL).
func (a *API) auditEntry(r *http.Request, actorUID string, in settings.Update) audit.Entry {
	details := map[string]any{
		"registration_enabled": in.RegistrationEnabled,
		"secret_set":           in.RegistrationSecret != "",
		"welcome_set":          in.WelcomeMarkdown != "",
	}
	if in.FooterText != nil {
		details["footer_text"] = strings.TrimSpace(*in.FooterText)
	}
	return audit.FromRequest(r, actorUID).Entry(audit.ActionSettingsUpdate, "settings", "", details)
}

// decodeSet reads the update request body, rejecting a body that is missing,
// malformed, oversized or carries unknown fields. The three registration values
// are required in the sense that the update replaces them wholesale: a field
// left out of the body is written as its zero value. footer_text is the one
// optional field — left out, the stored line stays (a client from before the
// footer existed saves the rest without wiping it).
func decodeSet(r *http.Request) (settings.Update, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	var in settings.Update
	if err := dec.Decode(&in); err != nil {
		return settings.Update{}, errors.New("invalid request body")
	}
	return in, nil
}

// writeSetError maps a store update error to an HTTP response: enabling
// registration without a secret or an unfit footer line is a 400 (the
// administrator has to fix the request), anything else a 500.
func writeSetError(w http.ResponseWriter, err error) {
	if errors.Is(err, settings.ErrSecretRequired) || errors.Is(err, settings.ErrFooterTooLong) ||
		errors.Is(err, settings.ErrFooterMultiline) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "saving settings failed")
}

// instancePublicResponse is the instance-level anonymous wire shape. It has
// exactly two fields on purpose: it is served without authentication, so
// anything added here is added to the open internet. Both are things the
// sign-in screen puts on that same open page anyway — a "sign in with a
// passkey" button, and the sentence telling a new account how it will hear
// back — and the passkey flag is one an anonymous caller could already read off
// POST /auth/passkeys/login/begin answering 200 rather than 501. The mail flag
// says only whether this instance sends transactional mail at all; it names no
// server, no account and no address.
type instancePublicResponse struct {
	PasskeysEnabled bool `json:"passkeys_enabled"`
	MailEnabled     bool `json:"mail_enabled"`
}

// projectNotFoundMessage is the 404 message of the project's public settings
// for a project that takes no registrations. It must stay identical to the one
// auth.RequireActiveProject answers an unknown slug with; the integration test
// compares the two bodies byte for byte.
const projectNotFoundMessage = "project not found"

// projectPublicResponse is the project's anonymous wire shape, served to
// anybody who knows the slug of a project whose registration is open: the name
// the registration page greets with, and registration_enabled — always true
// now that a closed project is a 404, kept so the wire shape did not change
// under the client. Never the secret, never the welcome text.
type projectPublicResponse struct {
	ProjectName         string `json:"project_name"`
	RegistrationEnabled bool   `json:"registration_enabled"`
}

// usageResponse is the admin's read-only usage block: the photo count, the
// summed size of the originals, and the two limits (null when there is none).
type usageResponse struct {
	Photos         int64  `json:"photos"`
	OriginalsBytes int64  `json:"originals_bytes"`
	MaxFileBytes   *int64 `json:"max_file_bytes"`
	MaxTotalBytes  *int64 `json:"max_total_bytes"`
}

// welcomeResponse is the authenticated wire shape for the first-sign-in
// greeting. Also one field: a signed-in viewer has no business reading the
// registration secret either.
type welcomeResponse struct {
	WelcomeMarkdown string `json:"welcome_markdown"`
}

// footerResponse is the members' wire shape for the footer line: one field,
// like welcomeResponse.
type footerResponse struct {
	FooterText string `json:"footer_text"`
}

// adminResponse is the full wire shape, registration secret included, served
// only behind RequireAdmin.
type adminResponse struct {
	RegistrationEnabled bool   `json:"registration_enabled"`
	RegistrationSecret  string `json:"registration_secret"`
	WelcomeMarkdown     string `json:"welcome_markdown"`
	FooterText          string `json:"footer_text"`
	UpdatedAt           string `json:"updated_at"`
	UpdatedByUID        string `json:"updated_by_uid,omitempty"`
}

// toResponse renders the settings as the admin wire shape, formatting the
// timestamp as RFC 3339 so the client can print it without reparsing.
func toResponse(s settings.Settings) adminResponse {
	return adminResponse{
		RegistrationEnabled: s.RegistrationEnabled,
		RegistrationSecret:  s.RegistrationSecret,
		WelcomeMarkdown:     s.WelcomeMarkdown,
		FooterText:          s.FooterText,
		UpdatedAt:           s.UpdatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedByUID:        s.UpdatedByUID,
	}
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("settingsapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// requestProject returns the uid of the project the request was admitted
// into by the guard in front of it.
func requestProject(r *http.Request) string {
	return auth.ProjectUIDFromContext(r.Context())
}
