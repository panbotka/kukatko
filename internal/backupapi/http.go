// Package backupapi exposes the admin-only HTTP API over the S3 backup
// subsystem: a status/last-run readout and an on-demand trigger that starts a
// backup in the background. It depends on the backup service for the work and on
// the auth subsystem only for the admin route guard, injected as middleware.
//
// When no backup destination is configured the service is nil: the status
// endpoint reports configured=false and the trigger returns 503, so the API can
// always be mounted regardless of configuration.
package backupapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/backup"
)

// Service is the subset of backup.Service the API needs: reading status and
// triggering a background run. It is an interface so the API can be tested with
// a fake. A nil Service means no backup destination is configured.
type Service interface {
	// Status returns the current backup subsystem state.
	Status() backup.Status
	// Trigger starts a backup in the background as of ts, returning
	// backup.ErrAlreadyRunning if one is already in progress.
	Trigger(ctx context.Context, ts time.Time) error
}

// API exposes the backup subsystem over HTTP. The admin route guard is supplied
// by the caller (the auth subsystem).
type API struct {
	svc          Service
	requireAdmin func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Service may be nil (no backup
// destination configured); RequireAdmin is required.
type Config struct {
	// Service runs and reports backups; nil when no destination is configured.
	Service Service
	// RequireAdmin guards every endpoint: backups are admin-only.
	RequireAdmin func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{svc: cfg.Service, requireAdmin: cfg.RequireAdmin}
}

// RegisterRoutes mounts the backup endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1). Both routes are admin-only:
//
//	GET  /backup  status and last-run readout
//	POST /backup  trigger a backup in the background
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/backup", func(r chi.Router) {
		r.With(a.requireAdmin).Get("/", a.handleStatus)
		r.With(a.requireAdmin).Post("/", a.handleTrigger)
	})
}

// handleStatus returns the backup subsystem status. When no destination is
// configured it reports configured=false rather than an error.
func (a *API) handleStatus(w http.ResponseWriter, _ *http.Request) {
	if a.svc == nil {
		writeJSON(w, http.StatusOK, backup.Status{Configured: false})
		return
	}
	writeJSON(w, http.StatusOK, a.svc.Status())
}

// triggerResponse is the JSON body returned when a backup is started.
type triggerResponse struct {
	Status string `json:"status"`
}

// handleTrigger starts a backup in the background and returns 202. A request
// while a run is in progress is answered with 409, and an unconfigured
// destination with 503.
func (a *API) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if a.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "backup destination not configured")
		return
	}
	if err := a.svc.Trigger(r.Context(), time.Now()); err != nil {
		if errors.Is(err, backup.ErrAlreadyRunning) {
			writeError(w, http.StatusConflict, "a backup is already in progress")
			return
		}
		writeError(w, http.StatusInternalServerError, "starting backup failed")
		return
	}
	writeJSON(w, http.StatusAccepted, triggerResponse{Status: "started"})
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
		log.Printf("backupapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
