// Package systemapi exposes the maintainer-only HTTP endpoint over the aggregated
// system status (internal/system): GET /system/status returns one snapshot of
// embeddings reachability, job-queue depth, the backup subsystem state, the last
// import per source, storage usage, database reachability and the build version.
// It depends on the system service for data and on the auth subsystem only for
// the maintainer route guard, injected as middleware. The dashboard polls this
// endpoint; quick actions reuse the existing jobs/backup/import/maintenance APIs.
package systemapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/system"
)

// StatusCollector is the subset of system.Service the API needs: gathering one
// status snapshot. It is an interface so the API can be tested with a fake.
type StatusCollector interface {
	// Collect gathers the full system-status snapshot.
	Collect(ctx context.Context) (system.Status, error)
}

// API exposes the system status over HTTP. The maintainer route guard is
// supplied by the caller (the auth subsystem) so this package depends on auth for
// the caller's identity, not its wiring.
type API struct {
	service           StatusCollector
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every field is required.
type Config struct {
	// Service aggregates the system status.
	Service StatusCollector
	// RequireMaintainer guards the endpoint: the status dashboard is a maintainer
	// operation.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, requireMaintainer: cfg.RequireMaintainer}
}

// RegisterRoutes mounts the system endpoint onto r, which the caller has scoped
// under the API base path (for example /api/v1). The route requires maintainer:
//
//	GET /system/status  aggregated operational status snapshot
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/system", func(r chi.Router) {
		r.With(a.requireMaintainer).Get("/status", a.handleStatus)
	})
}

// handleStatus returns the aggregated system-status snapshot, answering 500 when
// the underlying aggregation (which needs a working database) fails.
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := a.service.Collect(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collecting system status failed")
		return
	}
	writeJSON(w, http.StatusOK, status)
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
		log.Printf("systemapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
