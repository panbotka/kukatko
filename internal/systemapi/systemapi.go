// Package systemapi exposes the HTTP endpoints over the aggregated system state
// (internal/system). GET /system/status is maintainer-only and returns one
// snapshot of embeddings reachability, job-queue depth, the backup subsystem
// state, the last import per source, storage usage, database reachability and
// the build version. GET /system/stats is open to every signed-in user and
// returns the library statistics — the instance-wide photo/embedding/face/people
// counts — because knowing how big and how processed the library is, is not an
// operations secret; GET /system/stats/charts serves those same readers the
// series behind the counts (photos per year, arrivals per month, top cameras,
// storage). It depends on the system service for data and on the auth
// subsystem only for the two route guards, injected as middleware. The dashboard
// polls these endpoints; quick actions reuse the existing
// jobs/backup/import/maintenance APIs.
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
// status snapshot, the library counts and the chart series over them. It is an
// interface so the API can be tested with a fake.
type StatusCollector interface {
	// Collect gathers the full system-status snapshot.
	Collect(ctx context.Context) (system.Status, error)
	// LibraryStats gathers the instance-wide library counts.
	LibraryStats(ctx context.Context) (system.Library, error)
	// LibraryCharts gathers the chart series behind the statistics page.
	LibraryCharts(ctx context.Context) (system.Charts, error)
}

// API exposes the system status over HTTP. The route guards are supplied by the
// caller (the auth subsystem) so this package depends on auth for the caller's
// identity, not its wiring.
type API struct {
	service           StatusCollector
	requireMaintainer func(http.Handler) http.Handler
	requireAuth       func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Every field is required.
type Config struct {
	// Service aggregates the system status and the library counts.
	Service StatusCollector
	// RequireMaintainer guards the status endpoint: the operations dashboard is a
	// maintainer surface.
	RequireMaintainer func(http.Handler) http.Handler
	// RequireAuth guards the library-statistics endpoint: the counts are for every
	// signed-in user, but not for anonymous callers.
	RequireAuth func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		service:           cfg.Service,
		requireMaintainer: cfg.RequireMaintainer,
		requireAuth:       cfg.RequireAuth,
	}
}

// RegisterRoutes mounts the system endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET /system/status        aggregated operational status snapshot (maintainer)
//	GET /system/stats         instance-wide library counts (any signed-in user)
//	GET /system/stats/charts  the chart series over those counts (any signed-in user)
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/system", func(r chi.Router) {
		r.With(a.requireMaintainer).Get("/status", a.handleStatus)
		r.With(a.requireAuth).Get("/stats", a.handleStats)
		r.With(a.requireAuth).Get("/stats/charts", a.handleStatsCharts)
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

// handleStats returns the library-statistics snapshot. A failed aggregation is
// answered with 500 rather than a zeroed body, so a reader is never shown an
// empty library that only looks like a real count.
func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.service.LibraryStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collecting library statistics failed")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleStatsCharts returns the chart series behind the statistics page. It is a
// separate endpoint from /system/stats on purpose: the counts are cheap and are
// what an import is watched with, while these aggregates are heavier and change
// slowly, so they get their own longer memoisation and never hold up the numbers.
// A failed aggregation is answered with 500 rather than empty series, which would
// draw as an empty library.
func (a *API) handleStatsCharts(w http.ResponseWriter, r *http.Request) {
	charts, err := a.service.LibraryCharts(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "collecting library charts failed")
		return
	}
	writeJSON(w, http.StatusOK, charts)
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
