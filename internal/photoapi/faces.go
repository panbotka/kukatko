package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// FaceService is the face-matching backend the faces endpoints delegate to. It is
// an interface so photoapi depends on the behaviour, not facematch's wiring;
// facematch.Service satisfies it.
type FaceService interface {
	// PhotoFaces returns the photo's faces with their marker assignment and ranked
	// subject suggestions (to name an unnamed face, or to reassign an assigned one).
	PhotoFaces(ctx context.Context, photoUID string) (facematch.FacesResponse, error)
	// PhotoPeople returns who is on the photo — the markers naming a subject and
	// the detections nobody has named yet — without the suggestion search, so a
	// detail response can carry it.
	PhotoPeople(ctx context.Context, photoUID string) ([]facematch.PersonOnPhoto, error)
	// Apply runs one assignment-state transition (create_marker / assign_person /
	// unassign_person), recording an audit entry stamped with meta in the same
	// transaction as the change.
	Apply(ctx context.Context, req facematch.AssignRequest, meta audit.Meta) (facematch.AssignResult, error)
}

// handleFaces returns the faces detected on a photo together with their marker
// assignment and per-face subject suggestions, for the detail UI. It answers 404
// for a missing photo and 503 when no face backend is wired.
func (a *API) handleFaces(w http.ResponseWriter, r *http.Request) {
	if a.faces == nil {
		writeError(w, http.StatusServiceUnavailable, "face matching not available")
		return
	}
	uid := chi.URLParam(r, "uid")
	resp, err := a.faces.PhotoFaces(r.Context(), uid)
	if err != nil {
		writeFaceError(w, err, "fetching faces failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolvePeople returns who is on the photo for the detail response, or nil to
// omit the block entirely.
//
// Unlike ocr_text it is behind an opt-in `people=true` query parameter, because
// assembling it costs a face list, a marker list and a subject lookup per named
// person — work the detail endpoint otherwise never pays, and which the web UI
// already pays separately in GET /photos/{uid}/faces when the face editor opens.
// The parameter exists for the caller that wants the whole photo in one request:
// `kukatko ctl photos get --people`.
//
// A malformed value is treated as "not asked" rather than failing the detail, and
// so is a face backend that is missing or in trouble: nobody loses the photo over
// the list of who is on it.
func (a *API) resolvePeople(r *http.Request, uid string) *[]facematch.PersonOnPhoto {
	if a.faces == nil {
		return nil
	}
	want, err := boolParam(r.URL.Query(), "people")
	if err != nil || want == nil || !*want {
		return nil
	}
	onPhoto, err := a.faces.PhotoPeople(r.Context(), uid)
	if err != nil {
		log.Printf("photoapi: resolving people of %s: %v", uid, err)
		return nil
	}
	if onPhoto == nil {
		onPhoto = []facematch.PersonOnPhoto{}
	}
	return &onPhoto
}

// handleFaceAssign applies a face-assignment transition (create marker, assign or
// unassign a subject) named in the JSON body, with the photo uid taken from the
// path. The acting user (from the auth context) and request are stamped onto the
// audit entry the face service writes in the mutation's transaction. Validation
// problems answer 400, a missing marker or subject 404, and a missing backend 503.
func (a *API) handleFaceAssign(w http.ResponseWriter, r *http.Request) {
	if a.faces == nil {
		writeError(w, http.StatusServiceUnavailable, "face matching not available")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var req facematch.AssignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.PhotoUID = chi.URLParam(r, "uid")

	result, err := a.faces.Apply(r.Context(), req, audit.FromRequest(r, user.UID))
	if err != nil {
		writeFaceError(w, err, "applying face assignment failed")
		return
	}
	a.enqueueSidecar(r.Context(), req.PhotoUID)
	writeJSON(w, http.StatusOK, result)
}

// writeFaceError maps a face-service error to an HTTP response: 400 for invalid
// requests, 404 for a missing photo/marker/subject, otherwise 500 with failMsg.
func writeFaceError(w http.ResponseWriter, err error, failMsg string) {
	switch {
	case errors.Is(err, facematch.ErrInvalidAction),
		errors.Is(err, facematch.ErrMissingBBox),
		errors.Is(err, facematch.ErrMissingMarker),
		errors.Is(err, facematch.ErrMissingSubject),
		errors.Is(err, people.ErrInvalidBounds),
		errors.Is(err, people.ErrInvalidType):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, photos.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, people.ErrMarkerNotFound):
		writeError(w, http.StatusNotFound, "marker not found")
	case errors.Is(err, people.ErrSubjectNotFound):
		writeError(w, http.StatusNotFound, "subject not found")
	default:
		writeError(w, http.StatusInternalServerError, failMsg)
	}
}
