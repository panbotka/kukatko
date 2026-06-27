package photoapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/trash"
)

// Purger is the subset of the trash service the HTTP layer needs: permanently
// deleting one archived photo and emptying the whole trash. When nil the trash
// mutation endpoints answer 503.
type Purger interface {
	// PurgePhoto permanently deletes the archived photo, returning
	// photos.ErrPhotoNotFound or trash.ErrNotArchived for the obvious cases.
	PurgePhoto(ctx context.Context, uid string) error
	// EmptyTrash permanently deletes every archived photo and reports the counts.
	EmptyTrash(ctx context.Context) (trash.Result, error)
}

// trashInfo is the JSON body of the trash-info endpoint, carrying the retention
// window so the UI can show each item's countdown to automatic purge.
type trashInfo struct {
	RetentionDays int `json:"retention_days"`
}

// handleTrashInfo returns the configured retention period in days so the trash
// UI can compute how long each archived photo has until it is auto-purged.
func (a *API) handleTrashInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, trashInfo{RetentionDays: a.retentionDays})
}

// confirmed reports whether the request carries the explicit confirm=true query
// parameter, the guard against an accidental permanent deletion.
func confirmed(r *http.Request) bool {
	return r.URL.Query().Get("confirm") == "true"
}

// handlePurge permanently deletes a single archived photo. It requires the
// explicit confirm=true query parameter (400 otherwise), answers 404 for a
// missing photo and 409 for a photo that is not archived, and 204 on success.
// Without a purge backend it answers 503.
func (a *API) handlePurge(w http.ResponseWriter, r *http.Request) {
	if a.purger == nil {
		writeError(w, http.StatusServiceUnavailable, "trash purge is not available")
		return
	}
	if !confirmed(r) {
		writeError(w, http.StatusBadRequest, "confirmation required (confirm=true)")
		return
	}
	uid := chi.URLParam(r, "uid")
	switch err := a.purger.PurgePhoto(r.Context(), uid); {
	case errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, trash.ErrNotArchived):
		writeError(w, http.StatusConflict, "photo is not archived")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "purging photo failed")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleEmptyTrash permanently deletes every archived photo. It requires the
// explicit confirm=true query parameter (400 otherwise) and returns the purged
// and failed counts. Without a purge backend it answers 503.
func (a *API) handleEmptyTrash(w http.ResponseWriter, r *http.Request) {
	if a.purger == nil {
		writeError(w, http.StatusServiceUnavailable, "trash purge is not available")
		return
	}
	if !confirmed(r) {
		writeError(w, http.StatusBadRequest, "confirmation required (confirm=true)")
		return
	}
	res, err := a.purger.EmptyTrash(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "emptying trash failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
