// Package feedbackapi exposes the persisted-rejection endpoints over HTTP: a user
// telling Kukátko "no, this face is not this person" or "no, this photo should not
// have this label", and taking either back. The four endpoints all mutate, so they
// are guarded by the editor/admin write guard; each write is recorded in the audit
// log in the same transaction as the rejection. Recording a rejection is an opinion
// only — it never unassigns a face or detaches a label — and it is idempotent, so a
// double POST or a DELETE of something never rejected both answer 204.
//
// The store is an interface and the write guard is injected, so the package stays
// decoupled from the persistence and auth wiring and is unit-testable with fakes.
package feedbackapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/feedback"
)

// Store is the subset of feedback.Store the endpoints need. It is an interface so
// the handlers depend on behaviour rather than the concrete store, keeping them
// unit-testable with fakes; *feedback.Store satisfies it. Every mutation takes an
// audit.Entry the store writes in the same transaction as the change.
type Store interface {
	// RejectFace records that a face is NOT a subject, idempotently and audited.
	RejectFace(ctx context.Context, key feedback.FaceRejectionKey, entry audit.Entry) error
	// UnrejectFace takes a face rejection back, idempotently and audited.
	UnrejectFace(ctx context.Context, key feedback.FaceRejectionKey, entry audit.Entry) error
	// RejectLabel records that a photo should NOT have a label, idempotently and
	// audited.
	RejectLabel(ctx context.Context, key feedback.LabelRejectionKey, entry audit.Entry) error
	// UnrejectLabel takes a label rejection back, idempotently and audited.
	UnrejectLabel(ctx context.Context, key feedback.LabelRejectionKey, entry audit.Entry) error
}

// API exposes the rejection endpoints over HTTP. The write guard is supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	store        Store
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the rejection reads and writes.
	Store Store
	// RequireWrite guards every endpoint for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the rejection endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST   /feedback/face-rejections    RequireWrite  reject a face↔subject guess
//	DELETE /feedback/face-rejections    RequireWrite  take a face rejection back
//	POST   /feedback/label-rejections   RequireWrite  reject a photo↔label guess
//	DELETE /feedback/label-rejections   RequireWrite  take a label rejection back
//
// The face and the label are named in the request body rather than the path, so a
// DELETE carries a body the same way the label-detach endpoint does.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/feedback", func(r chi.Router) {
		r.With(a.requireWrite).Post("/face-rejections", a.handleFaceReject)
		r.With(a.requireWrite).Delete("/face-rejections", a.handleFaceUnreject)
		r.With(a.requireWrite).Post("/label-rejections", a.handleLabelReject)
		r.With(a.requireWrite).Delete("/label-rejections", a.handleLabelUnreject)
	})
}

// auditEntry builds an audit entry for a mutation, stamping the acting user
// (resolved from the request's auth context) plus the request's client IP and
// User-Agent onto the given action, target and details. The store writes the
// returned entry inside the mutation's transaction. entry.ActorUID doubles as the
// rejection's rejected_by, so the same user is recorded on the row and in the trail.
//
// The routes are guarded by RequireWrite, so a principal is present in production;
// an absent principal yields an empty actor UID (stored as NULL) rather than
// failing, which keeps the handlers exercisable behind pass-through guards in tests.
func (a *API) auditEntry(
	r *http.Request, action, targetType, targetUID string, details map[string]any,
) audit.Entry {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID).Entry(action, targetType, targetUID, details)
}

// rejectionStatus maps a store error to an HTTP status and client message: an
// incomplete key is 400, a missing referenced photo/subject/label is 404, and
// anything else is a 500 with a generic message.
func rejectionStatus(err error) (int, string) {
	switch {
	case errors.Is(err, feedback.ErrEmptyKey):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, feedback.ErrTargetNotFound):
		return http.StatusNotFound, err.Error()
	default:
		return http.StatusInternalServerError, "rejection operation failed"
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
		log.Printf("feedbackapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}
