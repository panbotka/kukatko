package photoapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// maxEditBody caps the PUT /photos/{uid}/edit request body so a malformed client
// cannot stream an unbounded JSON document into memory.
const maxEditBody = 1 << 16 // 64 KiB

// editBody is the non-destructive edit accepted by PUT /photos/{uid}/edit. Crop
// coordinates are normalised to 0..1 and all-or-nothing; rotation is one of 0,
// 90, 180, 270; brightness and contrast are neutral at 0 and meaningful within
// [-1, 1].
type editBody struct {
	CropX      *float64 `json:"crop_x"`
	CropY      *float64 `json:"crop_y"`
	CropW      *float64 `json:"crop_w"`
	CropH      *float64 `json:"crop_h"`
	Rotation   int      `json:"rotation"`
	Brightness float64  `json:"brightness"`
	Contrast   float64  `json:"contrast"`
}

// handleGetEdit returns the stored non-destructive edit for the photo named in
// the path, or a neutral (zero-value) edit when none has been saved yet, so the
// editor UI always has a value to seed its controls. A missing photo is 404.
func (a *API) handleGetEdit(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")
	if _, err := a.store.GetByUID(r.Context(), uid); err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	edit, err := a.store.GetEdit(r.Context(), uid)
	if err != nil {
		if errors.Is(err, photos.ErrEditNotFound) {
			writeJSON(w, http.StatusOK, photos.Edit{PhotoUID: uid})
			return
		}
		writeError(w, http.StatusInternalServerError, "fetching edit failed")
		return
	}
	writeJSON(w, http.StatusOK, edit)
}

// handlePutEdit validates and stores the non-destructive edit for the photo named
// in the path, returning the persisted edit (with its updated_at). A malformed or
// out-of-range body is 400 and a missing photo 404. Editor/admin only.
//
// A stored edit has three consequences beyond the row, all after the write and all
// best-effort: it is audited (photo.edit), the metadata sidecar is rescheduled, and
// a forced thumbnail rebuild is enqueued so the derived thumbnails — and with them
// the library grid, the search results and the map popovers — show the photo the
// way the user turned it. Saving the neutral edit is deliberately no special case:
// a reset has to leave correct thumbnails behind too.
func (a *API) handlePutEdit(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")

	body, err := decodeEdit(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateEdit(body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := a.store.GetByUID(r.Context(), uid); err != nil {
		writePhotoError(w, err, "fetching photo failed")
		return
	}

	edit := photos.Edit{
		PhotoUID:   uid,
		CropX:      body.CropX,
		CropY:      body.CropY,
		CropW:      body.CropW,
		CropH:      body.CropH,
		Rotation:   body.Rotation,
		Brightness: body.Brightness,
		Contrast:   body.Contrast,
	}
	if err := a.store.SetEdit(r.Context(), edit); err != nil {
		writeError(w, http.StatusInternalServerError, "saving edit failed")
		return
	}
	a.recordEditAudit(r, edit)
	a.enqueueSidecar(r.Context(), uid)
	// The cache is keyed by the original's file hash and cannot notice that the
	// photo now renders differently, so the rebuild has to be asked for — for the
	// neutral edit as much as for a rotation, since resetting an edit leaves the
	// previous rendering cached exactly the same way.
	a.enqueueThumbnailRebuild(r.Context(), uid)

	stored, err := a.store.GetEdit(r.Context(), uid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "reading saved edit failed")
		return
	}
	writeJSON(w, http.StatusOK, stored)
}

// recordEditAudit best-effort records a saved edit in the audit trail,
// attributing it to the acting user (resolved from the request context by the
// RequireWrite guard) and carrying the whole edit as details, so the trail says
// what the photo now renders as and not merely that something changed.
//
// It records after the write rather than inside it because Store.SetEdit exposes
// no transaction to join — the same position handleRegenerateThumbnail records
// from. A recording failure is logged and swallowed: the edit is saved, and
// answering with an error would tell the user their rotation was lost when it was
// not. When no recorder is wired it is a no-op.
func (a *API) recordEditAudit(r *http.Request, edit photos.Edit) {
	if a.audit == nil {
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	details := map[string]any{
		"rotation":   edit.Rotation,
		"brightness": edit.Brightness,
		"contrast":   edit.Contrast,
		"crop":       edit.CropX != nil,
	}
	entry := audit.FromRequest(r, user.UID).Entry(
		audit.ActionPhotoEdit, "photos", edit.PhotoUID, details,
	)
	if err := a.audit.Record(r.Context(), entry); err != nil {
		log.Printf("photoapi: recording edit audit for %s: %v", edit.PhotoUID, err)
	}
}

// decodeEdit reads and decodes the edit request body, rejecting an oversized
// body, unknown fields or malformed JSON.
func decodeEdit(r *http.Request) (editBody, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxEditBody+1))
	if err != nil {
		return editBody{}, errors.New("reading request body failed")
	}
	if len(raw) > maxEditBody {
		return editBody{}, errors.New("request body too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var body editBody
	if err := dec.Decode(&body); err != nil {
		return editBody{}, errors.New("malformed JSON body")
	}
	return body, nil
}

// validateEdit checks the rotation allow-list, the brightness/contrast range and
// the all-or-nothing normalised crop rectangle, returning a descriptive error on
// the first violation.
func validateEdit(body editBody) error {
	if body.Rotation != 0 && body.Rotation != 90 && body.Rotation != 180 && body.Rotation != 270 {
		return errors.New("rotation must be one of 0, 90, 180, 270")
	}
	if body.Brightness < -1 || body.Brightness > 1 {
		return errors.New("brightness must be between -1 and 1")
	}
	if body.Contrast < -1 || body.Contrast > 1 {
		return errors.New("contrast must be between -1 and 1")
	}
	return validateCrop(body)
}

// validateCrop enforces the all-or-nothing crop rule and then the normalised
// bounds of the rectangle it describes.
func validateCrop(body editBody) error {
	set := 0
	for _, v := range []*float64{body.CropX, body.CropY, body.CropW, body.CropH} {
		if v != nil {
			set++
		}
	}
	switch set {
	case 0:
		return nil
	case 4:
		return checkCropBounds(*body.CropX, *body.CropY, *body.CropW, *body.CropH)
	default:
		return errors.New("crop requires all of crop_x, crop_y, crop_w, crop_h or none")
	}
}

// checkCropBounds verifies the normalised crop rectangle has a non-negative
// origin, a positive size, and lies within the image (x+w <= 1, y+h <= 1).
func checkCropBounds(x, y, cropW, cropH float64) error {
	if x < 0 || y < 0 || cropW <= 0 || cropH <= 0 {
		return errors.New("crop coordinates must be within 0..1 with a positive size")
	}
	if x+cropW > 1 || y+cropH > 1 {
		return errors.New("crop rectangle must lie within the image")
	}
	return nil
}
