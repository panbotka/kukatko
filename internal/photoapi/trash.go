package photoapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/trash"
)

// Purger is the subset of the trash service the HTTP layer needs: permanently
// deleting one archived photo and emptying the whole trash. Each mutation carries
// the acting user (audit.Meta) so the permanent deletion is attributed in the
// audit trail. When nil the trash mutation endpoints answer 503.
type Purger interface {
	// PurgePhoto permanently deletes the archived photo, returning
	// photos.ErrPhotoNotFound or trash.ErrNotArchived for the obvious cases.
	PurgePhoto(ctx context.Context, uid string, meta audit.Meta) error
	// EmptyTrash permanently deletes every archived photo and reports the counts.
	EmptyTrash(ctx context.Context, meta audit.Meta) (trash.Result, error)
	// PurgeOlderThan permanently deletes every archived photo older than the given
	// number of days (days == 0 purges the whole trash) and reports the counts,
	// attributing each purge to the calling admin.
	PurgeOlderThan(ctx context.Context, days int, meta audit.Meta) (trash.Result, error)
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

// purgeAuditMeta builds the audit envelope for a trash purge from r, attributing
// it to the authenticated caller. The trash mutation endpoints are gated by
// requireWrite, so a principal is always present in production; if one is somehow
// absent the actor is recorded empty rather than failing the permanent deletion.
func purgeAuditMeta(r *http.Request) audit.Meta {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
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
	switch err := a.purger.PurgePhoto(r.Context(), uid, purgeAuditMeta(r)); {
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
	res, err := a.purger.EmptyTrash(r.Context(), purgeAuditMeta(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "emptying trash failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// parseDays parses the required days query parameter for the age-bounded purge:
// it must be a non-negative integer. An empty, non-numeric or negative value is
// rejected (the caller maps it to 400). days == 0 is valid and means "every
// archived photo" (equivalent to emptying the trash).
func parseDays(raw string) (int, error) {
	if raw == "" {
		return 0, errors.New("days is required")
	}
	days, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("days must be an integer, got %q", raw)
	}
	if days < 0 {
		return 0, fmt.Errorf("days must be >= 0, got %d", days)
	}
	return days, nil
}

// handlePurgeOlder permanently deletes every archived photo whose archived_at is
// older than the given number of days. It requires the explicit confirm=true
// query parameter (400 otherwise) and a non-negative integer days parameter (400
// otherwise); days=0 purges the whole trash. It returns the purged and failed
// counts. Without a purge backend it answers 503, mirroring the other purge
// endpoints. The purge is attributed in the audit trail to the calling admin.
func (a *API) handlePurgeOlder(w http.ResponseWriter, r *http.Request) {
	if a.purger == nil {
		writeError(w, http.StatusServiceUnavailable, "trash purge is not available")
		return
	}
	if !confirmed(r) {
		writeError(w, http.StatusBadRequest, "confirmation required (confirm=true)")
		return
	}
	days, err := parseDays(r.URL.Query().Get("days"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := a.purger.PurgeOlderThan(r.Context(), days, purgeAuditMeta(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "purging trash failed")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
