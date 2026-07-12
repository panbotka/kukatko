package photoapi

import (
	"context"
	"encoding/json"
	"errors"
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
	// PhotoFaces returns the photo's faces with their marker assignment and, for
	// unnamed faces, ranked subject suggestions.
	PhotoFaces(ctx context.Context, photoUID string) (facematch.FacesResponse, error)
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
