// Package phototaskapi exposes the work queue over HTTP: the tasks themselves
// and the conversation that answers them.
//
// # Who may do what
//
// Reading a task and writing in its thread are open to every authenticated role,
// viewers included; opening, editing, closing and changing membership need write
// access. That split is the point of the feature rather than an accident of it —
// the person who knows in which year the house was rebuilt is rarely the person
// who edits the library, and asking them to answer means letting them answer.
// It mirrors the one documented exception to the read-only rule, per-photo
// comments (see docs/API.md).
//
// # No routes for a task's photographs
//
// A task's photographs are read through the catalogue with GET /photos?task=…,
// not from a route here. They are photographs like any other — the same
// projection, the same signed media URLs, the same paging — and giving a task its
// own gallery endpoint would mean a second implementation of all of it that would
// drift from the first.
//
// The stores are interfaces and the auth guards are injected, so the package
// stays decoupled from their construction and is unit-testable with fakes.
package phototaskapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
)

// Store is the subset of phototask.Store the endpoints need. It is an interface
// so the handlers depend on behaviour rather than the concrete store, keeping
// them unit-testable with fakes.
type Store interface {
	// List returns one page of matching tasks and the total before paging.
	List(ctx context.Context, f phototask.Filter) ([]phototask.Task, int, error)
	// Summary returns the queue's counts as callerUID sees them.
	Summary(ctx context.Context, callerUID string) (phototask.Summary, error)
	// Get returns one task as callerUID sees it (the two caller-relative flags
	// are computed for them), or phototask.ErrNotFound.
	Get(ctx context.Context, uid, callerUID string) (phototask.Task, error)
	// Create opens a task over opening's photographs (possibly none) with its
	// people asked, auditing it all in the same transaction.
	Create(
		ctx context.Context, t phototask.Task, opening phototask.Opening, entry audit.Entry,
	) (phototask.Task, error)
	// Update folds a partial change onto a task, auditing it in the same
	// transaction.
	Update(
		ctx context.Context, uid string, upd phototask.Update, entry audit.Entry,
	) (phototask.Task, error)
	// Delete removes a task, or returns phototask.ErrNotFound.
	Delete(ctx context.Context, uid string, entry audit.Entry) error
	// AddPhotos adds photographs to a task and reports how many were new.
	AddPhotos(ctx context.Context, uid string, photoUIDs []string, entry audit.Entry) (int, error)
	// RemovePhotos drops photographs from a task and reports how many were removed.
	RemovePhotos(ctx context.Context, uid string, photoUIDs []string, entry audit.Entry) (int, error)
	// Participants returns the people on a task, oldest membership first.
	Participants(ctx context.Context, uid string) ([]phototask.Participant, error)
	// Assign puts somebody on a task on purpose, auditing it in the same
	// transaction.
	Assign(ctx context.Context, uid, userUID string, entry audit.Entry) error
	// Unassign takes somebody off a task, reporting whether a row was removed.
	Unassign(ctx context.Context, uid, userUID string, entry audit.Entry) (bool, error)
	// Join records that somebody acted on a task. Idempotent and unaudited: the
	// act it follows is what the audit trail records.
	Join(ctx context.Context, uid, userUID string) error
}

// CommentStore is the subset of comments.Store a task's thread needs. It is the
// same store the photo threads use, addressed with a task subject.
type CommentStore interface {
	// List returns the live comments on the subject, oldest first.
	List(ctx context.Context, subj comments.Subject) ([]comments.Comment, error)
	// Get returns one live comment, or comments.ErrNotFound.
	Get(ctx context.Context, uid string) (comments.Comment, error)
	// Create stores a comment by authorUID on the subject, auditing it in the
	// same transaction.
	Create(
		ctx context.Context, subj comments.Subject, authorUID, body string, entry audit.Entry,
	) (comments.Comment, error)
	// Update rewrites a live comment's body, auditing it in the same transaction.
	Update(ctx context.Context, uid, body string, entry audit.Entry) (comments.Comment, error)
	// Delete soft-deletes a live comment, auditing it in the same transaction.
	Delete(ctx context.Context, uid string, entry audit.Entry) error
}

// API exposes the task endpoints over HTTP. The auth guards are supplied by the
// caller so this package depends on auth's behaviour, not its wiring.
type API struct {
	store           Store
	comments        CommentStore
	requireAuth     func(http.Handler) http.Handler
	requireWrite    func(http.Handler) http.Handler
	commentThrottle func(http.Handler) http.Handler
}

// Config bundles the dependencies of NewAPI.
type Config struct {
	// Store backs the task reads and mutations.
	Store Store
	// Comments backs a task's thread. When nil the thread endpoints answer 503
	// and a task still reads and edits normally.
	Comments CommentStore
	// RequireAuth guards the reads and the thread for any signed-in role.
	RequireAuth func(http.Handler) http.Handler
	// RequireWrite guards opening, editing and closing a task.
	RequireWrite func(http.Handler) http.Handler
	// CommentThrottle rate-limits posting to a thread, keyed on the acting user.
	// Optional; without it a thread is bounded only by the body size.
	CommentThrottle func(http.Handler) http.Handler
}

// NewAPI returns an API from cfg.
func NewAPI(cfg Config) *API {
	return &API{
		store: cfg.Store, comments: cfg.Comments,
		requireAuth: cfg.RequireAuth, requireWrite: cfg.RequireWrite,
		commentThrottle: cfg.CommentThrottle,
	}
}

// RegisterRoutes mounts the task endpoints onto r, which the caller has scoped
// under the API base path (for example /api/v1):
//
//	GET    /tasks                          list tasks (any role)
//	GET    /tasks/summary                  the counts behind the badge and chips (any role)
//	POST   /tasks                          open a task
//	GET    /tasks/{uid}                    read one (any role)
//	PATCH  /tasks/{uid}                    edit or advance one
//	DELETE /tasks/{uid}                    delete one
//	POST   /tasks/{uid}/photos             add photographs
//	DELETE /tasks/{uid}/photos             remove photographs
//	GET    /tasks/{uid}/participants       who is on it (any role)
//	POST   /tasks/{uid}/participants       put somebody on it
//	DELETE /tasks/{uid}/participants/{u}   take somebody off it
//	GET    /tasks/{uid}/comments           read the thread (any role)
//	POST   /tasks/{uid}/comments           answer (any role, including viewers)
//	PATCH  /tasks/{uid}/comments/{cuid}    edit one's own answer
//	DELETE /tasks/{uid}/comments/{cuid}    delete one's own answer (admins: any)
func (a *API) RegisterRoutes(r chi.Router) {
	r.Route("/tasks", func(r chi.Router) {
		r.With(a.requireAuth).Get("/", a.handleList)
		// A static segment: chi matches it ahead of /{uid} whatever the order.
		r.With(a.requireAuth).Get("/summary", a.handleSummary)
		r.With(a.requireWrite).Post("/", a.handleCreate)
		r.With(a.requireAuth).Get("/{uid}", a.handleGet)
		r.With(a.requireWrite).Patch("/{uid}", a.handleUpdate)
		r.With(a.requireWrite).Delete("/{uid}", a.handleDelete)
		r.With(a.requireWrite).Post("/{uid}/photos", a.handleAddPhotos)
		r.With(a.requireWrite).Delete("/{uid}/photos", a.handleRemovePhotos)
		r.With(a.requireAuth).Get("/{uid}/participants", a.handleListParticipants)
		r.With(a.requireWrite).Post("/{uid}/participants", a.handleAssign)
		r.With(a.requireWrite).Delete("/{uid}/participants/{userUID}", a.handleUnassign)
		a.registerCommentRoutes(r)
	})
}

// registerCommentRoutes mounts a task's thread. Every route is RequireAuth, not
// RequireWrite: answering is what a viewer is here for.
func (a *API) registerCommentRoutes(r chi.Router) {
	r.With(a.requireAuth).Get("/{uid}/comments", a.handleListComments)
	post := r.With(a.requireAuth)
	if a.commentThrottle != nil {
		post = post.With(a.commentThrottle)
	}
	post.Post("/{uid}/comments", a.handleCreateComment)
	r.With(a.requireAuth).Patch("/{uid}/comments/{commentUID}", a.handleUpdateComment)
	r.With(a.requireAuth).Delete("/{uid}/comments/{commentUID}", a.handleDeleteComment)
}

// currentUser returns the authenticated user from the request context, writing a
// 401 and reporting ok=false when none is present (a defensive guard; the auth
// middleware should have already rejected the request).
func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return auth.User{}, false
	}
	return user, true
}

// taskEntry builds the audit entry for a task mutation: the task is the target,
// and the store fills the UID in for a task that does not have one yet.
func taskEntry(r *http.Request, actorUID, action, taskUID string) audit.Entry {
	return audit.FromRequest(r, actorUID).Entry(action, "photo_tasks", taskUID, nil)
}

// writeTaskError maps a task store error to an HTTP response: 404 for a missing
// task or photograph, 400 for anything the caller could fix, otherwise 500 with
// fallback.
func writeTaskError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, phototask.ErrNotFound):
		writeError(w, http.StatusNotFound, "task not found")
	case errors.Is(err, phototask.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, phototask.ErrEmptyTitle),
		errors.Is(err, phototask.ErrTooLong),
		errors.Is(err, phototask.ErrInvalidState),
		errors.Is(err, phototask.ErrClosedNeedsResolution),
		errors.Is(err, phototask.ErrTooManyPhotos),
		errors.Is(err, phototask.ErrTooManyOptions),
		errors.Is(err, phototask.ErrEmptyOption),
		errors.Is(err, phototask.ErrDuplicateOption):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, phototask.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user not found")
	default:
		writeError(w, http.StatusInternalServerError, fallback)
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
		log.Printf("phototaskapi: encoding JSON response: %v", err)
	}
}

// writeError writes an error response with the given status code and message.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

// taskUID returns the {uid} path parameter.
func taskUID(r *http.Request) string {
	return chi.URLParam(r, "uid")
}
