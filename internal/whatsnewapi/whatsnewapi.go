// Package whatsnewapi exposes GET /whats-new, the digest a returning reader sees
// above the library grid: how many photos arrived, which albums were created,
// which people were named and how many comments were written since their
// previous visit.
//
// The endpoint is readable by every authenticated role, viewers included —
// learning what the family added is not a curation power — and it is the only
// surface of internal/whatsnew, which owns both the counting and the visit
// bookkeeping.
//
// The request is a GET that writes: reading the digest is what stamps the
// reader's heartbeat and, after a long enough absence, rotates the reference
// point of the visit. That is deliberate. The alternative — a separate POST the
// client must remember to send — would leave the reference point wrong exactly
// when the client crashed or the tab was closed, and the write is a single row
// on the caller's own account, idempotent within a visit.
//
// An account whose first visit this is, or whose visit found nothing, gets a 200
// with has_news false rather than a 404 or a 204, so the client has one shape to
// parse and one flag to branch on.
package whatsnewapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/whatsnew"
)

// Summarizer produces the digest for one account and stamps that account's visit
// in the same call. whatsnew.Store satisfies it.
type Summarizer interface {
	// Summary returns the digest for userUID as of now, advancing the visit
	// bookkeeping. It returns whatsnew.ErrUserNotFound if the account is gone.
	Summary(ctx context.Context, userUID string, now time.Time) (whatsnew.Summary, error)
}

// API exposes the what's-new endpoint over HTTP. The auth guard is supplied by
// the caller (the auth subsystem) so this package depends on auth only for the
// caller's identity, not for its wiring.
type API struct {
	store       Summarizer
	now         func() time.Time
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Store and RequireAuth are required;
// Now defaults to time.Now.
type Config struct {
	// Store produces the digest and advances the visit bookkeeping.
	Store Summarizer
	// RequireAuth guards the endpoint: any logged-in user may read their digest.
	RequireAuth func(http.Handler) http.Handler
	// Now supplies the current time, injectable so tests can move a visit
	// forward without waiting.
	Now func() time.Time
}

// NewAPI returns an API from cfg, defaulting Now to time.Now.
func NewAPI(cfg Config) *API {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &API{store: cfg.Store, now: now, requireAuth: cfg.RequireAuth}
}

// RegisterRoutes mounts the endpoint onto r, which the caller has scoped under
// the API base path (for example /api/v1). The route requires an authenticated
// user of any role:
//
//	GET /whats-new  digest of the library since the caller's previous visit
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/whats-new", a.handleGet)
}

// handleGet returns the caller's digest and stamps their visit. An account that
// vanished mid-request is reported as an empty digest rather than an error: the
// request is already unauthenticated in every way that matters, and the panel
// has nothing to say either way.
func (a *API) handleGet(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	summary, err := a.store.Summary(r.Context(), user.UID, a.now())
	if errors.Is(err, whatsnew.ErrUserNotFound) {
		writeJSON(w, http.StatusOK, whatsnew.Summary{})
		return
	}
	if err != nil {
		log.Printf("whatsnewapi: building summary for %s: %v", user.UID, err)
		writeError(w, http.StatusInternalServerError, "could not build the summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("whatsnewapi: encoding JSON response: %v", err)
	}
}

// writeError writes the standard error envelope with the given status code.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
