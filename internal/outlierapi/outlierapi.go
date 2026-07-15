// Package outlierapi exposes per-subject face outlier detection over HTTP for
// editors and admins. GET /subjects/{uid}/outliers returns the subject's assigned
// faces ranked by cosine distance from their embedding centroid (most likely
// misassigned first), so a curator can spot and unassign a wrong face through the
// existing face-assignment API — this package adds no mutation. The optional
// threshold and limit query parameters narrow the list for the review page; both
// default to "everything, ranked". It depends on an outlier-service behaviour and
// a write guard, both injected, so it stays decoupled from the outliers package's
// wiring.
package outlierapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
)

// maxThreshold is the largest accepted distance threshold: cosine distance never
// exceeds 2, so anything above it can only be a caller mistake.
const maxThreshold = 2

// Service is the outlier backend the endpoint delegates to. It is an interface so
// outlierapi depends on the behaviour, not the outliers package's wiring;
// outliers.Service satisfies it.
type Service interface {
	// Outliers returns the subject's assigned faces ranked most-suspicious first,
	// narrowed by opts, or people.ErrSubjectNotFound when no such subject exists.
	Outliers(ctx context.Context, subjectUID string, opts outliers.Options) (outliers.Result, error)
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
//
// Optional query parameters: threshold (minimum cosine distance from the
// centroid, 0..2, default 0 = everything) and limit (maximum faces returned,
// default 0 = all).
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireWrite).Get("/subjects/{uid}/outliers", a.handleList)
}

// handleList returns the subject's assigned faces ranked by distance from their
// centroid, narrowed by the threshold and limit query parameters. A malformed
// parameter answers 400, an unknown subject 404 and a missing backend 503.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "face outlier detection not available")
		return
	}
	opts, err := parseOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.Outliers(r.Context(), chi.URLParam(r, "uid"), opts)
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

// parseOptions reads the threshold and limit query parameters into
// outliers.Options. Both are optional; their zero values keep the historical
// "everything, ranked" behaviour.
func parseOptions(r *http.Request) (outliers.Options, error) {
	threshold, err := parseThreshold(r.URL.Query().Get("threshold"))
	if err != nil {
		return outliers.Options{}, err
	}
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		return outliers.Options{}, err
	}
	return outliers.Options{Threshold: threshold, Limit: limit}, nil
}

// parseThreshold turns the threshold query value into a minimum cosine distance.
// An empty value defaults to 0 (return everything); a negative, non-numeric or
// too-large (>2) value is rejected.
func parseThreshold(raw string) (float64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, errors.New("threshold must be a number")
	}
	switch {
	case value < 0:
		return 0, errors.New("threshold must not be negative")
	case value > maxThreshold:
		return 0, errors.New("threshold must be a cosine distance (<=2)")
	default:
		return value, nil
	}
}

// parseLimit reads the maximum number of faces to return. An empty value means
// all (0); a negative or non-numeric value is rejected.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errors.New("limit must be an integer")
	}
	if value < 0 {
		return 0, errors.New("limit must not be negative")
	}
	return value, nil
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
