// Package announcementapi exposes the single instance-wide announcement over
// HTTP: any signed-in user can read the current banner message, and a maintainer
// can publish or clear it. It mirrors internal/organizeapi's dual-guard shape —
// reads behind RequireAuth, mutations behind RequireMaintainer — with both guards
// and the store injected so the package stays decoupled from auth's wiring and the
// concrete store, and is unit-testable with fakes.
//
// GET returns 200 with an empty-message body when nothing is published, which is
// friendlier for the polling banner client than a 404. Publishing and clearing are
// audited in the same transaction as the change by the store.
package announcementapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/announcement"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// maxBodyBytes caps the publish request body. A banner message is a short line,
// so a small limit is ample and keeps a malformed or hostile request cheap.
const maxBodyBytes = 16 << 10

// Store is the subset of announcement.Store the endpoints need. It is an interface
// so the handlers depend on behaviour rather than the concrete store, keeping them
// unit-testable with fakes. Set and Clear take an audit.Entry the store writes in
// the same transaction as the change.
type Store interface {
	// Get returns the current announcement, or announcement.ErrNotFound when none
	// is published.
	Get(ctx context.Context) (announcement.Announcement, error)
	// Set publishes message at level authored by authorUID (replacing any existing
	// one), auditing the change, and returns the persisted announcement.
	Set(
		ctx context.Context, message, level, authorUID string, entry audit.Entry,
	) (announcement.Announcement, error)
	// Clear takes the announcement down for all users, auditing the change.
	Clear(ctx context.Context, entry audit.Entry) error
}

// API exposes the announcement endpoints over HTTP. The route guards are supplied
// by the caller (the auth subsystem) so this package depends on auth's behaviour,
// not its wiring.
type API struct {
	store             Store
	requireAuth       func(http.Handler) http.Handler
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the announcement read, publish and clear operations.
	Store Store
	// RequireAuth guards the read endpoint for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireMaintainer guards the publish and clear endpoints for maintainers.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store:             cfg.Store,
		requireAuth:       cfg.RequireAuth,
		requireMaintainer: cfg.RequireMaintainer,
	}
}

// RegisterRoutes mounts the announcement endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET    /announcement   RequireAuth         read the current banner (200 {"message":""} when none)
//	PUT    /announcement   RequireMaintainer   publish/replace the banner
//	DELETE /announcement   RequireMaintainer   clear the banner
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/announcement", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleGet)
		r.With(a.requireMaintainer).Put("/", a.handleSet)
		r.With(a.requireMaintainer).Delete("/", a.handleClear)
	})
}

// handleGet writes the current announcement, or an empty-message body (still 200)
// when none is published.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	current, err := a.store.Get(r.Context())
	if errors.Is(err, announcement.ErrNotFound) {
		writeJSON(w, http.StatusOK, response{})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading announcement failed")
		return
	}
	writeJSON(w, http.StatusOK, toResponse(current))
}

// setRequest is the publish request body.
type setRequest struct {
	Message string `json:"message"`
	Level   string `json:"level"`
}

// handleSet publishes (or replaces) the announcement and writes the persisted
// record. A blank message or an unrecognised level is a 400.
func (a *API) handleSet(w http.ResponseWriter, r *http.Request) {
	in, err := decodeSet(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	entry := a.auditEntry(r, user.UID, audit.ActionAnnouncementSet, map[string]any{
		"message": in.Message,
		"level":   in.Level,
	})
	saved, err := a.store.Set(r.Context(), in.Message, in.Level, user.UID, entry)
	if err != nil {
		writeSetError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toResponse(saved))
}

// handleClear takes the announcement down for all users.
func (a *API) handleClear(w http.ResponseWriter, r *http.Request) {
	user, _ := auth.UserFromContext(r.Context())
	entry := a.auditEntry(r, user.UID, audit.ActionAnnouncementClear, nil)
	if err := a.store.Clear(r.Context(), entry); err != nil {
		writeError(w, http.StatusInternalServerError, "clearing announcement failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// auditEntry builds an audit entry for a mutation, stamping the acting user plus
// the request's client IP and User-Agent onto the announcement action and details.
// The store writes the returned entry inside the mutation's transaction. The
// mutating routes are guarded by RequireMaintainer, so a principal is present in
// production; an absent principal yields an empty actor UID (stored as NULL).
func (a *API) auditEntry(r *http.Request, actorUID, action string, details map[string]any) audit.Entry {
	return audit.FromRequest(r, actorUID).Entry(action, "announcement", "", details)
}

// decodeSet reads and validates the publish request body, rejecting a body that
// is missing, malformed, oversized or carries unknown fields.
func decodeSet(r *http.Request) (setRequest, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	var in setRequest
	if err := dec.Decode(&in); err != nil {
		return setRequest{}, errors.New("invalid request body")
	}
	return in, nil
}

// writeSetError maps a store publish error to an HTTP response: an empty message
// or bad level is a 400 (client error), anything else a 500.
func writeSetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, announcement.ErrEmptyMessage), errors.Is(err, announcement.ErrInvalidLevel):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "publishing announcement failed")
	}
}

// response is the announcement wire shape. When nothing is published every field
// but message is empty and omitted, so the body is {"message":""}; when an
// announcement exists the remaining fields are populated.
type response struct {
	Message   string `json:"message"`
	Level     string `json:"level,omitempty"`
	AuthorUID string `json:"author_uid,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// toResponse renders an announcement as its wire shape, formatting the timestamp
// as RFC 3339 so the client can persist it verbatim as a per-user dismissal key.
func toResponse(a announcement.Announcement) response {
	return response{
		Message:   a.Message,
		Level:     a.Level,
		AuthorUID: a.AuthorUID,
		UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339Nano),
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
		log.Printf("announcementapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
