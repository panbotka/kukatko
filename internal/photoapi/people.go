package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/people"
)

// maxAttachBody caps the attach request body. It carries one uid, so anything
// larger is malformed or hostile rather than a big request.
const maxAttachBody = 4 << 10

// PeopleAttacher is the subset of the people repository the photo API needs to
// record who is in a picture when no face was detected: the two audited
// mutations and the read that both answer with. It is an interface so photoapi
// depends on the behaviour, not the store's construction; people.Store satisfies
// it and a test fake can stand in. When nil the two endpoints answer 503 and the
// detail reports an empty list.
type PeopleAttacher interface {
	// ListPhotoSubjects returns the people attached to the photo by hand.
	ListPhotoSubjects(ctx context.Context, photoUID string) ([]people.PhotoSubject, error)
	// AttachSubjectToPhoto links a subject to the photo, auditing it in the same
	// transaction, and returns the resulting list. It is idempotent.
	AttachSubjectToPhoto(
		ctx context.Context, photoUID, subjectUID string, entry audit.Entry,
	) ([]people.PhotoSubject, error)
	// DetachSubjectFromPhoto removes the link, auditing it in the same
	// transaction, and returns the remaining list.
	DetachSubjectFromPhoto(
		ctx context.Context, photoUID, subjectUID string, entry audit.Entry,
	) ([]people.PhotoSubject, error)
}

// attachRequest is the JSON body of the attach endpoint: the subject to record as
// present in the picture.
type attachRequest struct {
	SubjectUID string `json:"subject_uid"`
}

// peopleResponse is the body both mutations answer with — and the same shape the
// detail response embeds — so a client renders the new state from the reply
// instead of re-reading the photo.
type peopleResponse struct {
	People []people.PhotoSubject `json:"people"`
}

// handleAttachPerson records that the subject named in the body is in the photo,
// with no bounding box and no detected face, and answers the photo's resulting
// hand-attached people.
//
// It is the only way to say who is in a picture the detector cannot see: anybody
// in a video at all (detection does not run on footage), and the profiles, backs
// of heads and crowd faces it misses on a still.
//
// Attaching somebody already attached is a success, not a conflict: the caller
// asked for a state that already holds and gets the same body as the first
// attach. A malformed body answers 400, an unknown photo or subject 404, and a
// missing backend 503.
func (a *API) handleAttachPerson(w http.ResponseWriter, r *http.Request) {
	if a.attacher == nil {
		writeError(w, http.StatusServiceUnavailable, "attaching people not available")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	subjectUID, err := decodeAttach(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	photoUID := chi.URLParam(r, "uid")
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionPersonAttach, "markers", "", map[string]any{
		"photo_uid":   photoUID,
		"subject_uid": subjectUID,
	})
	attached, err := a.attacher.AttachSubjectToPhoto(r.Context(), photoUID, subjectUID, entry)
	if err != nil {
		writeAttachError(w, err, "attaching the person failed")
		return
	}
	a.enqueueSidecar(r.Context(), photoUID)
	writeJSON(w, http.StatusOK, peopleResponse{People: attached})
}

// handleDetachPerson removes a hand-attached person from the photo and answers
// the remaining ones. Detaching somebody who is not attached is a success for the
// same reason attaching twice is: the requested state already holds. An unknown
// photo or subject answers 404 and a missing backend 503.
func (a *API) handleDetachPerson(w http.ResponseWriter, r *http.Request) {
	if a.attacher == nil {
		writeError(w, http.StatusServiceUnavailable, "attaching people not available")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	photoUID := chi.URLParam(r, "uid")
	subjectUID := chi.URLParam(r, "subjectUID")
	entry := audit.FromRequest(r, user.UID).Entry(audit.ActionPersonDetach, "subjects", subjectUID,
		map[string]any{"photo_uid": photoUID})
	remaining, err := a.attacher.DetachSubjectFromPhoto(r.Context(), photoUID, subjectUID, entry)
	if err != nil {
		writeAttachError(w, err, "detaching the person failed")
		return
	}
	a.enqueueSidecar(r.Context(), photoUID)
	writeJSON(w, http.StatusOK, peopleResponse{People: remaining})
}

// decodeAttach reads the attach body and returns the trimmed subject uid, or an
// error describing what was wrong with the request.
func decodeAttach(r *http.Request) (string, error) {
	var in attachRequest
	if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxAttachBody)).Decode(&in); err != nil {
		return "", errors.New("invalid request body")
	}
	uid := strings.TrimSpace(in.SubjectUID)
	if uid == "" {
		return "", errors.New("subject_uid is required")
	}
	return uid, nil
}

// writeAttachError maps an attach/detach failure to an HTTP response: 404 for a
// photo or subject that does not exist, otherwise 500 with failMsg.
func writeAttachError(w http.ResponseWriter, err error, failMsg string) {
	switch {
	case errors.Is(err, people.ErrPhotoNotFound):
		writeError(w, http.StatusNotFound, "photo not found")
	case errors.Is(err, people.ErrSubjectNotFound):
		writeError(w, http.StatusNotFound, "subject not found")
	default:
		writeError(w, http.StatusInternalServerError, failMsg)
	}
}

// resolveAttachedPeople returns the photo's hand-attached people for the detail
// response, always as a non-nil slice.
//
// Unlike the opt-in faces roll-call it is unconditional, because it is one
// indexed read joined to subjects — no face list, no IoU pairing, no vector
// search — and because it is the only place those links are visible at all: a
// hand-attached person has no box, so no face endpoint will ever mention them.
// A read failure is logged and degrades to an empty list: nobody loses the photo
// over the list of who is on it.
func (a *API) resolveAttachedPeople(ctx context.Context, photoUID string) []people.PhotoSubject {
	if a.attacher == nil {
		return []people.PhotoSubject{}
	}
	attached, err := a.attacher.ListPhotoSubjects(ctx, photoUID)
	if err != nil {
		log.Printf("photoapi: listing attached people of %s: %v", photoUID, err)
		return []people.PhotoSubject{}
	}
	return attached
}
