package review

import (
	"fmt"
	"strconv"
	"strings"
)

// questionRef is the parsed identity of a question: everything the answer
// endpoint needs to apply a verdict without server-side queue state.
type questionRef struct {
	// Kind is KindFace or KindLabel.
	Kind Kind
	// PhotoUID is the photo under question.
	PhotoUID string
	// FaceIndex is the face's per-photo slot (face questions only).
	FaceIndex int
	// SubjectUID is the person under question (face questions only).
	SubjectUID string
	// LabelUID is the label under question (label questions only).
	LabelUID string
}

// faceQuestionID derives the stable id of a face question from its content, so
// the same candidate always yields the same id across rebuilds and restarts.
func faceQuestionID(photoUID string, faceIndex int, subjectUID string) string {
	return fmt.Sprintf("%s:%s:%d:%s", KindFace, photoUID, faceIndex, subjectUID)
}

// labelQuestionID derives the stable id of a label question from its content.
func labelQuestionID(photoUID, labelUID string) string {
	return fmt.Sprintf("%s:%s:%s", KindLabel, photoUID, labelUID)
}

// parseQuestionID inverts faceQuestionID/labelQuestionID. It returns
// ErrInvalidQuestion for anything it did not itself mint. The photo UID is
// re-joined from the middle segments so a ':' inside a UID cannot corrupt the
// trailing fields.
func parseQuestionID(id string) (questionRef, error) {
	parts := strings.Split(id, ":")
	switch Kind(parts[0]) {
	case KindFace:
		return parseFaceRef(parts)
	case KindLabel:
		return parseLabelRef(parts)
	default:
		return questionRef{}, ErrInvalidQuestion
	}
}

// parseFaceRef decodes the segments of a "face:<photo>:<index>:<subject>" id.
func parseFaceRef(parts []string) (questionRef, error) {
	if len(parts) < 4 {
		return questionRef{}, ErrInvalidQuestion
	}
	subjectUID := parts[len(parts)-1]
	index, err := strconv.Atoi(parts[len(parts)-2])
	photoUID := strings.Join(parts[1:len(parts)-2], ":")
	if err != nil || index < 0 || photoUID == "" || subjectUID == "" {
		return questionRef{}, ErrInvalidQuestion
	}
	return questionRef{Kind: KindFace, PhotoUID: photoUID, FaceIndex: index, SubjectUID: subjectUID}, nil
}

// parseLabelRef decodes the segments of a "label:<photo>:<label>" id.
func parseLabelRef(parts []string) (questionRef, error) {
	if len(parts) < 3 {
		return questionRef{}, ErrInvalidQuestion
	}
	labelUID := parts[len(parts)-1]
	photoUID := strings.Join(parts[1:len(parts)-1], ":")
	if photoUID == "" || labelUID == "" {
		return questionRef{}, ErrInvalidQuestion
	}
	return questionRef{Kind: KindLabel, PhotoUID: photoUID, LabelUID: labelUID}, nil
}
