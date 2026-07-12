package photoapi

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/thumbjob"
)

// ThumbnailRegenerator force-rebuilds a photo's derived thumbnails and perceptual
// hashes on demand, overwriting any stale or missing cache, and returns the
// regenerated size names. It runs synchronously so the caller can report the
// outcome and surface a typed failure (a missing or undecodable original). It is
// satisfied by *thumbjob.Service.
type ThumbnailRegenerator interface {
	// ForceRegenerate rebuilds photoUID's thumbnails and pHash from the original,
	// returning the regenerated size names. It returns photos.ErrPhotoNotFound for
	// an unknown photo and wraps thumbjob.ErrRegenerateFailed when the original is
	// missing or cannot be decoded.
	ForceRegenerate(ctx context.Context, photoUID string) ([]string, error)
}

// AuditRecorder appends an audit entry outside any mutation transaction, used for
// actions (like thumbnail regeneration) that are not themselves a database
// mutation on the photo row, so there is no transaction to join. It is satisfied
// by *audit.Store.
type AuditRecorder interface {
	// Record appends entry on its own transaction.
	Record(ctx context.Context, entry audit.Entry) error
}

// regenerateThumbnailResponse is the JSON body returned on a successful
// regeneration: a fixed status plus the names of the sizes that were rebuilt, so
// the client has a clear, concrete result to show.
type regenerateThumbnailResponse struct {
	Status string   `json:"status"`
	Sizes  []string `json:"sizes"`
}

// handleRegenerateThumbnail rebuilds the photo's cached thumbnails and perceptual
// hashes from its original, overwriting stale cache entries, then records the
// action in the audit trail. It runs synchronously so it can report the outcome:
// 200 with the regenerated sizes on success, 404 for a missing photo, or 422 when
// the original is missing or cannot be decoded into an image. The original file
// is never touched — only derived data is rebuilt. Editors and admins only (the
// route is guarded by RequireWrite); when no regenerator is wired it answers 503.
func (a *API) handleRegenerateThumbnail(w http.ResponseWriter, r *http.Request) {
	if a.regenerator == nil {
		writeError(w, http.StatusServiceUnavailable, "thumbnail regeneration is not available")
		return
	}
	uid := chi.URLParam(r, "uid")
	sizes, err := a.regenerator.ForceRegenerate(r.Context(), uid)
	if err != nil {
		writeRegenerateError(w, err)
		return
	}
	a.recordThumbnailAudit(r, uid, sizes)
	writeJSON(w, http.StatusOK, regenerateThumbnailResponse{Status: "regenerated", Sizes: sizes})
}

// writeRegenerateError maps a regeneration error to an HTTP response: 404 for a
// missing photo, 422 when the original is missing or undecodable, otherwise 500.
func writeRegenerateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, thumbjob.ErrRegenerateFailed):
		writeError(w, http.StatusUnprocessableEntity,
			"cannot regenerate thumbnail: the original is missing or cannot be decoded")
	default:
		writeError(w, http.StatusInternalServerError, "regenerating thumbnail failed")
	}
}

// recordThumbnailAudit best-effort records the regeneration in the audit trail,
// attributing it to the acting user (resolved from the request context by the
// RequireWrite guard) and listing the regenerated sizes. A recording failure is
// logged but does not fail the request: the thumbnail was already rebuilt, so
// reporting an error to the client would misrepresent the outcome. When no
// recorder is wired it is a no-op.
func (a *API) recordThumbnailAudit(r *http.Request, uid string, sizes []string) {
	if a.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	entry := audit.FromRequest(r, user.UID).Entry(
		audit.ActionPhotoThumbnail, "photos", uid, map[string]any{"sizes": sizes},
	)
	if err := a.audit.Record(r.Context(), entry); err != nil {
		log.Printf("photoapi: recording thumbnail regeneration audit for %s: %v", uid, err)
	}
}
