package facematch

import (
	"errors"

	"github.com/panbotka/kukatko/internal/people"
)

// Assignment-state actions. They mirror photo-sorter's face apply state machine.
const (
	// ActionCreateMarker creates a new face marker on the photo and assigns a
	// subject to it (used when no existing marker overlaps the face).
	ActionCreateMarker = "create_marker"
	// ActionAssignPerson assigns a subject to an existing, unassigned marker.
	ActionAssignPerson = "assign_person"
	// ActionUnassignPerson clears the subject from a marker.
	ActionUnassignPerson = "unassign_person"
	// ActionAlreadyDone is reported (never requested) for a face whose overlapping
	// marker already names a subject.
	ActionAlreadyDone = "already_done"
)

// Sentinel errors returned by the Service so the HTTP layer can map them to status
// codes with errors.Is.
var (
	// ErrInvalidAction indicates an assignment request named an unknown action.
	ErrInvalidAction = errors.New("facematch: invalid action")
	// ErrMissingBBox indicates a create_marker request carried no bounding box.
	ErrMissingBBox = errors.New("facematch: bbox is required for create_marker")
	// ErrMissingMarker indicates an assign/unassign request carried no marker uid.
	ErrMissingMarker = errors.New("facematch: marker_uid is required")
	// ErrMissingSubject indicates an assignment request named neither a subject uid
	// nor a subject name to resolve.
	ErrMissingSubject = errors.New("facematch: subject_uid or subject_name is required")
)

// Suggestion is one likely identity for an unnamed face: the subject and how close
// (cosine distance) and confident (1 - distance, clamped) the nearest assigned
// neighbour of that subject is.
type Suggestion struct {
	SubjectUID  string  `json:"subject_uid"`
	SubjectName string  `json:"subject_name"`
	Distance    float64 `json:"distance"`
	Confidence  float64 `json:"confidence"`
}

// FaceView is one face (or unmatched marker) in the per-photo faces response: its
// normalised box, the matched marker and its assignment, the recommended action,
// and — for an unnamed face — the suggested subjects.
type FaceView struct {
	// FaceIndex is the stored face's per-photo slot, or negative for a marker that
	// matched no detected face (so the detail UI can still render it).
	FaceIndex int `json:"face_index"`
	// BBox is the normalised bounding box [x, y, w, h] in 0..1.
	BBox [4]float64 `json:"bbox"`
	// DetScore is the detector confidence (0 for a marker without a face).
	DetScore float64 `json:"det_score"`
	// Action is the recommended next step (create_marker / assign_person /
	// unassign_person / already_done).
	Action string `json:"action"`
	// MarkerUID, SubjectUID and SubjectName describe the matched marker's
	// assignment; empty when unset.
	MarkerUID   string `json:"marker_uid,omitempty"`
	SubjectUID  string `json:"subject_uid,omitempty"`
	SubjectName string `json:"subject_name,omitempty"`
	// IoU is the overlap with the matched marker (0 when no marker matched).
	IoU float64 `json:"iou,omitempty"`
	// Suggestions lists likely subjects for an unnamed face (never nil; empty when
	// the face is already assigned or the box is offline).
	Suggestions []Suggestion `json:"suggestions"`
}

// FacesResponse is the body of GET /photos/{uid}/faces: the photo's display
// dimensions plus every face and unmatched marker with its assignment and
// suggestions.
type FacesResponse struct {
	PhotoUID    string     `json:"photo_uid"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	Orientation int        `json:"orientation"`
	Faces       []FaceView `json:"faces"`
}

// AssignRequest is the body of POST /photos/{uid}/faces/assign. PhotoUID is filled
// from the path. FaceIndex, when set, names the stored face whose cache is linked
// to the marker. BBox is required for create_marker. A subject is named by SubjectUID
// or, failing that, SubjectName (find-or-create by slug).
type AssignRequest struct {
	PhotoUID    string      `json:"photo_uid"`
	Action      string      `json:"action"`
	FaceIndex   *int        `json:"face_index,omitempty"`
	MarkerUID   string      `json:"marker_uid,omitempty"`
	SubjectUID  string      `json:"subject_uid,omitempty"`
	SubjectName string      `json:"subject_name,omitempty"`
	BBox        *[4]float64 `json:"bbox,omitempty"`
}

// AssignResult is the body returned by a successful assignment: the effective
// action, the affected marker, and the subject (nil after an unassign).
type AssignResult struct {
	Action  string          `json:"action"`
	Marker  people.Marker   `json:"marker"`
	Subject *people.Subject `json:"subject,omitempty"`
}
