package review

import (
	"fmt"
	"strconv"
	"strings"
)

// questionRef is the parsed identity of a question: everything the answer
// endpoint needs to apply a verdict without server-side queue state.
type questionRef struct {
	// Kind is one of the values in Kinds.
	Kind Kind
	// PhotoUID is the photo under question.
	PhotoUID string
	// FaceIndex is the face's per-photo slot (face and outlier questions only).
	FaceIndex int
	// SubjectUID is the person under question (face and outlier questions only).
	SubjectUID string
	// LabelUID is the label under question (label questions only).
	LabelUID string
	// OtherUID is the second photo of the pair (duplicate questions only).
	OtherUID string
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

// placeQuestionID derives the stable id of a place question. The photo is the
// whole identity: a photo has at most one location, so there is nothing else to
// key on, and re-estimating it later is still the same question.
func placeQuestionID(photoUID string) string {
	return fmt.Sprintf("%s:%s", KindPlace, photoUID)
}

// duplicateQuestionID derives the stable id of a duplicate question from the
// pair. The uids are ordered smaller-first, the same normalisation the feedback
// store applies, so the pair keeps one id however the group happens to name it —
// otherwise a session could ask the same pair twice under two ids.
func duplicateQuestionID(photoUID, otherUID string) string {
	if otherUID < photoUID {
		photoUID, otherUID = otherUID, photoUID
	}
	return fmt.Sprintf("%s:%s:%s", KindDuplicate, photoUID, otherUID)
}

// outlierQuestionID derives the stable id of an outlier question. It is the same
// shape as a face question's — the face plus the subject — because it identifies
// the same thing; only the direction of the doubt differs.
func outlierQuestionID(photoUID string, faceIndex int, subjectUID string) string {
	return fmt.Sprintf("%s:%s:%d:%s", KindOutlier, photoUID, faceIndex, subjectUID)
}

// parseQuestionID inverts the *QuestionID builders above. It returns
// ErrInvalidQuestion for anything it did not itself mint. The photo UID is
// re-joined from the middle segments so a ':' inside a UID cannot corrupt the
// trailing fields.
func parseQuestionID(id string) (questionRef, error) {
	parts := strings.Split(id, ":")
	switch kind := Kind(parts[0]); kind {
	case KindFace, KindOutlier:
		return parseFaceRef(kind, parts)
	case KindLabel:
		return parseLabelRef(parts)
	case KindPlace:
		return parsePlaceRef(parts)
	case KindDuplicate:
		return parseDuplicateRef(parts)
	default:
		return questionRef{}, ErrInvalidQuestion
	}
}

// parseFaceRef decodes the segments of a "<kind>:<photo>:<index>:<subject>" id,
// which face and outlier questions share.
func parseFaceRef(kind Kind, parts []string) (questionRef, error) {
	if len(parts) < 4 {
		return questionRef{}, ErrInvalidQuestion
	}
	subjectUID := parts[len(parts)-1]
	index, err := strconv.Atoi(parts[len(parts)-2])
	photoUID := strings.Join(parts[1:len(parts)-2], ":")
	if err != nil || index < 0 || photoUID == "" || subjectUID == "" {
		return questionRef{}, ErrInvalidQuestion
	}
	return questionRef{Kind: kind, PhotoUID: photoUID, FaceIndex: index, SubjectUID: subjectUID}, nil
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

// parsePlaceRef decodes the segments of a "place:<photo>" id.
func parsePlaceRef(parts []string) (questionRef, error) {
	if len(parts) < 2 {
		return questionRef{}, ErrInvalidQuestion
	}
	photoUID := strings.Join(parts[1:], ":")
	if photoUID == "" {
		return questionRef{}, ErrInvalidQuestion
	}
	return questionRef{Kind: KindPlace, PhotoUID: photoUID}, nil
}

// parseDuplicateRef decodes the segments of a "duplicate:<photo>:<other>" id.
// The two uids must differ: a photo is never a duplicate of itself, and the
// feedback store would reject the pair anyway, so it is a malformed id rather
// than a failed write.
func parseDuplicateRef(parts []string) (questionRef, error) {
	if len(parts) < 3 {
		return questionRef{}, ErrInvalidQuestion
	}
	otherUID := parts[len(parts)-1]
	photoUID := strings.Join(parts[1:len(parts)-1], ":")
	if photoUID == "" || otherUID == "" || photoUID == otherUID {
		return questionRef{}, ErrInvalidQuestion
	}
	return questionRef{Kind: KindDuplicate, PhotoUID: photoUID, OtherUID: otherUID}, nil
}
