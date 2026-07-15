package feedbackapi

import (
	"net/http"

	"github.com/panbotka/kukatko/internal/audit"
)

// handleFaceReject records that the face in the request body is NOT the given
// subject and answers 204. Rejecting the same pair twice is a no-op. A malformed
// body or a missing identifier answers 400; a non-existent photo or subject
// answers 404.
func (a *API) handleFaceReject(w http.ResponseWriter, r *http.Request) {
	in, err := decodeFaceRejection(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toKey()
	entry := a.auditEntry(r, audit.ActionFaceReject, "subjects", key.SubjectUID, map[string]any{
		"photo_uid": key.PhotoUID, "face_index": key.FaceIndex,
	})
	if err := a.store.RejectFace(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFaceUnreject takes back the face rejection in the request body and answers
// 204. Un-rejecting a pair that was never rejected is a no-op. A malformed body or a
// missing identifier answers 400.
func (a *API) handleFaceUnreject(w http.ResponseWriter, r *http.Request) {
	in, err := decodeFaceRejection(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toKey()
	entry := a.auditEntry(r, audit.ActionFaceUnreject, "subjects", key.SubjectUID, map[string]any{
		"photo_uid": key.PhotoUID, "face_index": key.FaceIndex,
	})
	if err := a.store.UnrejectFace(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLabelReject records that the photo in the request body should NOT have the
// given label and answers 204. Rejecting the same pair twice is a no-op. A malformed
// body or a missing identifier answers 400; a non-existent photo or label answers
// 404.
func (a *API) handleLabelReject(w http.ResponseWriter, r *http.Request) {
	in, err := decodeLabelRejection(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toKey()
	entry := a.auditEntry(r, audit.ActionLabelReject, "labels", key.LabelUID,
		map[string]any{"photo_uid": key.PhotoUID})
	if err := a.store.RejectLabel(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLabelUnreject takes back the label rejection in the request body and answers
// 204. Un-rejecting a pair that was never rejected is a no-op. A malformed body or a
// missing identifier answers 400.
func (a *API) handleLabelUnreject(w http.ResponseWriter, r *http.Request) {
	in, err := decodeLabelRejection(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toKey()
	entry := a.auditEntry(r, audit.ActionLabelUnreject, "labels", key.LabelUID,
		map[string]any{"photo_uid": key.PhotoUID})
	if err := a.store.UnrejectLabel(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
