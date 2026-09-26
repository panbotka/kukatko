// Package feedbackapi exposes the persisted-feedback endpoints over HTTP: a user
// telling Kukátko "no, this face is not this person" or "no, this photo should not
// have this label" (rejections), "yes, this face really is this person" (a
// confirmation), and taking any of them back. The endpoints all mutate, so they
// are guarded: the face, label and repeated-marker opinions are curation and take
// the curator guard, while the near-duplicate-photo opinions (dismissals and
// confirmations of a pair) stay on the editor write guard, with the merge they
// feed. Each write is recorded in the audit log in the same transaction as the
// change. Recording feedback is an opinion
// only — it never unassigns a face or detaches a label — and it is idempotent, so a
// double POST or a DELETE of something never recorded both answer 204.
//
// The store is an interface and both guards are injected, so the package stays
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
	// ConfirmFace records that a face really IS a subject, idempotently and
	// audited.
	ConfirmFace(ctx context.Context, key feedback.FaceConfirmationKey, entry audit.Entry) error
	// UnconfirmFace takes a face confirmation back, idempotently and audited.
	UnconfirmFace(ctx context.Context, key feedback.FaceConfirmationKey, entry audit.Entry) error
	// DismissDuplicate records that two photos are NOT duplicates of each other,
	// idempotently and audited.
	DismissDuplicate(ctx context.Context, key feedback.DuplicateDismissalKey, entry audit.Entry) error
	// UndismissDuplicate takes a duplicate dismissal back, idempotently and
	// audited.
	UndismissDuplicate(ctx context.Context, key feedback.DuplicateDismissalKey, entry audit.Entry) error
	// ConfirmDuplicate records that two photos really ARE the same shot,
	// idempotently and audited. It merges nothing.
	ConfirmDuplicate(ctx context.Context, key feedback.DuplicateConfirmationKey, entry audit.Entry) error
	// UnconfirmDuplicate takes a duplicate confirmation back, idempotently and
	// audited.
	UnconfirmDuplicate(ctx context.Context, key feedback.DuplicateConfirmationKey, entry audit.Entry) error
	// DismissDuplicateMarkers records that a person really IS marked more than
	// once on a photo, idempotently and audited.
	DismissDuplicateMarkers(
		ctx context.Context, key feedback.DuplicateMarkerDismissalKey, entry audit.Entry,
	) error
	// UndismissDuplicateMarkers takes a repeated-marker dismissal back,
	// idempotently and audited.
	UndismissDuplicateMarkers(
		ctx context.Context, key feedback.DuplicateMarkerDismissalKey, entry audit.Entry,
	) error
}

// API exposes the rejection endpoints over HTTP. The guards are supplied by the
// caller (the auth subsystem) so this package depends on auth's behaviour, not its
// wiring.
type API struct {
	store          Store
	requireCurator func(http.Handler) http.Handler
	requireWrite   func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the rejection reads and writes.
	Store Store
	// RequireCurator guards the face, label and repeated-marker opinions for
	// curators and above.
	RequireCurator func(http.Handler) http.Handler
	// RequireWrite guards the near-duplicate-photo opinions for editors and above.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{store: cfg.Store, requireCurator: cfg.RequireCurator, requireWrite: cfg.RequireWrite}
}

// RegisterRoutes mounts the rejection endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	POST   /feedback/face-rejections     RequireCurator  reject a face↔subject guess
//	DELETE /feedback/face-rejections     RequireCurator  take a face rejection back
//	POST   /feedback/label-rejections    RequireCurator  reject a photo↔label guess
//	DELETE /feedback/label-rejections    RequireCurator  take a label rejection back
//	POST   /feedback/face-confirmations    RequireCurator  confirm a face↔subject assignment
//	DELETE /feedback/face-confirmations    RequireCurator  take a face confirmation back
//	POST   /feedback/duplicate-dismissals  RequireWrite  settle a pair as not duplicates
//	DELETE /feedback/duplicate-dismissals  RequireWrite  take a duplicate dismissal back
//	POST   /feedback/duplicate-confirmations  RequireWrite  settle a pair as the same shot
//	DELETE /feedback/duplicate-confirmations  RequireWrite  take that confirmation back
//	POST   /feedback/duplicate-marker-dismissals  RequireCurator  settle "she really is
//	                                                              marked twice here"
//	DELETE /feedback/duplicate-marker-dismissals  RequireCurator  take that back
//
// The repeated-marker dismissal is face work — the same workflow as
// dupmarkersapi — so it sits with the curator routes; the two near-duplicate-photo
// pairs are not, and stay on RequireWrite with the duplicate merge they settle.
//
// The face, the label, the pair and the (photo, subject) group are named in the
// request body rather than the path, so a DELETE carries a body the same way the
// label-detach endpoint does.
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/feedback", func(r chi.Router) {
		r.With(a.requireCurator).Post("/face-rejections", a.handleFaceReject)
		r.With(a.requireCurator).Delete("/face-rejections", a.handleFaceUnreject)
		r.With(a.requireCurator).Post("/label-rejections", a.handleLabelReject)
		r.With(a.requireCurator).Delete("/label-rejections", a.handleLabelUnreject)
		r.With(a.requireCurator).Post("/face-confirmations", a.handleFaceConfirm)
		r.With(a.requireCurator).Delete("/face-confirmations", a.handleFaceUnconfirm)
		r.With(a.requireWrite).Post("/duplicate-dismissals", a.handleDuplicateDismiss)
		r.With(a.requireWrite).Delete("/duplicate-dismissals", a.handleDuplicateUndismiss)
		r.With(a.requireWrite).Post("/duplicate-confirmations", a.handleDuplicateConfirm)
		r.With(a.requireWrite).Delete("/duplicate-confirmations", a.handleDuplicateUnconfirm)
		r.With(a.requireCurator).Post("/duplicate-marker-dismissals", a.handleMarkerDismiss)
		r.With(a.requireCurator).Delete("/duplicate-marker-dismissals", a.handleMarkerUndismiss)
	})
}

// auditEntry builds an audit entry for a mutation, stamping the acting user
// (resolved from the request's auth context) plus the request's client IP and
// User-Agent onto the given action, target and details. The store writes the
// returned entry inside the mutation's transaction. entry.ActorUID doubles as the
// rejection's rejected_by, so the same user is recorded on the row and in the trail.
//
// The routes are guarded by RequireCurator or RequireWrite, so a principal is
// present in production; an absent principal yields an empty actor UID (stored as
// NULL) rather than failing, which keeps the handlers exercisable behind
// pass-through guards in tests.
func (a *API) auditEntry(
	r *http.Request, action, targetType, targetUID string, details map[string]any,
) audit.Entry {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID).Entry(action, targetType, targetUID, details)
}

// rejectionStatus maps a store error to an HTTP status and client message: an
// incomplete key or an impossible pair is 400, a missing referenced
// photo/subject/label is 404, and anything else is a 500 with a generic message.
func rejectionStatus(err error) (int, string) {
	switch {
	case errors.Is(err, feedback.ErrEmptyKey), errors.Is(err, feedback.ErrSamePhoto):
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
