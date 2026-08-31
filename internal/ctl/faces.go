package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Sentinel errors raised by the face commands before a request is spent.
var (
	// ErrUnknownFace indicates the photo carries no face with the requested index.
	ErrUnknownFace = errors.New("ctl: this photo has no such face")
	// ErrFaceNotMarked indicates a detach of a face that carries no marker, so
	// there is no assignment to undo.
	ErrFaceNotMarked = errors.New("ctl: this face carries no marker, so nobody is assigned to it")
	// ErrFaceNotNamed indicates a detach of a marker that names nobody.
	ErrFaceNotNamed = errors.New("ctl: this face names nobody, so there is nothing to detach")
	// ErrNegativeFaceIndex indicates a feedback command was given a negative face
	// index, which the API rejects with 400.
	ErrNegativeFaceIndex = errors.New("ctl: face index must not be negative")
)

// The assignment-state actions of POST /photos/{uid}/faces/assign, mirrored from
// internal/facematch. The state machine itself stays on the server: ctl only
// names the action the server's own face listing recommends.
const (
	// FaceActionCreateMarker draws a new marker over a detection and names it.
	FaceActionCreateMarker = "create_marker"
	// FaceActionAssignPerson names an existing, already drawn marker.
	FaceActionAssignPerson = "assign_person"
	// FaceActionUnassignPerson clears the subject from a marker.
	FaceActionUnassignPerson = "unassign_person"
	// FaceActionAlreadyDone is only ever reported, never requested: the marker
	// overlapping this detection already names a subject.
	FaceActionAlreadyDone = "already_done"
)

// FaceSuggestion is one likely identity for a face or a cluster: the subject and
// how close (cosine distance) and confident (1 - distance) the nearest tagged
// neighbour of that subject is.
type FaceSuggestion struct {
	SubjectUID  string  `json:"subject_uid"`
	SubjectName string  `json:"subject_name"`
	Distance    float64 `json:"distance"`
	Confidence  float64 `json:"confidence"`
}

// FaceView is one detection (or one marker that matched no detection) of GET
// /photos/{uid}/faces: its normalised box, the marker it was paired with and who
// that marker names, the action that would advance it, and the identities the
// server suggests for it.
type FaceView struct {
	// FaceIndex is the detection's per-photo slot, or negative for a marker that
	// matched no detection — a box a person drew where the detector saw nothing.
	FaceIndex int `json:"face_index"`
	// BBox is the normalised box [x, y, w, h] in 0..1 display space.
	BBox [4]float64 `json:"bbox"`
	// DetScore is the detector's confidence, 0 for a marker without a detection.
	DetScore float64 `json:"det_score"`
	// Action is what the server recommends doing with this face next.
	Action string `json:"action"`
	// MarkerUID, SubjectUID and SubjectName describe the paired marker; empty
	// when the detection has no marker, or the marker names nobody.
	MarkerUID   string `json:"marker_uid,omitempty"`
	SubjectUID  string `json:"subject_uid,omitempty"`
	SubjectName string `json:"subject_name,omitempty"`
	// IoU is the overlap with the paired marker, 0 when none matched.
	IoU float64 `json:"iou,omitempty"`
	// Suggestions are candidate identities for an unnamed face and alternatives
	// for a named one. Never nil on the wire, but may be empty.
	Suggestions []FaceSuggestion `json:"suggestions"`
}

// Named reports whether this face already names a subject.
func (v FaceView) Named() bool {
	return v.SubjectUID != "" || v.SubjectName != ""
}

// FaceList is the body of GET /photos/{uid}/faces: the photo's display frame plus
// every detection and unmatched marker on it.
type FaceList struct {
	PhotoUID    string     `json:"photo_uid"`
	Width       int        `json:"width"`
	Height      int        `json:"height"`
	Orientation int        `json:"orientation"`
	Faces       []FaceView `json:"faces"`
}

// Find returns the face with the given per-photo index, or ErrUnknownFace naming
// the indexes the photo actually has — a face index is not a uid, and guessing
// one wrong should say what was available rather than 404 from the server.
func (l FaceList) Find(faceIndex int) (FaceView, error) {
	for _, face := range l.Faces {
		if face.FaceIndex == faceIndex {
			return face, nil
		}
	}
	return FaceView{}, fmt.Errorf("%w: %d is not one of %s",
		ErrUnknownFace, faceIndex, joinFaceIndexes(l.Faces))
}

// joinFaceIndexes renders the face indexes of a photo for an error message.
func joinFaceIndexes(faces []FaceView) string {
	if len(faces) == 0 {
		return "(none — nothing has been detected on it)"
	}
	out := make([]byte, 0, len(faces)*3)
	for i, face := range faces {
		if i > 0 {
			out = append(out, ',', ' ')
		}
		out = strconv.AppendInt(out, int64(face.FaceIndex), 10)
	}
	return string(out)
}

// FaceAssignment is the body of POST /photos/{uid}/faces/assign. It is built from
// a FaceView rather than by hand: which action applies follows from whether the
// detection already carries a marker, and only the server knows that.
type FaceAssignment struct {
	Action      string      `json:"action"`
	FaceIndex   *int        `json:"face_index,omitempty"`
	MarkerUID   string      `json:"marker_uid,omitempty"`
	SubjectUID  string      `json:"subject_uid,omitempty"`
	SubjectName string      `json:"subject_name,omitempty"`
	BBox        *[4]float64 `json:"bbox,omitempty"`
}

// AssignTo builds the request that attaches this face to subject: naming an
// existing marker when the detection has one, and drawing a marker over the
// detection when it has not. It is the same routing the web face editor does, and
// deliberately no more: the state machine, the auto-creation of a subject by name
// and every threshold stay on the server.
func (v FaceView) AssignTo(subject SubjectRef) FaceAssignment {
	if v.MarkerUID != "" {
		return FaceAssignment{
			Action:      FaceActionAssignPerson,
			MarkerUID:   v.MarkerUID,
			SubjectUID:  subject.UID,
			SubjectName: subject.Name,
		}
	}
	index := v.FaceIndex
	bbox := v.BBox
	return FaceAssignment{
		Action:      FaceActionCreateMarker,
		FaceIndex:   &index,
		BBox:        &bbox,
		SubjectUID:  subject.UID,
		SubjectName: subject.Name,
	}
}

// Detach builds the request that clears whoever this face names. A detection with
// no marker yields ErrFaceNotMarked and one whose marker names nobody
// ErrFaceNotNamed, so a mistyped index fails locally instead of as a puzzling 404.
// Neither names the face: the caller already knows which one it asked about, and
// wraps these with it.
func (v FaceView) Detach() (FaceAssignment, error) {
	if v.MarkerUID == "" {
		return FaceAssignment{}, ErrFaceNotMarked
	}
	if !v.Named() {
		return FaceAssignment{}, ErrFaceNotNamed
	}
	return FaceAssignment{Action: FaceActionUnassignPerson, MarkerUID: v.MarkerUID}, nil
}

// Marker is the normalised region POST /photos/{uid}/faces/assign echoes back,
// in 0..1 display space like every face box.
type Marker struct {
	UID        string  `json:"uid"`
	PhotoUID   string  `json:"photo_uid"`
	SubjectUID *string `json:"subject_uid,omitempty"`
	Type       string  `json:"type"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	W          float64 `json:"w"`
	H          float64 `json:"h"`
	Invalid    bool    `json:"invalid"`
	Reviewed   bool    `json:"reviewed"`
}

// FaceAssignResult is the body of a successful assignment: the action the server
// actually applied, the marker it wrote, and the subject it now names — nil after
// a detach, which is what makes the two distinguishable in the output.
type FaceAssignResult struct {
	Action  string   `json:"action"`
	Marker  Marker   `json:"marker"`
	Subject *Subject `json:"subject,omitempty"`
}

// FaceOpinion is the body shared by the four face-feedback endpoints: which
// detection on which photo, and the person the opinion is about.
type FaceOpinion struct {
	PhotoUID   string `json:"photo_uid"`
	FaceIndex  int    `json:"face_index"`
	SubjectUID string `json:"subject_uid"`
}

// validate range-checks an opinion the API would only answer with a 400.
func (o FaceOpinion) validate() error {
	if err := requireUID("photo", o.PhotoUID); err != nil {
		return err
	}
	if err := requireUID("subject", o.SubjectUID); err != nil {
		return err
	}
	if o.FaceIndex < 0 {
		return fmt.Errorf("%w: %d", ErrNegativeFaceIndex, o.FaceIndex)
	}
	return nil
}

// ListFaces fetches GET /photos/{uid}/faces and returns the raw JSON body: every
// detection on the photo with the marker it was paired with, who that marker
// names, and the identities the server suggests. Decode it with DecodeFaceList.
func (c *Client) ListFaces(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.get(ctx, "/photos/"+url.PathEscape(photoUID)+"/faces", nil)
}

// AssignFace posts one assignment action to POST /photos/{uid}/faces/assign and
// returns the raw result. It needs the editor or admin role.
func (c *Client) AssignFace(
	ctx context.Context, photoUID string, in FaceAssignment,
) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/photos/"+url.PathEscape(photoUID)+"/faces/assign", in)
}

// RejectFace records "this face is NOT this person" at POST
// /feedback/face-rejections. It is an opinion: nothing is detached, and a repeated
// call is a no-op.
func (c *Client) RejectFace(ctx context.Context, in FaceOpinion) error {
	return c.faceOpinion(ctx, http.MethodPost, "/feedback/face-rejections", in)
}

// UnrejectFace withdraws a rejection at DELETE /feedback/face-rejections.
func (c *Client) UnrejectFace(ctx context.Context, in FaceOpinion) error {
	return c.faceOpinion(ctx, http.MethodDelete, "/feedback/face-rejections", in)
}

// ConfirmFace records "this face IS this person" at POST
// /feedback/face-confirmations — the opposite of a rejection, not a stronger form
// of it. A confirmed face drops out of the outlier review, so the same false alarm
// is not offered forever.
func (c *Client) ConfirmFace(ctx context.Context, in FaceOpinion) error {
	return c.faceOpinion(ctx, http.MethodPost, "/feedback/face-confirmations", in)
}

// UnconfirmFace withdraws a confirmation at DELETE /feedback/face-confirmations.
func (c *Client) UnconfirmFace(ctx context.Context, in FaceOpinion) error {
	return c.faceOpinion(ctx, http.MethodDelete, "/feedback/face-confirmations", in)
}

// faceOpinion drives the four feedback endpoints, which share a body and a 204 and
// differ only in path and verb — the DELETE half carries a body too.
func (c *Client) faceOpinion(ctx context.Context, method, path string, in FaceOpinion) error {
	if err := in.validate(); err != nil {
		return err
	}
	_, err := c.send(ctx, method, path, in)
	return err
}

// DecodeFaceList decodes the body of GET /photos/{uid}/faces.
func DecodeFaceList(raw json.RawMessage) (FaceList, error) {
	var list FaceList
	if err := json.Unmarshal(raw, &list); err != nil {
		return FaceList{}, fmt.Errorf("decoding the face list: %w", err)
	}
	return list, nil
}

// DecodeFaceAssign decodes the body of POST /photos/{uid}/faces/assign.
func DecodeFaceAssign(raw json.RawMessage) (FaceAssignResult, error) {
	var result FaceAssignResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return FaceAssignResult{}, fmt.Errorf("decoding the assignment: %w", err)
	}
	return result, nil
}
