// Package feedback is Kukátko's store for persisted review feedback: a user's
// durable "no" to a face↔subject guess or a photo↔label guess (a rejection), and
// the durable "yes, this really is them" to a face↔subject assignment (a
// confirmation). It exists to close photo-sorter's gap where an opinion was never
// persisted, so the very same wrong face was offered again on the next search
// forever and the review work never shrank (see docs/ARCHITECTURE.md and
// migrations 0031 and 0032).
//
// Feedback records an OPINION; it never mutates the data it is about. Rejecting
// a face does not unassign a marker or delete the face; rejecting a label does not
// detach the label; confirming a face assigns nothing. The review features read
// these opinions to exclude what a user has already settled (the unassigned-face
// search takes a face rejection set as an exclusion filter; label expansion takes
// a label rejection set; outlier review takes a face confirmation set), and the
// negative-exemplar rule in internal/vectors turns a rejection into a
// nearest-neighbour margin test rather than just hiding one row.
//
// Every write is audited in the same transaction as the mutation, matching the
// project's durable-audit convention. Recording feedback is idempotent: the
// natural keys are UNIQUE and inserts use ON CONFLICT DO NOTHING, so rejecting or
// confirming twice is a no-op; taking back a pair that was never recorded is
// likewise a no-op.
package feedback

import (
	"errors"
	"time"
)

// ErrEmptyKey is returned by the store when a rejection key is missing a required
// identifier (an empty photo UID, subject UID or label UID). It signals a caller
// mistake, not a transient failure, so the HTTP layer maps it to 400.
var ErrEmptyKey = errors.New("feedback: rejection key is incomplete")

// ErrTargetNotFound is returned when recording a rejection references a photo,
// subject or label that does not exist (a foreign-key violation). The HTTP layer
// maps it to 404.
var ErrTargetNotFound = errors.New("feedback: referenced photo, subject or label not found")

// FaceRejectionKey identifies a single "this face is NOT this person" rejection:
// the face by the identity Kukátko already uses for a face (photo UID + per-photo
// face index, see internal/facematch and the faces table) plus the subject the
// face was rejected for.
type FaceRejectionKey struct {
	// PhotoUID is the owning photo's uid.
	PhotoUID string
	// FaceIndex is the per-photo face slot (faces.face_index).
	FaceIndex int
	// SubjectUID is the subject the face was rejected for.
	SubjectUID string
}

// valid reports whether the key has every identifier a face rejection needs. A
// zero FaceIndex is legitimate (it is the first face slot), so only the string
// identifiers are checked.
func (k FaceRejectionKey) valid() bool {
	return k.PhotoUID != "" && k.SubjectUID != ""
}

// FaceConfirmationKey identifies a single "this face really IS this person"
// confirmation: the face by the identity Kukátko already uses for a face (photo
// UID + per-photo face index) plus the subject the assignment was confirmed
// for. It is the positive mirror of FaceRejectionKey; outlier review persists
// it so a face a user vouched for is not offered as an outlier again.
type FaceConfirmationKey struct {
	// PhotoUID is the owning photo's uid.
	PhotoUID string
	// FaceIndex is the per-photo face slot (faces.face_index).
	FaceIndex int
	// SubjectUID is the subject the face was confirmed for.
	SubjectUID string
}

// valid reports whether the key has every identifier a face confirmation needs.
// A zero FaceIndex is legitimate (it is the first face slot), so only the
// string identifiers are checked.
func (k FaceConfirmationKey) valid() bool {
	return k.PhotoUID != "" && k.SubjectUID != ""
}

// LabelRejectionKey identifies a single "this photo should NOT have this label"
// rejection by photo UID and label UID.
type LabelRejectionKey struct {
	// PhotoUID is the photo the label was rejected for.
	PhotoUID string
	// LabelUID is the rejected label.
	LabelUID string
}

// valid reports whether the key has both identifiers a label rejection needs.
func (k LabelRejectionKey) valid() bool {
	return k.PhotoUID != "" && k.LabelUID != ""
}

// FaceRef is a face identified by (photo UID, face index) with no subject — the
// shape the unassigned-face search needs as an exclusion filter. It is what the
// bulk "every face rejected for subject X" lookup returns, so a search path can
// exclude those faces in SQL without an N+1.
type FaceRef struct {
	// PhotoUID is the owning photo's uid.
	PhotoUID string
	// FaceIndex is the per-photo face slot (faces.face_index).
	FaceIndex int
}

// FaceRejection is a stored face rejection row as read back from the table.
type FaceRejection struct {
	// PhotoUID and FaceIndex identify the rejected face.
	PhotoUID  string `json:"photo_uid"`
	FaceIndex int    `json:"face_index"`
	// SubjectUID is the subject the face was rejected for.
	SubjectUID string `json:"subject_uid"`
	// RejectedBy is the UID of the user who rejected it, empty when unknown (a
	// system action or a since-deleted user).
	RejectedBy string `json:"rejected_by,omitempty"`
	// RejectedAt is when the rejection was recorded.
	RejectedAt time.Time `json:"rejected_at"`
}

// LabelRejection is a stored label rejection row as read back from the table.
type LabelRejection struct {
	// PhotoUID and LabelUID identify the rejected photo↔label pair.
	PhotoUID string `json:"photo_uid"`
	LabelUID string `json:"label_uid"`
	// RejectedBy is the UID of the user who rejected it, empty when unknown.
	RejectedBy string `json:"rejected_by,omitempty"`
	// RejectedAt is when the rejection was recorded.
	RejectedAt time.Time `json:"rejected_at"`
}
