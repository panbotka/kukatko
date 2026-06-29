// Package duplicatesapi exposes the editor/admin HTTP endpoint that lists groups
// of likely-duplicate photos for review. It depends only on a Service behaviour
// and a write guard, both injected, so it stays decoupled from the duplicates
// and auth wiring. A nil Service answers 503, so the route mounts unconditionally
// even when duplicate detection is disabled by config. Cleanup itself is not done
// here — the client archives the unwanted members through the existing bulk API.
package duplicatesapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/duplicates"
)

// Service is the duplicates behaviour the API drives. It is satisfied by
// *duplicates.Service; a nil Service makes the endpoint answer 503.
type Service interface {
	// FindGroups returns one page of duplicate groups.
	FindGroups(ctx context.Context, limit, offset int) (duplicates.Result, error)
}

// API exposes the duplicates endpoint over HTTP behind a write guard.
type API struct {
	service      Service
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service is valid (the endpoint
// answers 503); RequireWrite is required.
type Config struct {
	// Service finds duplicate groups; nil means detection is not configured.
	Service Service
	// RequireWrite guards the endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the duplicates endpoint onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET /duplicates  RequireWrite  list duplicate groups (query: limit, offset)
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireWrite).Get("/duplicates", a.handleList)
}

// handleList returns a page of duplicate groups. It answers 503 when detection is
// not configured, 400 for an invalid limit/offset, and 500 when the scan fails.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "duplicate detection not available")
		return
	}
	limit, offset, err := parsePaging(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.FindGroups(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "finding duplicates failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// parsePaging reads the optional limit and offset query parameters, returning a
// descriptive error when either is present but not a non-negative integer. Absent
// parameters yield zero, which the service treats as "default".
func parsePaging(r *http.Request) (limit, offset int, err error) {
	limit, err = parseNonNegative(r, "limit")
	if err != nil {
		return 0, 0, err
	}
	offset, err = parseNonNegative(r, "offset")
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

// parseNonNegative parses query parameter name as a non-negative integer,
// returning zero when it is absent and an error when it is malformed or negative.
func parseNonNegative(r *http.Request, name string) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, invalidParamError(name)
	}
	return n, nil
}

// invalidParamError builds the 400 error for a bad pagination parameter.
func invalidParamError(name string) error {
	return &paramError{name: name}
}

// paramError reports an invalid query parameter by name.
type paramError struct {
	name string
}

// Error implements error for paramError.
func (e *paramError) Error() string {
	return "invalid " + e.name + " parameter"
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
		log.Printf("duplicatesapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
