package feedbackapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/feedback"
)

// maxBodyBytes caps the request body size. A feedback body is a handful of short
// identifiers, so a tight 64 KiB limit guards against oversized payloads.
const maxBodyBytes = 64 << 10

// errNoPhotoUID is returned when a feedback body omits the photo UID.
var errNoPhotoUID = errors.New("photo_uid is required")

// errNoSubjectUID is returned when a face-feedback body omits the subject UID.
var errNoSubjectUID = errors.New("subject_uid is required")

// errNoLabelUID is returned when a label-rejection body omits the label UID.
var errNoLabelUID = errors.New("label_uid is required")

// errNegativeFaceIndex is returned when a face-feedback body carries a negative
// face index, which can never identify a real face slot.
var errNegativeFaceIndex = errors.New("face_index must not be negative")

// errNoOtherUID is returned when a duplicate-dismissal body omits the second photo
// of the pair.
var errNoOtherUID = errors.New("other_uid is required")

// faceFeedbackInput is the JSON body accepted by the face-rejection and
// face-confirmation endpoints: the face (photo UID + face index) and the subject
// the opinion is about.
type faceFeedbackInput struct {
	PhotoUID   string `json:"photo_uid"`
	FaceIndex  int    `json:"face_index"`
	SubjectUID string `json:"subject_uid"`
}

// labelRejectionInput is the JSON body accepted by the label-rejection endpoints:
// the photo and the label it is rejected for.
type labelRejectionInput struct {
	PhotoUID string `json:"photo_uid"`
	LabelUID string `json:"label_uid"`
}

// duplicateDismissalInput is the JSON body accepted by the duplicate-dismissal
// endpoints: the two photos of the pair. The pair is unordered, so which uid goes
// in which field does not matter — the store normalises it.
type duplicateDismissalInput struct {
	PhotoUID string `json:"photo_uid"`
	OtherUID string `json:"other_uid"`
}

// decodeJSON reads dst from the JSON request body, rejecting unknown fields and an
// oversized body. The returned error message is safe to surface to the client.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid request body: " + err.Error())
	}
	return nil
}

// decodeFaceFeedback decodes and validates a face-rejection or face-confirmation
// body, requiring a non-empty photo UID and subject UID and a non-negative face
// index.
func decodeFaceFeedback(r *http.Request) (faceFeedbackInput, error) {
	var in faceFeedbackInput
	if err := decodeJSON(r, &in); err != nil {
		return faceFeedbackInput{}, err
	}
	in.PhotoUID = strings.TrimSpace(in.PhotoUID)
	in.SubjectUID = strings.TrimSpace(in.SubjectUID)
	switch {
	case in.PhotoUID == "":
		return faceFeedbackInput{}, errNoPhotoUID
	case in.SubjectUID == "":
		return faceFeedbackInput{}, errNoSubjectUID
	case in.FaceIndex < 0:
		return faceFeedbackInput{}, errNegativeFaceIndex
	}
	return in, nil
}

// decodeLabelRejection decodes and validates a label-rejection body, requiring a
// non-empty photo UID and label UID.
func decodeLabelRejection(r *http.Request) (labelRejectionInput, error) {
	var in labelRejectionInput
	if err := decodeJSON(r, &in); err != nil {
		return labelRejectionInput{}, err
	}
	in.PhotoUID = strings.TrimSpace(in.PhotoUID)
	in.LabelUID = strings.TrimSpace(in.LabelUID)
	switch {
	case in.PhotoUID == "":
		return labelRejectionInput{}, errNoPhotoUID
	case in.LabelUID == "":
		return labelRejectionInput{}, errNoLabelUID
	}
	return in, nil
}

// toRejectionKey converts the request input into a feedback.FaceRejectionKey.
func (in faceFeedbackInput) toRejectionKey() feedback.FaceRejectionKey {
	return feedback.FaceRejectionKey{
		PhotoUID:   in.PhotoUID,
		FaceIndex:  in.FaceIndex,
		SubjectUID: in.SubjectUID,
	}
}

// toConfirmationKey converts the request input into a feedback.FaceConfirmationKey.
func (in faceFeedbackInput) toConfirmationKey() feedback.FaceConfirmationKey {
	return feedback.FaceConfirmationKey{
		PhotoUID:   in.PhotoUID,
		FaceIndex:  in.FaceIndex,
		SubjectUID: in.SubjectUID,
	}
}

// toKey converts the request input into a feedback.LabelRejectionKey.
func (in labelRejectionInput) toKey() feedback.LabelRejectionKey {
	return feedback.LabelRejectionKey{PhotoUID: in.PhotoUID, LabelUID: in.LabelUID}
}

// decodeDuplicateDismissal decodes and validates a duplicate-dismissal body,
// requiring both photo UIDs. Whether the two name the same photo is left to the
// store (ErrSamePhoto), so the "impossible pair" rule lives in exactly one place.
func decodeDuplicateDismissal(r *http.Request) (duplicateDismissalInput, error) {
	var in duplicateDismissalInput
	if err := decodeJSON(r, &in); err != nil {
		return duplicateDismissalInput{}, err
	}
	in.PhotoUID = strings.TrimSpace(in.PhotoUID)
	in.OtherUID = strings.TrimSpace(in.OtherUID)
	switch {
	case in.PhotoUID == "":
		return duplicateDismissalInput{}, errNoPhotoUID
	case in.OtherUID == "":
		return duplicateDismissalInput{}, errNoOtherUID
	}
	return in, nil
}

// toKey converts the request input into a feedback.DuplicateDismissalKey.
func (in duplicateDismissalInput) toKey() feedback.DuplicateDismissalKey {
	return feedback.DuplicateDismissalKey{PhotoUID: in.PhotoUID, OtherUID: in.OtherUID}
}
