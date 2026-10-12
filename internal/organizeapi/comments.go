package organizeapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/project"
)

// CommentStore is the subset of comments.Store an album's thread needs. It is
// an interface so the handlers depend on behaviour rather than the concrete
// store; comments.Store satisfies it and a test fake can stand in. When nil the
// thread endpoints answer 503 and the album detail reports a zero count.
type CommentStore interface {
	// List returns the live comments on the subject, oldest first.
	List(ctx context.Context, subj comments.Subject) ([]comments.Comment, error)
	// CountsAmong returns the number of live comments per subject UID within the
	// kind; subjects without a comment are absent from the map.
	CountsAmong(ctx context.Context, kind comments.SubjectKind, uids []string) (map[string]int, error)
	// Get returns one live comment, or comments.ErrNotFound.
	Get(ctx context.Context, uid string) (comments.Comment, error)
	// Create stores a comment by authorUID on the subject, auditing it in the
	// same transaction. It returns comments.ErrSubjectNotFound for a missing
	// album and comments.ErrEmptyBody / comments.ErrBodyTooLong for a bad body.
	Create(
		ctx context.Context, subj comments.Subject, authorUID, body string, entry audit.Entry,
	) (comments.Comment, error)
	// Update rewrites a live comment's body, auditing it in the same transaction.
	Update(ctx context.Context, uid, body string, entry audit.Entry) (comments.Comment, error)
	// Delete soft-deletes a live comment, auditing it in the same transaction.
	Delete(ctx context.Context, uid string, entry audit.Entry) error
}

// commentRequest is the JSON body of the create and edit endpoints: the
// plain-text body, trimmed and length-checked by the store.
type commentRequest struct {
	Body string `json:"body"`
}

// commentListResponse is the JSON body of the thread endpoint. Comments is
// always an array (never null) so a client can render an empty thread without
// a guard.
type commentListResponse struct {
	Comments []comments.Comment `json:"comments"`
}

// albumDetail is what GET /albums/{uid} answers: the album plus how many live
// comments its thread holds, so the page can badge the thread without fetching
// it. The count resolves through the bulk CountsAmong with a single UID, as the
// photo detail's does, so it can never quietly become a per-item query.
type albumDetail struct {
	organize.Album
	CommentCount int `json:"comment_count"`
}

// registerCommentRoutes mounts an album's thread under /albums/{uid}/comments.
// Every route is RequireAuth, not RequireCurator, on purpose: whoever can open
// the album (every signed-in role — GET /albums/{uid} is RequireAuth) may read
// its conversation and join it, exactly as on a photograph's or a task's
// thread. The album itself stays a curator's to edit.
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

// handleListComments returns an album's thread, oldest first, each comment with
// its author's resolved name. The album is resolved first, so the thread of an
// album that does not exist is a 404 — the same answer GET /albums/{uid} gives,
// which is the rule the thread mirrors.
func (a *API) handleListComments(w http.ResponseWriter, r *http.Request) {
	if !a.commentsAvailable(w) {
		return
	}
	uid := chi.URLParam(r, "uid")
	if !a.requireAlbum(w, r, uid) {
		return
	}
	list, err := a.comments.List(r.Context(), comments.AlbumSubject(uid))
	if err != nil {
		writeCommentError(w, r, err, "listing comments failed")
		return
	}
	writeJSON(w, http.StatusOK, commentListResponse{Comments: list})
}

// handleCreateComment appends a comment to an album's thread and writes 201. It
// is guarded by RequireAuth rather than RequireCurator: commenting is
// participation, not curation, and a viewer who remembers who else was at the
// wedding is exactly who the thread is for. A blank or over-long body is 400, a
// missing album 404, and the route is rate limited per user.
func (a *API) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	if !a.commentsAvailable(w) {
		return
	}
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	var body commentRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := chi.URLParam(r, "uid")
	created, err := a.comments.Create(r.Context(), comments.AlbumSubject(uid), user.UID, body.Body,
		commentEntry(r, user.UID, audit.ActionCommentCreate, uid))
	if err != nil {
		writeCommentError(w, r, err, "creating comment failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleUpdateComment rewrites the caller's own comment. Only the author may —
// an admin can remove a comment but never put words in someone else's mouth.
func (a *API) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	existing, user, ok := a.resolveComment(w, r)
	if !ok {
		return
	}
	if !canEditComment(existing, user) {
		writeError(w, http.StatusForbidden, "only the author may edit a comment")
		return
	}
	var body commentRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := a.comments.Update(r.Context(), existing.UID, body.Body,
		commentEntry(r, user.UID, audit.ActionCommentUpdate, existing.AlbumUID))
	if err != nil {
		writeCommentError(w, r, err, "updating comment failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteComment soft-deletes the caller's own comment, or any comment when
// the caller is an admin, and writes 204.
func (a *API) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	existing, user, ok := a.resolveComment(w, r)
	if !ok {
		return
	}
	if !canDeleteComment(existing, user, auth.ProjectRoleFromContext(r.Context())) {
		writeError(w, http.StatusForbidden, "only the author or an admin may delete a comment")
		return
	}
	if err := a.comments.Delete(r.Context(), existing.UID,
		commentEntry(r, user.UID, audit.ActionCommentDelete, existing.AlbumUID)); err != nil {
		writeCommentError(w, r, err, "deleting comment failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveComment reads the acting user and the {commentUID} comment, and refuses
// one that belongs to a different album — or to no album at all — with a 404,
// so a comment UID cannot be probed through an unrelated album's thread.
func (a *API) resolveComment(w http.ResponseWriter, r *http.Request) (comments.Comment, auth.User, bool) {
	if !a.commentsAvailable(w) {
		return comments.Comment{}, auth.User{}, false
	}
	user, ok := currentUser(w, r)
	if !ok {
		return comments.Comment{}, auth.User{}, false
	}
	existing, err := a.comments.Get(r.Context(), chi.URLParam(r, "commentUID"))
	if err != nil {
		writeCommentError(w, r, err, "reading comment failed")
		return comments.Comment{}, auth.User{}, false
	}
	if existing.AlbumUID == "" || existing.AlbumUID != chi.URLParam(r, "uid") {
		writeError(w, http.StatusNotFound, "comment not found")
		return comments.Comment{}, auth.User{}, false
	}
	return existing, user, true
}

// currentUser reads the acting user off the request, answering 401 when the
// guard let nobody through (unreachable in production, where every thread route
// is RequireAuth; a pass-through guard in a unit test can get here).
func currentUser(w http.ResponseWriter, r *http.Request) (auth.User, bool) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return auth.User{}, false
	}
	return user, true
}

// canEditComment reports whether user may rewrite the comment: only its author,
// and only when the comment still has one (an authorless comment, left behind
// by a deleted account, is editable by nobody).
func canEditComment(c comments.Comment, user auth.User) bool {
	return c.AuthorUID != "" && c.AuthorUID == user.UID
}

// canDeleteComment reports whether user may remove the comment: its author, or
// an admin (and a maintainer, up the ladder) moderating anyone's.
//
// role is the caller's role in the request's project.
func canDeleteComment(c comments.Comment, user auth.User, role project.Role) bool {
	return canEditComment(c, user) || role.IsAdmin()
}

// commentsAvailable reports whether a comment store is configured, writing 503
// when it is not. The album itself still reads and edits without one.
func (a *API) commentsAvailable(w http.ResponseWriter) bool {
	if a.comments == nil {
		writeError(w, http.StatusServiceUnavailable, "comments are unavailable")
		return false
	}
	return true
}

// commentCount returns how many live comments the album has, for the detail
// payload's badge. A nil comments backend reports zero rather than failing the
// detail.
func (a *API) commentCount(ctx context.Context, uid string) (int, error) {
	if a.comments == nil {
		return 0, nil
	}
	counts, err := a.comments.CountsAmong(ctx, comments.SubjectAlbum, []string{uid})
	if err != nil {
		return 0, fmt.Errorf("organizeapi: counting comments: %w", err)
	}
	return counts[uid], nil
}

// commentEntry builds the audit entry for a thread mutation. The target is the
// album the comment hangs off; the store adds the comment's own UID to the
// details.
func commentEntry(r *http.Request, actorUID, action, albumUID string) audit.Entry {
	return audit.FromRequest(r, actorUID).Entry(action, "albums", albumUID, nil)
}

// writeCommentError maps a comments store error to an HTTP response: 404 for a
// missing album or comment, 400 for an invalid body, otherwise 500 with failMsg
// (the cause logged through fail, as every other 5xx of this package is).
func writeCommentError(w http.ResponseWriter, r *http.Request, err error, failMsg string) {
	switch {
	case errors.Is(err, comments.ErrSubjectNotFound):
		writeError(w, http.StatusNotFound, "album not found")
	case errors.Is(err, comments.ErrNotFound):
		writeError(w, http.StatusNotFound, "comment not found")
	case errors.Is(err, comments.ErrEmptyBody):
		writeError(w, http.StatusBadRequest, "comment body is empty")
	case errors.Is(err, comments.ErrBodyTooLong):
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("comment body is longer than %d characters", comments.MaxBodyLen))
	default:
		fail(w, r, http.StatusInternalServerError, failMsg, err)
	}
}
