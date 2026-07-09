// Package peopleapi exposes the subject (people/pet/other) catalogue over HTTP:
// listing subjects with their photo counts, fetching and editing a single
// subject, and paging a subject's photos. It is the read/curation surface the
// People UI is built on, complementing the face-assignment, cluster and outlier
// endpoints. Reads are open to any authenticated user; mutations require the
// editor/admin write guard, both injected so this package stays decoupled from
// auth's wiring and from the people/photos package internals (the stores are
// interfaces, fakeable in tests).
package peopleapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// defaultPageLimit is the page size used when a subject-photos request omits or
// zeroes the limit parameter.
const defaultPageLimit = 100

// maxPageLimit caps the subject-photos page size so a single request cannot pull
// an unbounded number of rows.
const maxPageLimit = 500

// SubjectStore is the subset of people.Store the API needs. It is an interface so
// peopleapi depends on the behaviour rather than the concrete store, which keeps
// the handlers unit-testable with fakes.
type SubjectStore interface {
	// ListSubjects returns every subject with its non-invalid marker count.
	ListSubjects(ctx context.Context) ([]people.SubjectCount, error)
	// GetSubjectByUID returns one subject or people.ErrSubjectNotFound.
	GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error)
	// CreateSubject inserts a subject and returns it with its generated UID/slug.
	CreateSubject(ctx context.Context, subj people.Subject) (people.Subject, error)
	// UpdateSubject rewrites a subject's editable fields, or returns
	// people.ErrSubjectNotFound.
	UpdateSubject(ctx context.Context, uid string, upd people.SubjectUpdate) (people.Subject, error)
	// DeleteSubject removes a subject, or returns people.ErrSubjectNotFound.
	DeleteSubject(ctx context.Context, uid string) error
	// ListPhotoUIDsBySubject returns the subject's photo UIDs, newest first.
	ListPhotoUIDsBySubject(ctx context.Context, subjectUID string) ([]string, error)
}

// PhotoStore is the subset of photos.Store the API needs to resolve a page of
// subject photo UIDs into full photo records.
type PhotoStore interface {
	// ListByUIDs returns the photos for the given UIDs in unspecified order.
	ListByUIDs(ctx context.Context, uids []string) ([]photos.Photo, error)
}

// API exposes the subject endpoints over HTTP. The auth middlewares are supplied
// by the caller so this package depends on auth's behaviour, not its wiring.
type API struct {
	subjects SubjectStore
	photos   PhotoStore
	// media stamps the thumb/download URLs onto every photo this API returns.
	media        *mediaurl.Builder
	requireAuth  func(http.Handler) http.Handler
	requireWrite func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Subjects backs the subject reads and mutations.
	Subjects SubjectStore
	// Photos resolves subject photo UIDs to full records.
	Photos PhotoStore
	// Storage decides where a client fetches the returned photos' media. A nil
	// storage points them at this application's own media routes.
	Storage storage.Storage
	// RequireAuth guards the read endpoints for any signed-in user.
	RequireAuth func(http.Handler) http.Handler
	// RequireWrite guards the mutating endpoints for editors and admins.
	RequireWrite func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		subjects:     cfg.Subjects,
		photos:       cfg.Photos,
		media:        mediaurl.NewBuilder(cfg.Storage),
		requireAuth:  cfg.RequireAuth,
		requireWrite: cfg.RequireWrite,
	}
}

// RegisterRoutes mounts the subject endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET    /subjects               RequireAuth   list subjects with photo counts
//	POST   /subjects               RequireWrite  create a subject
//	GET    /subjects/{uid}         RequireAuth   one subject
//	PATCH  /subjects/{uid}         RequireWrite  edit a subject's fields
//	DELETE /subjects/{uid}         RequireWrite  delete a subject
//	GET    /subjects/{uid}/photos  RequireAuth   a subject's photos (paginated)
//
// Flat patterns (rather than a mounted subrouter) are used so this group can
// coexist on the same router with outlierapi's GET /subjects/{uid}/outliers
// without a chi Mount conflict.
func (a *API) RegisterRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/subjects", a.handleList)
	r.With(a.requireWrite).Post("/subjects", a.handleCreate)
	r.With(a.requireAuth).Get("/subjects/{uid}", a.handleGet)
	r.With(a.requireWrite).Patch("/subjects/{uid}", a.handleUpdate)
	r.With(a.requireWrite).Delete("/subjects/{uid}", a.handleDelete)
	r.With(a.requireAuth).Get("/subjects/{uid}/photos", a.handlePhotos)
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
		log.Printf("peopleapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// subjectStatus maps a store error to the HTTP status and client message used for
// subject mutations: a missing subject is 404, an invalid type is 400, anything
// else is a 500 with a generic message.
func subjectStatus(err error) (int, string) {
	switch {
	case errors.Is(err, people.ErrSubjectNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, people.ErrInvalidType):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, "subject operation failed"
	}
}
