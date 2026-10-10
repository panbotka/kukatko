package phototaskapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/phototask"
)

// commentRequest is the JSON body of the answer and edit endpoints: the
// plain-text body, trimmed and length-checked by the store.
type commentRequest struct {
	Body string `json:"body"`
}

// answerRequest is the JSON body of the answer endpoint: a comment and,
// optionally, the state the task moves to with it ("send and hand to the
// agent"). The state part is a writer's move exactly as it is on PATCH.
type answerRequest struct {
	Body  string  `json:"body"`
	State *string `json:"state,omitempty"`
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
//
// An optional state moves the task in the same transaction as the comment, both
// audited, so "here is my answer, your move" is one action that either lands
// whole or not at all. Moving a task is still a writer's act: a viewer who sends
// a state gets 403 and nothing is written, and an unknown state is a 400 before
// anything is.
func (a *API) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	if !a.commentsAvailable(w) {
		return
	}
	user, ok := currentUser(w, r)
	if !ok {
		return
	}
	var body answerRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	uid := taskUID(r)
	along, ok := a.stateMove(w, r, user, uid, body.State)
	if !ok {
		return
	}
	created, err := a.comments.CreateAlong(r.Context(), comments.TaskSubject(uid), user.UID, body.Body,
		commentEntry(r, user.UID, audit.ActionCommentCreate, uid), along)
	if err != nil {
		writeAnswerError(w, err)
		return
	}
	if along == nil {
		// Answering a question is what puts somebody on it — for a viewer it is
		// the only thing they can do to a task, so it has to be what counts. A
		// state move has already done it inside its own transaction.
		a.joinAuthor(r, uid, user.UID)
	}
	writeJSON(w, http.StatusCreated, created)
}

// stateMove turns an answer's optional state into the write that moves the task
// along with the comment, or nil when the answer moves nothing. It refuses —
// writing the response and reporting ok=false — a caller without write access
// (403) and a state outside phototask.States (400), both before anything is
// written.
func (a *API) stateMove(
	w http.ResponseWriter, r *http.Request, user auth.User, uid string, raw *string,
) (comments.Along, bool) {
	if raw == nil {
		return nil, true
	}
	if !user.Role.CanWrite() {
		writeError(w, http.StatusForbidden, "moving a task needs write access")
		return nil, false
	}
	state := phototask.State(*raw)
	if !state.Valid() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%v: %q", phototask.ErrInvalidState, *raw))
		return nil, false
	}
	entry := taskEntry(r, user.UID, audit.ActionTaskUpdate, uid)
	return func(ctx context.Context, tx pgx.Tx) error {
		return phototask.UpdateTx(ctx, tx, uid, phototask.Update{State: &state}, entry)
	}, true
}

// writeAnswerError maps a failed answer to an HTTP response: the task's own
// errors (a move the task's rules refuse) as on PATCH, everything else as any
// other comment failure.
func writeAnswerError(w http.ResponseWriter, err error) {
	const failMsg = "creating comment failed"
	switch {
	case errors.Is(err, phototask.ErrNotFound),
		errors.Is(err, phototask.ErrInvalidState),
		errors.Is(err, phototask.ErrClosedNeedsResolution):
		writeTaskError(w, err, failMsg)
	default:
		writeCommentError(w, err, failMsg)
	}
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
