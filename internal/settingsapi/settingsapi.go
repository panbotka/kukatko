// Package settingsapi exposes the instance-wide settings over HTTP with three
// different audiences, and one rule that shapes all of them: the registration
// secret is stored readable so an administrator can read it back, so it must
// never reach anyone else.
//
// That is why the three responses are separate wire types built explicitly,
// rather than one record filtered per role — a field added to settings.Settings
// then has to be given an audience on purpose instead of leaking into whichever
// payload happens to embed it:
//
//	GET /settings/public    anonymous       only what the sign-in screen must know
//	GET /settings/welcome   RequireAuth     only the first-sign-in welcome Markdown
//	GET /settings           RequireAdmin    the full record, secret included
//	PUT /settings           RequireAdmin    replaces all three values
//
// The public endpoint is deliberately unauthenticated: the sign-in screen has to
// know whether to offer registration — and whether this instance can run a
// passkey ceremony — before anybody is signed in. It answers two booleans and
// reads one seeded row, so it tells an anonymous caller nothing beyond what the
// sign-in screen would show them anyway.
//
// The guards and the store are injected so the package stays decoupled from
// auth's wiring and the concrete store, and is unit-testable with fakes. An
// update is audited in the same transaction as the change by the store.
package settingsapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
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
	// Get returns the current instance settings, including the registration
	// secret in readable form.
	Get(ctx context.Context) (settings.Settings, error)
	// Set replaces all three settings, auditing the change in the same
	// transaction, and returns the persisted record.
	Set(ctx context.Context, in settings.Update, actorUID string, entry audit.Entry) (settings.Settings, error)
}

// API exposes the instance-settings endpoints over HTTP. The route guards are
// supplied by the caller (the auth subsystem) so this package depends on auth's
// behaviour, not its wiring.
type API struct {
	store        Store
	passkeys     bool
	requireAuth  func(http.Handler) http.Handler
	requireAdmin func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the settings read and update operations.
	Store Store
	// Passkeys reports whether this instance has a WebAuthn relying party
	// configured. It is a static fact about the deployment (auth.API.PasskeysEnabled),
	// not a stored setting, and it travels on the public response because the
	// sign-in screen has to decide whether to offer passkey sign-in while nobody
	// is signed in yet — GET /capabilities, which carries the same flag for the
	// rest of the app, is behind RequireAuth and cannot answer that question.
	Passkeys bool
	// RequireAuth guards the welcome-text endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireAdmin guards the full record and the update.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store:        cfg.Store,
		passkeys:     cfg.Passkeys,
		requireAuth:  cfg.RequireAuth,
		requireAdmin: cfg.RequireAdmin,
	}
}

// RegisterRoutes mounts the settings endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET /settings/public    (no guard)     {"registration_enabled":…,"passkeys_enabled":…}
//	GET /settings/welcome   RequireAuth    {"welcome_markdown":"…"}
//	GET /settings           RequireAdmin   the full record, secret included
//	PUT /settings           RequireAdmin   replaces all three values
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Get("/public", a.handlePublic)
		r.With(a.requireAuth).Get("/welcome", a.handleWelcome)
		r.With(a.requireAdmin).Get("/", a.handleGet)
		r.With(a.requireAdmin).Put("/", a.handleSet)
	})
}

// handlePublic writes the two facts an anonymous caller is allowed to learn:
// whether the sign-in screen should offer self-service registration, and whether
// it should offer to sign in with a passkey.
func (a *API) handlePublic(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	writeJSON(w, http.StatusOK, publicResponse{
		RegistrationEnabled: current.RegistrationEnabled,
		PasskeysEnabled:     a.passkeys,
	})
}

// handleWelcome writes the first-sign-in greeting for any signed-in user. An
// unset greeting is an empty string, not a 404 — the client decides whether an
// empty greeting is worth showing.
func (a *API) handleWelcome(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	writeJSON(w, http.StatusOK, welcomeResponse{WelcomeMarkdown: current.WelcomeMarkdown})
}

// handleGet writes the full settings record, registration secret included. The
// route is behind RequireAdmin: reading the secret back is the whole point of
// storing it unhashed, and the reason nobody below an administrator may.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading settings failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(current))
}

// handleSet replaces all three settings and writes the persisted record.
// Enabling registration with a blank secret is a 400.
func (a *API) handleSet(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSet(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	entry := a.auditEntry(r, user.UID, in)
	saved, err := a.store.Set(r.Context(), in, user.UID, entry)
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
// or the greeting into a permanent record. The store writes the returned entry
// inside the update's transaction. The route is guarded by RequireAdmin, so a
// principal is present in production; an absent principal yields an empty actor
// UID (stored as NULL).
func (a *API) auditEntry(r *http.Request, actorUID string, in settings.Update) audit.Entry {
	return audit.FromRequest(r, actorUID).Entry(audit.ActionSettingsUpdate, "settings", "", map[string]any{
		"registration_enabled": in.RegistrationEnabled,
		"secret_set":           in.RegistrationSecret != "",
		"welcome_set":          in.WelcomeMarkdown != "",
	})
}

// decodeSet reads the update request body, rejecting a body that is missing,
// malformed, oversized or carries unknown fields. All three values are required
// in the sense that the update replaces the record wholesale: a field left out
// of the body is written as its zero value, which is what "PUT replaces all
// three" means.
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
// registration without a secret is a 400 (the administrator has to fix the
// request), anything else a 500.
func writeSetError(w http.ResponseWriter, err error) {
	if errors.Is(err, settings.ErrSecretRequired) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "saving settings failed")
}

// publicResponse is the anonymous wire shape. It has exactly two fields on
// purpose: it is served without authentication, so anything added here is added
// to the open internet. Both are things the sign-in screen puts on that same
// open page anyway — an invitation to register, and a "sign in with a passkey"
// button — and the passkey flag is one an anonymous caller could already read
// off POST /auth/passkeys/login/begin answering 200 rather than 501.
type publicResponse struct {
	RegistrationEnabled bool `json:"registration_enabled"`
	PasskeysEnabled     bool `json:"passkeys_enabled"`
}

// welcomeResponse is the authenticated wire shape for the first-sign-in
// greeting. Also one field: a signed-in viewer has no business reading the
// registration secret either.
type welcomeResponse struct {
	WelcomeMarkdown string `json:"welcome_markdown"`
}

// adminResponse is the full wire shape, registration secret included, served
// only behind RequireAdmin.
type adminResponse struct {
	RegistrationEnabled bool   `json:"registration_enabled"`
	RegistrationSecret  string `json:"registration_secret"`
	WelcomeMarkdown     string `json:"welcome_markdown"`
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
