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
	in, err := decodeFaceFeedback(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toRejectionKey()
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
	in, err := decodeFaceFeedback(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toRejectionKey()
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

// handleFaceConfirm records that the face in the request body really IS the given
// subject and answers 204. Confirming the same pair twice is a no-op. A malformed
// body or a missing identifier answers 400; a non-existent photo or subject
// answers 404.
func (a *API) handleFaceConfirm(w http.ResponseWriter, r *http.Request) {
	in, err := decodeFaceFeedback(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toConfirmationKey()
	entry := a.auditEntry(r, audit.ActionFaceConfirm, "subjects", key.SubjectUID, map[string]any{
		"photo_uid": key.PhotoUID, "face_index": key.FaceIndex,
	})
	if err := a.store.ConfirmFace(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleFaceUnconfirm takes back the face confirmation in the request body and
// answers 204. Un-confirming a pair that was never confirmed is a no-op. A
// malformed body or a missing identifier answers 400.
func (a *API) handleFaceUnconfirm(w http.ResponseWriter, r *http.Request) {
	in, err := decodeFaceFeedback(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toConfirmationKey()
	entry := a.auditEntry(r, audit.ActionFaceUnconfirm, "subjects", key.SubjectUID, map[string]any{
		"photo_uid": key.PhotoUID, "face_index": key.FaceIndex,
	})
	if err := a.store.UnconfirmFace(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDuplicateDismiss records that the two photos in the request body are NOT
// duplicates of each other and answers 204. The pair is unordered and the write is
// idempotent, so dismissing it twice — in either argument order — is a no-op. A
// malformed body or a missing identifier answers 400, as does a pair naming the
// same photo twice; a non-existent photo answers 404.
func (a *API) handleDuplicateDismiss(w http.ResponseWriter, r *http.Request) {
	in, err := decodeDuplicateDismissal(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toKey()
	entry := a.auditEntry(r, audit.ActionDuplicateDismiss, "photos", key.PhotoUID,
		map[string]any{"other_uid": key.OtherUID})
	if err := a.store.DismissDuplicate(r.Context(), key, entry); err != nil {
		status, msg := rejectionStatus(err)
		writeError(w, status, msg)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDuplicateUndismiss takes back the duplicate dismissal in the request body
// and answers 204, letting the pair be offered for review again. Un-dismissing a
// pair that was never dismissed is a no-op. A malformed body or a missing
// identifier answers 400.
func (a *API) handleDuplicateUndismiss(w http.ResponseWriter, r *http.Request) {
	in, err := decodeDuplicateDismissal(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key := in.toKey()
	entry := a.auditEntry(r, audit.ActionDuplicateUndismiss, "photos", key.PhotoUID,
		map[string]any{"other_uid": key.OtherUID})
	if err := a.store.UndismissDuplicate(r.Context(), key, entry); err != nil {
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
