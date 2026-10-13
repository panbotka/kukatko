// Package rekeyapi is the system administrator's HTTP control of a project's
// storage move (internal/rekey): read the run, start it, pause, resume and
// retry it.
//
// Every route is RequireMaintainer and mounted once under /api/v1 beside the
// instance-level project management (internal/projectapi) — the move is about
// where the instance keeps a project's objects, not about one library's
// content. Every action is audited, with the project, by the move's own service
// in the transaction that makes it, and starting, resuming and retrying
// schedule the storage_rekey job in that same transaction; the worker does the
// moving. Errors are a stable machine-readable code — {"error":"already_running"}.
package rekeyapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/rekey"
)

// Mover is the part of the storage move the routes drive. It is satisfied by
// *rekey.Service; an interface so the handlers can be unit-tested with a fake.
type Mover interface {
	// Status returns the project's most recent run, or rekey.ErrNoRun.
	Status(ctx context.Context, projectUID string) (rekey.Run, error)
	// Start begins the move of the project, audited, and schedules its job.
	Start(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
	// Pause stops the project's running run, audited.
	Pause(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
	// Resume puts the project's paused run back to running, audited, and
	// schedules its job.
	Resume(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
	// Retry puts the project's failed run back to running, audited, and
	// schedules its job.
	Retry(ctx context.Context, meta audit.Meta, projectUID string) (rekey.Run, error)
}

// compile-time assertion that the real service is what the routes drive.
var _ Mover = (*rekey.Service)(nil)

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Mover reads and drives the runs.
	Mover Mover
	// RequireMaintainer guards every route: the system administrator only.
	RequireMaintainer func(http.Handler) http.Handler
}

// API serves the storage move's routes.
type API struct {
	mover             Mover
	requireMaintainer func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{mover: cfg.Mover, requireMaintainer: cfg.RequireMaintainer}
}

// RegisterRoutes mounts the routes onto r, which the caller has scoped under the
// API base path (/api/v1), instance-level:
//
//	GET  /projects/{uid}/rekey          the project's latest run (404 when none)
//	POST /projects/{uid}/rekey          start the move (201)
//	POST /projects/{uid}/rekey/pause    pause the running run
//	POST /projects/{uid}/rekey/resume   resume the paused run
//	POST /projects/{uid}/rekey/retry    retry the failed run
//
// All of them RequireMaintainer. They are flat routes rather than a /projects
// sub-router: internal/projectapi owns that prefix's sub-router, and chi's tree
// routes the more specific paths here.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(a.requireMaintainer)
		r.Get("/projects/{uid}/rekey", a.handleStatus)
		r.Post("/projects/{uid}/rekey", a.handleStart)
		r.Post("/projects/{uid}/rekey/pause", a.transition(a.mover.Pause, "pausing"))
		r.Post("/projects/{uid}/rekey/resume", a.transition(a.mover.Resume, "resuming"))
		r.Post("/projects/{uid}/rekey/retry", a.transition(a.mover.Retry, "retrying"))
	})
}

// handleStatus serves the project's latest run.
func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	run, err := a.mover.Status(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeMoveError(w, err, "reading the run")
		return
	}
	writeJSON(w, http.StatusOK, run)
}

// handleStart starts the project's move and answers the new run.
func (a *API) handleStart(w http.ResponseWriter, r *http.Request) {
	run, err := a.mover.Start(r.Context(), auditMeta(r), chi.URLParam(r, "uid"))
	if err != nil {
		writeMoveError(w, err, "starting the move")
		return
	}
	writeJSON(w, http.StatusCreated, run)
}

// transition returns the handler of one operator transition: it applies move
// to the project and answers the run as it now stands.
func (a *API) transition(
	move func(context.Context, audit.Meta, string) (rekey.Run, error), doing string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		run, err := move(r.Context(), auditMeta(r), chi.URLParam(r, "uid"))
		if err != nil {
			writeMoveError(w, err, doing+" the move")
			return
		}
		writeJSON(w, http.StatusOK, run)
	}
}

// auditMeta is the audit envelope of an action, attributed to the signed-in
// maintainer (RequireMaintainer guarantees one).
func auditMeta(r *http.Request) audit.Meta {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
}

// The machine-readable error codes, the "error" field of every refusal.
const (
	codeNotFound        = "not_found"
	codeAlreadyRunning  = "already_running"
	codeAlreadyPrefixed = "already_prefixed"
	codeInvalidState    = "invalid_state"
	codeInternal        = "internal"
)

// moveErrors maps the move's sentinel errors onto refusals; the first match
// wins.
var moveErrors = []struct {
	sentinel error
	status   int
	code     string
}{
	{rekey.ErrNoRun, http.StatusNotFound, codeNotFound},
	{rekey.ErrProjectNotFound, http.StatusNotFound, codeNotFound},
	{rekey.ErrRunActive, http.StatusConflict, codeAlreadyRunning},
	{rekey.ErrAlreadyPrefixed, http.StatusConflict, codeAlreadyPrefixed},
	{rekey.ErrInvalidState, http.StatusConflict, codeInvalidState},
}

// errorBody is the wire shape of every refusal.
type errorBody struct {
	Error string `json:"error"`
}

// writeMoveError writes the refusal err stands for, logging the unexpected ones
// (the client only learns "internal") with what was being done.
func writeMoveError(w http.ResponseWriter, err error, doing string) {
	for _, known := range moveErrors {
		if errors.Is(err, known.sentinel) {
			writeJSON(w, known.status, errorBody{Error: known.code})
			return
		}
	}
	log.Printf("rekeyapi: %s: %v", doing, err)
	writeJSON(w, http.StatusInternalServerError, errorBody{Error: codeInternal})
}

// writeJSON encodes v as the response body with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("rekeyapi: writing response: %v", err)
	}
}
