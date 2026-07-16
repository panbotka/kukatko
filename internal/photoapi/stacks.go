package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// Stacker performs the manual stacking operations behind the write-guarded stack
// endpoints. stacks.Service satisfies it; a nil Stacker (the feature disabled in
// config) makes those endpoints answer 503.
type Stacker interface {
	// StackSelection groups the given photos into one new stack and returns its
	// stack_uid.
	StackSelection(ctx context.Context, uids []string) (string, error)
	// SetPrimary makes the photo the primary of its stack and returns the stack_uid.
	SetPrimary(ctx context.Context, uid string) (string, error)
	// Unstack removes the photo from its stack and returns the stack_uid it left.
	Unstack(ctx context.Context, uid string) (string, error)
	// UnstackWhole dissolves the entire stack the photo belongs to.
	UnstackWhole(ctx context.Context, uid string) (string, error)
}

// stackMember is one file of a stack as presented in the detail response's
// variants strip: enough to render a thumbnail, its format and size, and to link
// to it. Every member — including the primary and the photo being viewed — is
// listed so the strip can mark the primary and let the user reach any variant.
type stackMember struct {
	UID         string `json:"uid"`
	FileName    string `json:"file_name"`
	MediaType   string `json:"media_type"`
	FileMime    string `json:"file_mime"`
	FileWidth   int    `json:"file_width"`
	FileHeight  int    `json:"file_height"`
	FileSize    int64  `json:"file_size"`
	IsPrimary   bool   `json:"is_primary"`
	ThumbURL    string `json:"thumb_url,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

// stackSelectionRequest is the body of POST /photos/stack.
type stackSelectionRequest struct {
	PhotoUIDs []string `json:"photo_uids"`
}

// annotateStacks fills each stacked primary's StackCount from one grouped query
// over the page's stack uids, leaving standalone photos at 0. Only primaries are
// ever in a listing, so the count is the number of files behind the visible tile.
func (a *API) annotateStacks(ctx context.Context, views []photoView) error {
	stackUIDs := make([]string, 0)
	for i := range views {
		if views[i].StackUID != nil {
			stackUIDs = append(stackUIDs, *views[i].StackUID)
		}
	}
	if len(stackUIDs) == 0 {
		return nil
	}
	counts, err := a.store.StackCounts(ctx, stackUIDs)
	if err != nil {
		return fmt.Errorf("photoapi: resolving stack counts: %w", err)
	}
	for i := range views {
		if views[i].StackUID != nil {
			views[i].StackCount = counts[*views[i].StackUID]
		}
	}
	return nil
}

// stackMembers returns the variants strip for photo: every member of its stack
// (the primary first), each stamped with its media URLs. It returns nil for an
// unstacked photo so the detail response omits the strip entirely.
func (a *API) stackMembers(ctx context.Context, photo photos.Photo) ([]stackMember, error) {
	if photo.StackUID == nil {
		return nil, nil
	}
	members, err := a.store.ListStackMembers(ctx, *photo.StackUID)
	if err != nil {
		return nil, fmt.Errorf("photoapi: listing stack members: %w", err)
	}
	out := make([]stackMember, len(members))
	for i := range members {
		m := members[i]
		a.media.DecorateOne(&m)
		out[i] = stackMember{
			UID: m.UID, FileName: m.FileName, MediaType: string(m.MediaType), FileMime: m.FileMime,
			FileWidth: m.FileWidth, FileHeight: m.FileHeight, FileSize: m.FileSize,
			IsPrimary: m.StackPrimary, ThumbURL: m.ThumbURL, DownloadURL: m.DownloadURL,
		}
	}
	return out, nil
}

// handleStackSelection groups the photos named in the JSON body into one new
// stack (manual stacking, for the cases the rules miss) and answers with the new
// stack's primary detail. It answers 503 when stacking is disabled, 400 for a
// selection of fewer than two photos, and 404 when one of them does not exist.
func (a *API) handleStackSelection(w http.ResponseWriter, r *http.Request) {
	if a.stacker == nil {
		writeError(w, http.StatusServiceUnavailable, "stacking not available")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req stackSelectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stackUID, err := a.stacker.StackSelection(r.Context(), req.PhotoUIDs)
	if err != nil {
		writeStackError(w, err, "stacking photos failed")
		return
	}
	// Every member's stack membership changed, so every member's sidecar is stale.
	a.enqueueSidecars(r.Context(), req.PhotoUIDs)
	a.writePrimaryDetail(w, r, user.UID, stackUID)
}

// handleStackSetPrimary makes the path photo the primary of its stack and answers
// with the refreshed detail. It answers 503 when stacking is disabled, 404 for a
// missing photo and 409 when the photo is not stacked.
func (a *API) handleStackSetPrimary(w http.ResponseWriter, r *http.Request) {
	a.runStackMutation(w, r, a.stacker.SetPrimary, "setting stack primary failed")
}

// handleUnstackMember removes the path photo from its stack and answers with the
// refreshed detail (the photo is now standalone).
func (a *API) handleUnstackMember(w http.ResponseWriter, r *http.Request) {
	a.runStackMutation(w, r, a.stacker.Unstack, "unstacking photo failed")
}

// handleUnstackAll dissolves the whole stack the path photo belongs to and
// answers with the refreshed detail.
func (a *API) handleUnstackAll(w http.ResponseWriter, r *http.Request) {
	a.runStackMutation(w, r, a.stacker.UnstackWhole, "unstacking stack failed")
}

// runStackMutation resolves the acting user, applies op to the path photo and
// answers with the photo's refreshed detail. It answers 503 when no stacker is
// wired, 401 when unauthenticated, and maps stack errors otherwise.
func (a *API) runStackMutation(
	w http.ResponseWriter, r *http.Request,
	op func(ctx context.Context, uid string) (string, error), failMsg string,
) {
	if a.stacker == nil {
		writeError(w, http.StatusServiceUnavailable, "stacking not available")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	uid := chi.URLParam(r, "uid")
	if _, err := op(r.Context(), uid); err != nil {
		writeStackError(w, err, failMsg)
		return
	}
	a.enqueueSidecar(r.Context(), uid)
	photo, err := a.store.GetByUID(r.Context(), uid)
	if err != nil {
		writePhotoError(w, err, failMsg)
		return
	}
	a.writeDetail(w, r, user.UID, photo)
}

// writePrimaryDetail answers with the full detail of the stack's primary member,
// used after manual stacking so the client can present the surviving tile.
func (a *API) writePrimaryDetail(w http.ResponseWriter, r *http.Request, userUID, stackUID string) {
	members, err := a.store.ListStackMembers(r.Context(), stackUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading stack failed")
		return
	}
	for _, m := range members {
		if m.StackPrimary {
			a.writeDetail(w, r, userUID, m)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "stack has no primary")
}

// writeStackError maps a stacking error to an HTTP response: 400 for a too-small
// selection, 404 for a missing photo, 409 for a photo that is not stacked, and
// otherwise 500 with failMsg.
func writeStackError(w http.ResponseWriter, err error, failMsg string) {
	switch {
	case errors.Is(err, photos.ErrStackTooSmall):
		writeError(w, http.StatusBadRequest, "a stack needs at least two photos")
	case errors.Is(err, photos.ErrPhotoNotStacked):
		writeError(w, http.StatusConflict, "photo is not stacked")
	case errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	default:
		writeError(w, http.StatusInternalServerError, failMsg)
	}
}
