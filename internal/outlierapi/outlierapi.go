// Package outlierapi exposes per-subject face outlier detection over HTTP for
// editors and admins. GET /subjects/{uid}/outliers returns the subject's assigned
// faces ranked by cosine distance from their embedding centroid (most likely
// misassigned first), so a curator can spot and unassign a wrong face through the
// existing face-assignment API — this package adds no mutation. It depends on an
// outlier-service behaviour and a write guard, both injected, so it stays
// decoupled from the outliers package's wiring.
package outlierapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
)

// Service is the outlier backend the endpoint delegates to. It is an interface so
// outlierapi depends on the behaviour, not the outliers package's wiring;
// outliers.Service satisfies it.
type Service interface {
	// Outliers returns the subject's assigned faces ranked most-suspicious first,
	// or people.ErrSubjectNotFound when no such subject exists.
	Outliers(ctx context.Context, subjectUID string) (outliers.Result, error)
}

// API exposes the outlier endpoint over HTTP. The write guard is supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	service      Service
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service makes the endpoint
// answer 503.
type Config struct {
	// Service backs the outlier endpoint.
	Service Service
	// RequireWrite guards the endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the outlier endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET /subjects/{uid}/outliers  RequireWrite  ranked outlier faces for a subject
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireWrite).Get("/subjects/{uid}/outliers", a.handleList)
}

// handleList returns the subject's assigned faces ranked by distance from their
// centroid. An unknown subject answers 404 and a missing backend 503.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "face outlier detection not available")
		return
	}
	result, err := a.service.Outliers(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		if errors.Is(err, people.ErrSubjectNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "listing outliers failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
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
		log.Printf("outlierapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
