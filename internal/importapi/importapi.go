// Package importapi exposes the admin-only HTTP trigger for the PhotoPrism
// import. It does not run the import inline — a full import is long-running and
// belongs on the background worker — but enqueues a single pp_import job and
// returns its id. The job's payload carries a fixed sentinel so the queue's dedup
// key allows only one import to be queued or running at a time: triggering again
// while an import is in flight is reported as a conflict, not a second run.
//
// The package depends only on a Queue behaviour and an admin guard, both
// injected, so it stays decoupled from the job store and the importer's wiring.
package importapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/ppimport"
)

// Queue enqueues background jobs. It is the import-facing subset of jobs.Store,
// satisfied by *jobs.Store.
type Queue interface {
	// Enqueue inserts a job of the given type and payload, returning
	// jobs.ErrDuplicate when an active job already exists for its dedup key.
	Enqueue(ctx context.Context, jobType string, payload json.RawMessage, opts jobs.EnqueueOptions) (jobs.Job, error)
}

// API exposes the import trigger over HTTP. The admin guard is supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not
// its wiring.
type API struct {
	queue        Queue
	requireAdmin func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Both fields are required.
type Config struct {
	// Queue is the job queue the trigger enqueues the pp_import job onto.
	Queue Queue
	// RequireAdmin guards the endpoint for administrators only.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{queue: cfg.queueOrPanic(), requireAdmin: cfg.RequireAdmin}
}

// queueOrPanic returns the configured queue, panicking on a nil one since a
// missing queue is a wiring bug that should surface at startup.
func (c Config) queueOrPanic() Queue {
	if c.Queue == nil {
		panic("importapi: NewAPI requires a Queue")
	}
	return c.Queue
}

// RegisterRoutes mounts the import endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST /import/photoprism  RequireAdmin  enqueue a PhotoPrism import job
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAdmin).Post("/import/photoprism", a.handleImportPhotoPrism)
}

// importResponse is the JSON body returned when an import job is enqueued.
type importResponse struct {
	// JobID is the queued pp_import job's id.
	JobID int64 `json:"job_id"`
	// Status is the queued job's state ("queued").
	Status string `json:"status"`
}

// handleImportPhotoPrism enqueues a single pp_import job. An import already in
// flight (the dedup sentinel collides) is reported as 409 Conflict; the queued
// job is reported as 202 Accepted with its id.
func (a *API) handleImportPhotoPrism(w http.ResponseWriter, r *http.Request) {
	job, err := a.queue.Enqueue(r.Context(), jobs.TypePPImport, ppimport.JobPayload(), jobs.EnqueueOptions{
		MaxAttempts: 3,
	})
	if errors.Is(err, jobs.ErrDuplicate) {
		writeError(w, http.StatusConflict, "a photoprism import is already in progress")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "enqueuing import failed")
		return
	}
	writeJSON(w, http.StatusAccepted, importResponse{JobID: job.ID, Status: string(job.State)})
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
		log.Printf("importapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
