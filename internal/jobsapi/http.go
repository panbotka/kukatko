// Package jobsapi exposes the admin-only HTTP API over the persistent job queue:
// aggregate counts for the dashboard, a recent/dead-letter job listing, and a
// requeue action for failed or dead-lettered jobs. The frontend polls these
// endpoints (there is no SSE dependency). It depends on the jobs store for data
// and on the auth subsystem only for the admin route guard, injected as
// middleware.
package jobsapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/jobs"
)

// maxListLimit caps the page size accepted from the client for the recent-jobs
// listing; the store clamps too, but rejecting here yields a clear 400.
const maxListLimit = 500

// API exposes the job queue over HTTP. The admin route guard is supplied by the
// caller (the auth subsystem) so this package depends on auth for the caller's
// identity, not its wiring.
type API struct {
	store        *jobs.Store
	requireAdmin func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every field is required.
type Config struct {
	// Store is the persistent job queue backing reads and the requeue action.
	Store *jobs.Store
	// RequireAdmin guards every endpoint: job administration is admin-only.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireAdmin: cfg.RequireAdmin}
}

// RegisterRoutes mounts the job endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1). Every route is admin-only:
//
//	GET  /jobs/stats        aggregate counts by state and type
//	GET  /jobs              recent jobs (optionally filtered by state)
//	POST /jobs/{id}/requeue requeue a failed or dead-lettered job
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/jobs", func(r chi.Router) {
		r.With(a.requireAdmin).Get("/stats", a.handleStats)
		r.With(a.requireAdmin).Get("/", a.handleList)
		r.With(a.requireAdmin).Post("/{id}/requeue", a.handleRequeue)
	})
}

// statsResponse is the JSON body of the stats endpoint: per-state and per-type
// counts plus the grand total, for the admin queue dashboard.
type statsResponse struct {
	ByState map[jobs.State]int `json:"by_state"`
	ByType  map[string]int     `json:"by_type"`
	Total   int                `json:"total"`
}

// handleStats returns the aggregate queue counts grouped by state and by type.
func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	byState, err := a.store.CountsByState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting jobs by state failed")
		return
	}
	byType, err := a.store.CountsByType(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "counting jobs by type failed")
		return
	}
	total := 0
	for _, n := range byState {
		total += n
	}
	writeJSON(w, http.StatusOK, statsResponse{ByState: byState, ByType: byType, Total: total})
}

// listResponse is the JSON body of the list endpoint.
type listResponse struct {
	Jobs   []jobs.Job `json:"jobs"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset"`
}

// handleList returns a page of recent jobs, optionally filtered to a single
// state, ordered most-recently-updated first. Invalid query parameters are
// answered with 400.
func (a *API) handleList(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListOptions(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	list, err := a.store.List(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing jobs failed")
		return
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	writeJSON(w, http.StatusOK, listResponse{Jobs: list, Limit: limit, Offset: opts.Offset})
}

// handleRequeue resets the failed or dead-lettered job named in the path back to
// queued and returns the refreshed job. A missing job is answered with 404, a
// job in a non-requeueable state with 409, and a malformed id with 400.
func (a *API) handleRequeue(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	job, err := a.store.Requeue(r.Context(), id)
	if err != nil {
		writeRequeueError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// writeRequeueError maps a requeue store error to an HTTP response: 404 for a
// missing job, 409 for a job that is not in a requeueable state, otherwise 500.
func writeRequeueError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, jobs.ErrJobNotFound):
		writeError(w, http.StatusNotFound, "job not found")
	case errors.Is(err, jobs.ErrNotDead):
		writeError(w, http.StatusConflict, "job is not in a requeueable state")
	default:
		writeError(w, http.StatusInternalServerError, "requeuing job failed")
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
		log.Printf("jobsapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
