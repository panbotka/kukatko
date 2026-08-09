package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
)

// maxCommentBody caps the comment request body. A body is at most
// comments.MaxBodyLen characters, so even four-byte runes escaped by a client
// stay far below this; the limit only bounds a malformed or hostile request.
const maxCommentBody = 16 << 10

// CommentStore is the subset of the comments repository the photo API needs to
// expose per-photo comment threads: reading a photo's thread, counting threads
// for a payload, and the three audited mutations. It is an interface so photoapi
// depends on the behaviour, not the store's construction; comments.Store
// satisfies it and a test fake can stand in. When nil the comment endpoints
// answer 503 and the detail reports a zero count.
type CommentStore interface {
	// List returns the live comments on photoUID, oldest first.
	List(ctx context.Context, photoUID string) ([]comments.Comment, error)
	// CountsAmong returns the number of live comments per photo UID; photos
	// without a comment are absent from the map.
	CountsAmong(ctx context.Context, photoUIDs []string) (map[string]int, error)
	// Get returns one live comment, or comments.ErrNotFound.
	Get(ctx context.Context, uid string) (comments.Comment, error)
	// Create stores a comment by authorUID on photoUID, auditing it in the same
	// transaction. It returns comments.ErrPhotoNotFound for a missing photo and
	// comments.ErrEmptyBody / comments.ErrBodyTooLong for an invalid body.
	Create(ctx context.Context, photoUID, authorUID, body string, entry audit.Entry) (comments.Comment, error)
	// Update rewrites a live comment's body, auditing it in the same transaction.
	Update(ctx context.Context, uid, body string, entry audit.Entry) (comments.Comment, error)
	// Delete soft-deletes a live comment, auditing it in the same transaction.
	Delete(ctx context.Context, uid string, entry audit.Entry) error
}

// commentRequest is the JSON body of the comment create and edit endpoints: the
// plain-text body, trimmed and length-checked by the store.
type commentRequest struct {
	Body string `json:"body"`
}

// commentListResponse is the JSON body of the thread endpoint. Comments is always
// an array (never null) so a client can render an empty thread without a guard.
type commentListResponse struct {
	Comments []comments.Comment `json:"comments"`
}

// handleListComments returns the photo's live comments, oldest first, each with
// its author's resolved name. Every authenticated role may read a thread. It
// answers 503 when no comments backend is wired.
func (a *API) handleListComments(w http.ResponseWriter, r *http.Request) {
	if a.comments == nil {
		writeError(w, http.StatusServiceUnavailable, "comments backend not configured")
		return
	}
	list, err := a.comments.List(r.Context(), chi.URLParam(r, "uid"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing comments failed")
		return
	}
	writeJSON(w, http.StatusOK, commentListResponse{Comments: list})
}

// handleCreateComment appends a comment to the photo's thread and returns it with
// 201.
//
// It is guarded by RequireAuth, not RequireWrite: writing a comment is the one
// mutation a viewer may make. Commenting is social participation, not curation of
// the library — a viewer still cannot retitle a photo, move it between albums or
// name a face — and locking the read-only half of a family out of the
// conversation would defeat the feature. See docs/API.md.
//
// A blank or over-long body is 400, a missing photo 404, and the route is rate
// limited per user so one client cannot flood a thread.
func (a *API) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	if a.comments == nil {
		writeError(w, http.StatusServiceUnavailable, "comments backend not configured")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	body, err := decodeComment(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	photoUID := chi.URLParam(r, "uid")
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionCommentCreate, "photos", photoUID, nil)
	created, err := a.comments.Create(r.Context(), photoUID, user.UID, body.Body, entry)
	if err != nil {
		writeCommentError(w, err, "creating comment failed")
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleUpdateComment rewrites the body of the caller's own comment and returns
// the edited record. Only the author may edit — an admin can remove a comment but
// never put words in someone else's mouth — so another user's comment is 403. A
// blank or over-long body is 400 and a missing comment 404.
func (a *API) handleUpdateComment(w http.ResponseWriter, r *http.Request) {
	existing, user, ok := a.resolveComment(w, r)
	if !ok {
		return
	}
	if !canEditComment(user, existing) {
		writeError(w, http.StatusForbidden, "only the author can edit a comment")
		return
	}
	body, err := decodeComment(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry := a.commentEntry(r, user.UID, audit.ActionCommentUpdate, existing)
	updated, err := a.comments.Update(r.Context(), existing.UID, body.Body, entry)
	if err != nil {
		writeCommentError(w, err, "updating comment failed")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// handleDeleteComment soft-deletes a comment and returns 204. The author may
// delete their own; an admin (and a maintainer, up the ladder) may delete
// anyone's, which is the moderation power the role already carries for the rest
// of the library. Any other user's comment is 403, a missing one 404.
func (a *API) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	existing, user, ok := a.resolveComment(w, r)
	if !ok {
		return
	}
	if !canDeleteComment(user, existing) {
		writeError(w, http.StatusForbidden, "only the author or an admin can delete a comment")
		return
	}
	entry := a.commentEntry(r, user.UID, audit.ActionCommentDelete, existing)
	if err := a.comments.Delete(r.Context(), existing.UID, entry); err != nil {
		writeCommentError(w, err, "deleting comment failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// canEditComment reports whether user may rewrite c. Only the author may: an
// admin's moderation power is to remove a comment, never to change what someone
// is recorded as having said. A comment whose author's account is gone
// (AuthorUID empty) belongs to nobody and is therefore editable by nobody.
func canEditComment(user auth.User, c comments.Comment) bool {
	return c.AuthorUID != "" && c.AuthorUID == user.UID
}

// canDeleteComment reports whether user may remove c: its author, or an admin
// (and a maintainer, up the ladder) moderating anyone's — the same governance
// power the role already carries over the rest of the library.
func canDeleteComment(user auth.User, c comments.Comment) bool {
	return canEditComment(user, c) || user.Role.IsAdmin()
}

// resolveComment performs the preamble the edit and delete handlers share:
// backend wired, caller authenticated, and the path comment read back so the
// authorization decision is made against the stored row rather than the request.
// A comment that belongs to a different photo than the path names is reported as
// missing — the pair is one address, and answering otherwise would let a caller
// probe which comment UIDs exist. It writes the error response and returns false
// when the request cannot proceed.
func (a *API) resolveComment(w http.ResponseWriter, r *http.Request) (comments.Comment, auth.User, bool) {
	if a.comments == nil {
		writeError(w, http.StatusServiceUnavailable, "comments backend not configured")
		return comments.Comment{}, auth.User{}, false
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return comments.Comment{}, auth.User{}, false
	}
	existing, err := a.comments.Get(r.Context(), chi.URLParam(r, "commentUID"))
	if err != nil {
		writeCommentError(w, err, "reading comment failed")
		return comments.Comment{}, auth.User{}, false
	}
	if existing.PhotoUID != chi.URLParam(r, "uid") {
		writeError(w, http.StatusNotFound, "comment not found")
		return comments.Comment{}, auth.User{}, false
	}
	return existing, user, true
}

// commentEntry builds the audit entry for a comment mutation. The target is the
// photo the comment hangs off (a comment is only ever read in the context of its
// picture); the store adds the comment's own UID to the details.
func (a *API) commentEntry(
	r *http.Request, actorUID, action string, existing comments.Comment,
) audit.Entry {
	return audit.FromRequest(r, actorUID).Entry(action, "photos", existing.PhotoUID, nil)
}

// commentCount returns how many live comments the photo has, for the detail
// payload's badge. It resolves through the bulk CountsAmong with a single UID, so
// the count can never become a per-item query when a listing wants the same badge.
// A nil comments backend reports zero rather than failing the detail.
func (a *API) commentCount(ctx context.Context, uid string) (int, error) {
	if a.comments == nil {
		return 0, nil
	}
	counts, err := a.comments.CountsAmong(ctx, []string{uid})
	if err != nil {
		return 0, fmt.Errorf("photoapi: counting comments: %w", err)
	}
	return counts[uid], nil
}

// decodeComment decodes the comment request body, rejecting unknown fields and a
// body larger than maxCommentBody. The text itself is trimmed and length-checked
// by the store, so create and edit accept exactly the same bodies.
func decodeComment(r *http.Request) (commentRequest, error) {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxCommentBody))
	dec.DisallowUnknownFields()
	var body commentRequest
	if err := dec.Decode(&body); err != nil {
		return commentRequest{}, errors.New("invalid request body: " + err.Error())
	}
	return body, nil
}

// writeCommentError maps a comments store error to an HTTP response: 404 for a
// missing photo or comment, 400 for an invalid body, otherwise 500 with failMsg.
func writeCommentError(w http.ResponseWriter, err error, failMsg string) {
	switch {
	case errors.Is(err, comments.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
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
