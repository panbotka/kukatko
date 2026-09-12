// Package familyapi exposes the genealogy over subjects — internal/family — over
// HTTP: one subject's immediate relations, the tree walked up or down from them,
// recording and removing a relation, and editing the family row itself. Reads are
// open to any authenticated user; the three mutations require the editor/admin
// write guard. Both guards are injected and the store is an interface, so this
// package stays decoupled from auth's wiring and testable with fakes.
//
// Its patterns are flat rather than a mounted subrouter, because the
// /subjects/{uid} prefix is shared with peopleapi and outlierapi and a chi Mount
// would collide with them.
//
// The one endpoint that is more than a thin wrapper is POST …/relations, which
// accepts a subject to create inline instead of an existing one and hands both
// halves to a single audited transaction. That is what makes filling a tree
// bearable: the great-grandmother nobody photographed is recorded from the page
// she is missing on, not from a trip to another screen and back.
package familyapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/people"
)

// Store is the subset of family.Store the API needs. It is an interface so the
// handlers depend on the behaviour rather than on the concrete store, which keeps
// them unit-testable with fakes. Every mutation takes an audit.Entry the store
// writes in the same transaction as the change.
type Store interface {
	// Relations returns the four derived lists of a subject's immediate family,
	// or family.ErrSubjectNotFound.
	Relations(ctx context.Context, subjectUID string) (family.Relations, error)
	// Tree returns the layout-ready tree walked from rootUID in the given
	// direction, bounded by generations (0 = the whole bounded walk).
	Tree(ctx context.Context, rootUID string, direction family.Direction, generations int) (family.Tree, error)
	// AddRelationAudited records a relation on the subject, creating the person on
	// the other side first when the request described one instead of naming it.
	AddRelationAudited(
		ctx context.Context, subjectUID string, rel family.AddRelation, entry audit.Entry,
	) (family.AddResult, error)
	// RemoveRelationAudited removes whatever relation ties the two subjects
	// together, or returns family.ErrRelationNotFound.
	RemoveRelationAudited(ctx context.Context, subjectUID, otherUID string, entry audit.Entry) error
	// GetFamily returns one family row, or family.ErrFamilyNotFound.
	GetFamily(ctx context.Context, uid string) (family.Family, error)
	// UpdateFamilyAudited rewrites a family's editable fields — kind, years, note.
	UpdateFamilyAudited(
		ctx context.Context, familyUID string, upd family.Update, entry audit.Entry,
	) (family.Family, error)
}

// API exposes the family endpoints over HTTP. The auth middlewares are supplied
// by the caller so this package depends on auth's behaviour, not its wiring.
type API struct {
	store        Store
	export       ExportEnqueuer
	requireAuth  func(http.Handler) http.Handler
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the relation reads and mutations.
	Store Store
	// Export schedules the rewrite of the library's genealogy export after a
	// mutation. Nil disables it, which is what the wiring passes when the export
	// is switched off.
	Export ExportEnqueuer
	// RequireAuth guards the read endpoints for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireWrite guards the mutating endpoints for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store:        cfg.Store,
		export:       cfg.Export,
		requireAuth:  cfg.RequireAuth,
		requireWrite: cfg.RequireWrite,
	}
}

// RegisterRoutes mounts the family endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET    /subjects/{uid}/relations         RequireAuth   parents, siblings, partners, children
//	POST   /subjects/{uid}/relations         RequireWrite  record a relation (existing or new subject)
//	DELETE /subjects/{uid}/relations/{uid2}  RequireWrite  remove the relation between two subjects
//	GET    /subjects/{uid}/tree              RequireAuth   the tree walked from the subject
//	PATCH  /families/{uid}                   RequireWrite  edit a family: kind, years, note
//
// The tree takes direction=descendants|ancestors (default descendants) and
// generations=N (default: the whole bounded walk).
//
// Flat patterns (rather than a mounted subrouter) are used so this group can
// coexist on the same router with peopleapi's and outlierapi's /subjects routes
// without a chi Mount conflict.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/subjects/{uid}/relations", a.handleRelations)
	r.With(a.requireWrite).Post("/subjects/{uid}/relations", a.handleAddRelation)
	r.With(a.requireWrite).Delete("/subjects/{uid}/relations/{uid2}", a.handleRemoveRelation)
	r.With(a.requireAuth).Get("/subjects/{uid}/tree", a.handleTree)
	r.With(a.requireWrite).Patch("/families/{uid}", a.handleUpdateFamily)
}

// auditEntry builds an audit entry for a family mutation, stamping the acting
// user (resolved from the request's auth context) plus the request's client IP
// and User-Agent onto the given action, target and details. The store writes the
// returned entry inside the mutation's transaction.
//
// The mutating routes are guarded by RequireWrite, so a principal is present in
// production; an absent principal yields an empty actor UID (stored as NULL)
// rather than failing, which keeps the handlers exercisable behind pass-through
// guards in unit tests.
func (a *API) auditEntry(
	r *http.Request, action, targetType, targetUID string, details map[string]any,
) audit.Entry {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID).Entry(action, targetType, targetUID, details)
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
		log.Printf("familyapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// familyStatus maps a store error to the HTTP status and client message.
//
// A missing subject, family or relation is 404. A refusal that is about the
// *state* of the tree rather than the request — a cycle, a second parentage, a
// second family for one couple — is 409, because the same request would have been
// accepted against different rows and the client is being told what is in the way,
// not that it wrote nonsense. Everything the request itself got wrong is 400, and
// only an error this package does not recognise is a 500.
func familyStatus(err error) (int, string) {
	switch {
	case errors.Is(err, family.ErrSubjectNotFound),
		errors.Is(err, family.ErrFamilyNotFound),
		errors.Is(err, family.ErrRelationNotFound),
		errors.Is(err, people.ErrSubjectNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, family.ErrCycle),
		errors.Is(err, family.ErrAlreadyChild),
		errors.Is(err, family.ErrFamilyConflict):
		return http.StatusConflict, err.Error()
	case errors.Is(err, family.ErrSelfRelation),
		errors.Is(err, family.ErrInvalidKind),
		errors.Is(err, family.ErrInvalidYears),
		errors.Is(err, family.ErrAmbiguousRelation),
		errors.Is(err, people.ErrInvalidType),
		errors.Is(err, people.ErrInvalidLifeYears):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, "family operation failed"
	}
}
