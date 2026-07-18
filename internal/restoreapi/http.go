// Package restoreapi exposes a small, maintainer-only HTTP API over the restore /
// disaster-recovery subsystem: listing the database dumps available in the
// backup bucket and running the catalogue-vs-originals integrity check. Both are
// read-only and safe to call against a running server.
//
// The destructive database restore itself is deliberately NOT exposed over HTTP:
// restoring the database underneath a running server would drop the tables the
// server is actively using. That operation lives only in the `kukatko restore
// db` command, which is run with the server stopped during disaster recovery.
//
// When no backup destination is configured the service is nil: both endpoints
// report it as unavailable (503), so the API can always be mounted.
package restoreapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/backup"
)

// Service is the subset of backup.RestoreService the API needs: listing dumps
// and running the integrity check. It is an interface so the API can be tested
// with a fake. A nil Service means no backup destination is configured.
type Service interface {
	// ListDumps returns the database dumps available in the bucket, newest first.
	ListDumps(ctx context.Context) ([]backup.DumpInfo, error)
	// Verify reconciles the catalogue against the originals on disk.
	Verify(ctx context.Context) (backup.VerifyReport, error)
}

// API exposes the restore subsystem over HTTP. The maintainer route guard is
// supplied by the caller (the auth subsystem).
type API struct {
	svc               Service
	requireMaintainer func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI. Service may be nil (no backup
// destination configured); RequireMaintainer is required.
type Config struct {
	// Service lists dumps and verifies integrity; nil when no destination exists.
	Service Service
	// RequireMaintainer guards every endpoint: restore is a maintainer operation.
	RequireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{svc: cfg.Service, requireMaintainer: cfg.RequireMaintainer}
}

// RegisterRoutes mounts the restore endpoints onto r, which the caller has
// scoped under the API base path (for example /api/v1). Both routes require
// maintainer:
//
//	GET  /restore/dumps   list available database dumps
//	POST /restore/verify  run the catalogue/originals integrity check
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/restore", func(r chi.Router) {
		r.With(a.requireMaintainer).Get("/dumps", a.handleListDumps)
		r.With(a.requireMaintainer).Post("/verify", a.handleVerify)
	})
}

// dumpsResponse is the JSON body listing available dumps.
type dumpsResponse struct {
	Dumps []backup.DumpInfo `json:"dumps"`
}

// handleListDumps returns the dumps available in the bucket, newest first. An
// unconfigured destination yields 503.
func (a *API) handleListDumps(w http.ResponseWriter, r *http.Request) {
	if a.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "backup destination not configured")
		return
	}
	dumps, err := a.svc.ListDumps(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "listing dumps failed")
		return
	}
	writeJSON(w, http.StatusOK, dumpsResponse{Dumps: dumps})
}

// handleVerify runs the integrity check and returns the report. An unconfigured
// destination yields 503.
func (a *API) handleVerify(w http.ResponseWriter, r *http.Request) {
	if a.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "backup destination not configured")
		return
	}
	report, err := a.svc.Verify(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "integrity check failed")
		return
	}
	writeJSON(w, http.StatusOK, report)
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
		log.Printf("restoreapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
