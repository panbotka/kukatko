// Package bulkapi exposes the bulk metadata editing endpoint over HTTP. One
// POST /photos/bulk request lists target photo UIDs and an operation set; the
// whole batch is applied transactionally (with an audit-log entry) and the
// response carries a per-photo result summary plus aggregate counts. The
// mutation is guarded by the editor/admin write guard, injected so the package
// stays decoupled from auth's wiring and is unit-testable with fakes.
package bulkapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/bulk"
)

// maxBodyBytes caps the request body. A bulk request is a UID list plus a small
// operation set, so a 4 MiB limit comfortably covers large batches while
// guarding against oversized payloads.
const maxBodyBytes = 4 << 20

// Service is the bulk-apply behaviour the endpoint needs. It is an interface so
// the handler is unit-testable with a fake; *bulk.Service satisfies it.
type Service interface {
	// Apply runs the operations against the target photos for the acting user and
	// returns the per-photo result. See bulk.Service.Apply.
	Apply(ctx context.Context, actorUID string, photoUIDs []string, ops bulk.Operations) (bulk.Result, error)
}

// SidecarEnqueuer schedules a rewrite of a photo's metadata sidecar — the YAML
// file in storage holding its metadata and curation. It is satisfied by
// jobs.Enqueuer. A nil SidecarEnqueuer disables the scheduling.
type SidecarEnqueuer interface {
	// EnqueueSidecar schedules a sidecar write for photoUID.
	EnqueueSidecar(ctx context.Context, photoUID string) error
}

// API exposes the bulk endpoint over HTTP.
type API struct {
	service      Service
	sidecar      SidecarEnqueuer
	requireWrite func(http.Handler) http.Handler
	rateLimit    func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Service applies the bulk operations.
	Service Service
	// Sidecar schedules a sidecar rewrite per updated photo. When nil no sidecar is
	// scheduled and the batch still succeeds.
	Sidecar SidecarEnqueuer
	// RequireWrite guards the endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
	// RateLimit is an optional per-client-IP throttle applied ahead of the auth
	// check. A nil value disables throttling.
	RateLimit func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg. A nil RateLimit disables throttling.
func NewAPI(cfg Config) *API {
	rateLimit := cfg.RateLimit
	if rateLimit == nil {
		rateLimit = passthroughMiddleware
	}
	return &API{
		service:      cfg.Service,
		sidecar:      cfg.Sidecar,
		requireWrite: cfg.RequireWrite,
		rateLimit:    rateLimit,
	}
}

// passthroughMiddleware is a no-op middleware used when no rate limiter is configured.
func passthroughMiddleware(next http.Handler) http.Handler { return next }

// RegisterRoutes mounts the bulk endpoint onto r, scoped by the caller under the
// API base path (for example /api/v1):
//
//	POST /photos/bulk   rate limit + RequireWrite   apply metadata operations to many photos
//
// The rate limiter runs outermost so an abusive batch flood is capped by client
// IP before the auth lookup and the transactional apply.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.rateLimit, a.requireWrite).Post("/photos/bulk", a.handleBulk)
}

// handleBulk decodes the request, resolves the operation set, applies it for the
// acting user and writes the per-photo result. Validation failures return 400, an
// oversized batch returns 413, and other failures return 500. A run with
// per-photo errors still returns 200 with the errors detailed in the body.
func (a *API) handleBulk(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req bulkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ops, err := req.Operations.toOperations()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := a.service.Apply(r.Context(), user.UID, req.PhotoUIDs, ops)
	if err != nil {
		status, msg := bulkStatus(err)
		writeError(w, status, msg)
		return
	}
	a.enqueueSidecars(r.Context(), result)
	writeJSON(w, http.StatusOK, result)
}

// enqueueSidecars schedules a sidecar rewrite for each photo the batch actually
// changed, and is best-effort: a failure is logged and swallowed, never returned.
//
// This is where the spec's "500 photos must enqueue 500 cheap jobs, not write 500
// files inside the request" is honoured. Each enqueue is one small INSERT; the
// files are written later by the worker. The queue's per-photo dedup index
// collapses repeats, so a photo edited twice in quick succession still yields one
// write.
//
// Only the updated photos are scheduled: a skipped or errored photo did not
// change, so its sidecar is still current and rewriting it would be pure I/O for
// nothing.
func (a *API) enqueueSidecars(ctx context.Context, result bulk.Result) {
	if a.sidecar == nil {
		return
	}
	for _, res := range result.Results {
		if res.Status != bulk.StatusUpdated {
			continue
		}
		if err := a.sidecar.EnqueueSidecar(ctx, res.PhotoUID); err != nil {
			log.Printf("bulkapi: enqueuing sidecar for %s: %v", res.PhotoUID, err)
		}
	}
}

// bulkStatus maps a bulk apply error to an HTTP status and client message.
func bulkStatus(err error) (int, string) {
	switch {
	case errors.Is(err, bulk.ErrNoPhotos),
		errors.Is(err, bulk.ErrNoOperations),
		errors.Is(err, bulk.ErrAlbumNotFound),
		errors.Is(err, bulk.ErrLabelNotFound):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, bulk.ErrBatchTooLarge):
		return http.StatusRequestEntityTooLarge, err.Error()
	default:
		return http.StatusInternalServerError, "bulk operation failed"
	}
}

// errorBody is the JSON body returned for error responses.
type errorBody struct {
	Error string `json:"error"`
}

// writeJSON encodes payload as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("bulkapi: encoding JSON response: %v", err)
	}
}

// writeError writes a JSON error body with the given status and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// decodeJSON decodes the request body into dst, rejecting unknown fields and
// bodies larger than maxBodyBytes.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}
