package phototaskapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
)

// commentRequest is the JSON body of the answer and edit endpoints: the
// plain-text body, trimmed and length-checked by the store.
type commentRequest struct {
	Body string `json:"body"`
}

// commentListResponse is the JSON body of the thread endpoint. Comments is
// always an array (never null) so a client can render an empty thread without a
// guard.
type commentListResponse struct {
	Comments []comments.Comment `json:"comments"`
}

// handleListComments returns a task's thread, oldest first, each comment with
// its author's resolved name. Every authenticated role may read it.
func (a *API) handleListComments(w http.ResponseWriter, r *http.Request) {
	if !a.commentsAvailable(w) {
		return
	}
	list, err := a.comments.List(r.Context(), comments.TaskSubject(taskUID(r)))
	if err != nil {
		writeCommentError(w, err, "listing comments failed")
		return
	}
	writeJSON(w, http.StatusOK, commentListResponse{Comments: list})
}

// handleCreateComment appends an answer to a task's thread and writes 201.
//
// It is guarded by RequireAuth rather than RequireWrite on purpose: answering is
// exactly what a viewer account is for here. The person who remembers the year a
// house was rebuilt is not the person who edits the library.
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
	uid := taskUID(r)
	created, err := a.comments.Create(r.Context(), comments.TaskSubject(uid), user.UID, body.Body,
		commentEntry(r, user.UID, audit.ActionCommentCreate, uid))
	if err != nil {
		writeCommentError(w, err, "creating comment failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleUpdateComment rewrites the caller's own comment.
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
		commentEntry(r, user.UID, audit.ActionCommentUpdate, existing.TaskUID))
	if err != nil {
		writeCommentError(w, err, "updating comment failed")
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
	if !canDeleteComment(existing, user) {
		writeError(w, http.StatusForbidden, "only the author or an admin may delete a comment")
		return
	}
	if err := a.comments.Delete(r.Context(), existing.UID,
		commentEntry(r, user.UID, audit.ActionCommentDelete, existing.TaskUID)); err != nil {
		writeCommentError(w, err, "deleting comment failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveComment reads the acting user and the {commentUID} comment, and refuses
// one that belongs to a different task with a 404 — so a comment UID cannot be
// probed through an unrelated task's thread.
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
		writeCommentError(w, err, "reading comment failed")
		return comments.Comment{}, auth.User{}, false
	}
	if existing.TaskUID != taskUID(r) {
		writeError(w, http.StatusNotFound, "comment not found")
		return comments.Comment{}, auth.User{}, false
	}
	return existing, user, true
}

// canEditComment reports whether user may rewrite the comment: only its author,
// and only when the comment still has one (an authorless comment, left behind by
// a deleted account, is editable by nobody).
func canEditComment(c comments.Comment, user auth.User) bool {
	return c.AuthorUID != "" && c.AuthorUID == user.UID
}

// canDeleteComment reports whether user may remove the comment: its author, or
// an admin removing anyone's.
func canDeleteComment(c comments.Comment, user auth.User) bool {
	return (c.AuthorUID != "" && c.AuthorUID == user.UID) || user.Role.IsAdmin()
}

// commentsAvailable reports whether a comment store is configured, writing 503
// when it is not. A task itself still reads and edits without one.
func (a *API) commentsAvailable(w http.ResponseWriter) bool {
	if a.comments == nil {
		writeError(w, http.StatusServiceUnavailable, "comments are unavailable")
		return false
	}
	return true
}

// commentEntry builds the audit entry for a thread mutation. The target is the
// task the comment hangs off; the store adds the comment's own UID to the
// details.
func commentEntry(r *http.Request, actorUID, action, taskUID string) audit.Entry {
	return audit.FromRequest(r, actorUID).Entry(action, "photo_tasks", taskUID, nil)
}

// writeCommentError maps a comments store error to an HTTP response: 404 for a
// missing task or comment, 400 for an invalid body, otherwise 500 with failMsg.
func writeCommentError(w http.ResponseWriter, err error, failMsg string) {
	switch {
	case errors.Is(err, comments.ErrSubjectNotFound):
		writeError(w, http.StatusNotFound, "task not found")
	case errors.Is(err, comments.ErrNotFound):
		writeError(w, http.StatusNotFound, "comment not found")
	case errors.Is(err, comments.ErrEmptyBody):
		writeError(w, http.StatusBadRequest, "comment body is empty")
	case errors.Is(err, comments.ErrBodyTooLong):
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("comment body is longer than %d characters", comments.MaxBodyLen))
	default:
		writeError(w, http.StatusInternalServerError, failMsg)
	}
}
