// Package maintenanceapi exposes the maintainer-only HTTP endpoints for library
// maintenance: an integrity scan that reports catalogue/disk drift, a repair
// trigger that schedules the opt-in fixes, and a retention purge of old audit-log
// entries. It depends only on injected behaviours (a maintenance Service, an audit
// purger) and a maintainer guard, so it stays decoupled from the maintenance and
// auth wiring. A nil Service answers 503 on the scan/repair endpoints and a nil
// AuditPurger answers 503 on the purge endpoint, so the routes mount
// unconditionally even when a collaborator is not wired.
package maintenanceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/maintenance"
)

// maxBodyBytes caps the scan/repair/purge request bodies; the payloads are a
// handful of flags or a single retention window, so a small limit is ample.
const maxBodyBytes = 4 << 10

// maxAuditRetentionDays caps the accepted audit-purge retention window. It is
// deliberately generous (100 years) — the bound only rejects absurd input, it is
// not a retention policy.
const maxAuditRetentionDays = 36500

// Service is the maintenance behaviour the API drives. It is satisfied by
// *maintenance.Service; a nil Service makes the scan/repair endpoints answer 503.
type Service interface {
	// Scan reconciles the catalogue against disk and derived data.
	Scan(ctx context.Context) (maintenance.Report, error)
	// Repair runs the selected repairs.
	Repair(ctx context.Context, opts maintenance.RepairOptions) (maintenance.RepairResult, error)
}

// AuditPurger deletes old audit entries by retention and records standalone audit
// entries. It is satisfied by *audit.Store; a nil AuditPurger makes the purge
// endpoint answer 503.
type AuditPurger interface {
	// PurgeOlderThan deletes audit entries older than cutoff, returning the count.
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int, error)
	// Record writes one standalone audit entry — here the self-audit of the purge.
	Record(ctx context.Context, entry audit.Entry) error
}

// API exposes the maintenance endpoints over HTTP behind a maintainer guard.
type API struct {
	service           Service
	audit             AuditPurger
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. A nil Service and a nil Audit are
// both valid (their endpoints answer 503); RequireMaintainer is required.
type Config struct {
	// Service runs scans and repairs; nil means maintenance is not configured.
	Service Service
	// Audit purges old audit entries and records the purge; nil disables the purge.
	Audit AuditPurger
	// RequireMaintainer guards every endpoint for maintainers only.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{service: cfg.Service, audit: cfg.Audit, requireMaintainer: cfg.RequireMaintainer}
}

// RegisterRoutes mounts the maintenance endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1):
//
//	GET  /maintenance/scan         RequireMaintainer  run an integrity scan
//	POST /maintenance/repair       RequireMaintainer  run the selected repairs
//	POST /maintenance/audit/purge  RequireMaintainer  delete old audit entries (retention)
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/maintenance", func(r chi.Router) {
		r.With(a.requireMaintainer).Get("/scan", a.handleScan)
		r.With(a.requireMaintainer).Post("/repair", a.handleRepair)
		r.With(a.requireMaintainer).Post("/audit/purge", a.handleAuditPurge)
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

// auditPurgeRequest is the audit-purge body: a positive retention window in days.
// Entries older than now minus this window are deleted.
type auditPurgeRequest struct {
	OlderThanDays int `json:"older_than_days"`
}

// auditPurgeResponse reports the outcome of a purge: how many entries were
// deleted, the retention window applied and the resulting cutoff instant (RFC 3339).
type auditPurgeResponse struct {
	Deleted       int    `json:"deleted"`
	OlderThanDays int    `json:"older_than_days"`
	Cutoff        string `json:"cutoff"`
}

// handleAuditPurge deletes audit entries older than the requested retention
// window and writes a recent self-audit record for the purge itself, so deleting
// the audit trail stays traceable. It answers 503 when the audit store is not
// wired, 400 for a malformed body or a non-positive/oversized window, and 500
// when the purge fails.
func (a *API) handleAuditPurge(w http.ResponseWriter, r *http.Request) {
	if a.audit == nil {
		writeError(w, http.StatusServiceUnavailable, "audit purge not available")
		return
	}
	days, err := decodeAuditPurge(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	deleted, err := a.audit.PurgeOlderThan(r.Context(), cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "audit purge failed")
		return
	}
	a.recordAuditPurge(r, days, cutoff, deleted)
	writeJSON(w, http.StatusOK, auditPurgeResponse{
		Deleted: deleted, OlderThanDays: days, Cutoff: cutoff.UTC().Format(time.RFC3339),
	})
}

// decodeAuditPurge reads and validates the audit-purge body, rejecting a
// malformed payload, unknown fields and a non-positive or oversized retention
// window (the window must be a whole number of days in [1, maxAuditRetentionDays]).
func decodeAuditPurge(r *http.Request) (int, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	var in auditPurgeRequest
	if err := dec.Decode(&in); err != nil {
		return 0, errors.New("invalid request body")
	}
	if in.OlderThanDays < 1 {
		return 0, errors.New("older_than_days must be a positive number of days")
	}
	if in.OlderThanDays > maxAuditRetentionDays {
		return 0, errors.New("older_than_days is too large")
	}
	return in.OlderThanDays, nil
}

// recordAuditPurge writes the self-audit record for a completed purge: the acting
// maintainer (from the auth context) plus the applied cutoff, retention window and
// deleted count in the details. The record post-dates the cutoff, so it survives
// the purge it describes. A write failure is logged rather than surfaced — the
// purge already succeeded and its count was returned — but against the same
// database it is a should-never-happen.
func (a *API) recordAuditPurge(r *http.Request, days int, cutoff time.Time, deleted int) {
	user, _ := auth.UserFromContext(r.Context())
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionAuditPurge, "audit_log", "", map[string]any{
		"older_than_days": days,
		"cutoff":          cutoff.UTC().Format(time.RFC3339),
		"deleted":         deleted,
	})
	if err := a.audit.Record(r.Context(), entry); err != nil {
		log.Printf("maintenanceapi: recording audit purge: %v", err)
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
		log.Printf("maintenanceapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
