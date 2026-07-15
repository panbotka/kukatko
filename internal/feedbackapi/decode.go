package feedbackapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/panbotka/kukatko/internal/feedback"
)

// maxBodyBytes caps the request body size. A rejection body is a handful of short
// identifiers, so a tight 64 KiB limit guards against oversized payloads.
const maxBodyBytes = 64 << 10

// errNoPhotoUID is returned when a rejection body omits the photo UID.
var errNoPhotoUID = errors.New("photo_uid is required")

// errNoSubjectUID is returned when a face-rejection body omits the subject UID.
var errNoSubjectUID = errors.New("subject_uid is required")

// errNoLabelUID is returned when a label-rejection body omits the label UID.
var errNoLabelUID = errors.New("label_uid is required")

// errNegativeFaceIndex is returned when a face-rejection body carries a negative
// face index, which can never identify a real face slot.
var errNegativeFaceIndex = errors.New("face_index must not be negative")

// faceRejectionInput is the JSON body accepted by the face-rejection endpoints: the
// face (photo UID + face index) and the subject it is rejected for.
type faceRejectionInput struct {
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

// decodeFaceRejection decodes and validates a face-rejection body, requiring a
// non-empty photo UID and subject UID and a non-negative face index.
func decodeFaceRejection(r *http.Request) (faceRejectionInput, error) {
	var in faceRejectionInput
	if err := decodeJSON(r, &in); err != nil {
		return faceRejectionInput{}, err
	}
	in.PhotoUID = strings.TrimSpace(in.PhotoUID)
	in.SubjectUID = strings.TrimSpace(in.SubjectUID)
	switch {
	case in.PhotoUID == "":
		return faceRejectionInput{}, errNoPhotoUID
	case in.SubjectUID == "":
		return faceRejectionInput{}, errNoSubjectUID
	case in.FaceIndex < 0:
		return faceRejectionInput{}, errNegativeFaceIndex
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

// toKey converts the request input into a feedback.FaceRejectionKey.
func (in faceRejectionInput) toKey() feedback.FaceRejectionKey {
	return feedback.FaceRejectionKey{
		PhotoUID:   in.PhotoUID,
		FaceIndex:  in.FaceIndex,
		SubjectUID: in.SubjectUID,
	}
}

// toKey converts the request input into a feedback.LabelRejectionKey.
func (in labelRejectionInput) toKey() feedback.LabelRejectionKey {
	return feedback.LabelRejectionKey{PhotoUID: in.PhotoUID, LabelUID: in.LabelUID}
}
