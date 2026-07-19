// Package capabilitiesapi exposes GET /capabilities, an all-authenticated view of
// the instance's optional feature flags — currently only whether semantic search
// is available, which depends on the embeddings sidecar being reachable. The
// frontend polls it to show or hide the semantic-search affordance as the box
// goes on- or offline.
//
// Unlike the maintainer-only system status (internal/systemapi) it is cheap and
// readable by any logged-in user, because it surfaces only a cached boolean read
// from the background reachability checker (internal/reachability), never a live
// probe or any operational internals. The response shape is deliberately open so
// future flags (for example maps configuration) can be added without a new route.
package capabilitiesapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Reachability reports whether the embeddings sidecar (which powers semantic
// search) is currently reachable, from a cached background probe.
// reachability.Checker satisfies it with its Reachable method.
type Reachability interface {
	// Reachable reports the last cached probe result without blocking.
	Reachable() bool
}

// API exposes the capabilities endpoint over HTTP. The auth guard is supplied by
// the caller (the auth subsystem) so this package depends on auth for the
// caller's identity, not its wiring.
type API struct {
	embeddings  Reachability
	requireAuth func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every field is required.
type Config struct {
	// Embeddings reports the cached reachability of the semantic-search backend.
	Embeddings Reachability
	// RequireAuth guards the endpoint: any logged-in user may read capabilities.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{embeddings: cfg.Embeddings, requireAuth: cfg.RequireAuth}
}

// capabilities is the JSON body of GET /capabilities: the instance feature flags
// a logged-in client uses to decide which optional affordances to show. The
// shape is deliberately open for future flags.
type capabilities struct {
	// SemanticSearch is true when the embeddings sidecar is currently reachable,
	// so semantic search runs rather than silently degrading to full text.
	SemanticSearch bool `json:"semantic_search"`
}

// RegisterRoutes mounts the capabilities endpoint onto r, which the caller has
// scoped under the API base path (for example /api/v1). The route requires an
// authenticated user:
//
//	GET /capabilities  instance feature flags
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/capabilities", a.handleGet)
}

// handleGet returns the current instance feature flags, reading the cached
// reachability flag (never a live probe, so the request is always cheap).
func (a *API) handleGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, capabilities{SemanticSearch: a.embeddings.Reachable()})
}

// writeJSON writes payload as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("capabilitiesapi: encoding JSON response: %v", err)
	}
}
