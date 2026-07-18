// Package maintenanceapi exposes the maintainer-only HTTP endpoints for library
// maintenance: an integrity scan that reports catalogue/disk drift and a repair
// trigger that schedules the opt-in fixes. It depends only on a Service behaviour
// and a maintainer guard, both injected, so it stays decoupled from the
// maintenance and auth wiring. A nil Service answers 503 on every endpoint, so
// the routes mount unconditionally even when maintenance is not wired.
package maintenanceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/maintenance"
)

// maxBodyBytes caps the repair request body; the payload is a handful of boolean
// flags, so a small limit is ample.
const maxBodyBytes = 4 << 10

// Service is the maintenance behaviour the API drives. It is satisfied by
// *maintenance.Service; a nil Service makes every endpoint answer 503.
type Service interface {
	// Scan reconciles the catalogue against disk and derived data.
	Scan(ctx context.Context) (maintenance.Report, error)
	// Repair runs the selected repairs.
	Repair(ctx context.Context, opts maintenance.RepairOptions) (maintenance.RepairResult, error)
}

// API exposes the maintenance endpoints over HTTP behind a maintainer guard.
type API struct {
	service           Service
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service is valid (the
// endpoints answer 503); RequireMaintainer is required.
type Config struct {
	// Service runs scans and repairs; nil means maintenance is not configured.
	Service Service
	// RequireMaintainer guards every endpoint for maintainers only.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, requireMaintainer: cfg.RequireMaintainer}
}

// RegisterRoutes mounts the maintenance endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET  /maintenance/scan    RequireMaintainer  run an integrity scan
//	POST /maintenance/repair  RequireMaintainer  run the selected repairs
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/maintenance", func(r chi.Router) {
		r.With(a.requireMaintainer).Get("/scan", a.handleScan)
		r.With(a.requireMaintainer).Post("/repair", a.handleRepair)
	})
}

// handleScan runs an integrity scan and returns the report. It answers 503 when
// maintenance is not configured and 500 when the scan itself fails.
func (a *API) handleScan(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "maintenance not available")
		return
	}
	report, err := a.service.Scan(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "integrity scan failed")
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// handleRepair decodes the requested repairs and runs them, returning the result.
// It answers 503 when maintenance is not configured, 400 for a malformed body or
// when no repair is selected, and 500 when a repair fails.
func (a *API) handleRepair(w http.ResponseWriter, r *http.Request) {
	if a.service == nil {
		writeError(w, http.StatusServiceUnavailable, "maintenance not available")
		return
	}
	opts, err := decodeRepairOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.Repair(r.Context(), opts)
	if err != nil {
		if errors.Is(err, maintenance.ErrOrphanImportUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "orphan import not configured")
			return
		}
		writeError(w, http.StatusInternalServerError, "repair failed")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// decodeRepairOptions reads and validates the repair request body, rejecting
// unknown fields and a no-op request (no repair selected).
func decodeRepairOptions(r *http.Request) (maintenance.RepairOptions, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	var opts maintenance.RepairOptions
	if err := dec.Decode(&opts); err != nil {
		return maintenance.RepairOptions{}, errors.New("invalid request body")
	}
	if !opts.Any() {
		return maintenance.RepairOptions{}, errors.New("no repair selected")
	}
	return opts, nil
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
		log.Printf("maintenanceapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
