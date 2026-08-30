package facematch

import (
	"context"
	"fmt"

	"github.com/panbotka/kukatko/internal/people"
)

// PersonOnPhoto is one person on a photo, as the photo detail reports them: a
// marker that names a subject, or a detection nobody has named yet.
//
// It is deliberately narrower than FaceView. A reader asking "who is on this
// photo?" wants the roll-call, not the assignment state machine, and above all
// not the ranked suggestions — those cost a vector search per face and are only
// worth paying for in the face editor, which has its own endpoint.
type PersonOnPhoto struct {
	// SubjectUID and SubjectName name the person. Both are empty for a detection
	// that nobody has assigned yet — that is exactly what makes it unassigned.
	SubjectUID  string `json:"subject_uid,omitempty"`
	SubjectName string `json:"subject_name,omitempty"`
	// MarkerUID is the region the person was marked on, empty for a detection
	// that no marker claims.
	MarkerUID string `json:"marker_uid,omitempty"`
	// BBox is the normalised bounding box [x, y, w, h] in 0..1.
	BBox [4]float64 `json:"bbox"`
	// DetScore is the detector's confidence in the detection, 0 for a marker
	// drawn by hand that no detected face matched.
	DetScore float64 `json:"det_score,omitempty"`
}

// PhotoPeople returns who is on the photo: every detected face with the subject
// its matched marker names (empty when nobody has named it), followed by the
// face-type markers that matched no detection — the regions drawn by hand.
//
// It shares the exclusive IoU pairing with PhotoFaces but does none of its
// expensive or mutating work: no suggestion search (up to two HNSW queries per
// face) and no rewriting of the cached face↔marker link. That makes it cheap
// enough to answer a detail request with, and keeps a plain read a plain read.
// A photo with no faces and no markers yields an empty, non-nil slice.
func (s *Service) PhotoPeople(ctx context.Context, photoUID string) ([]PersonOnPhoto, error) {
	faces, err := s.faces.ListFaces(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("facematch: listing faces for %s: %w", photoUID, err)
	}
	markers, err := s.people.ListMarkersByPhoto(ctx, photoUID)
	if err != nil {
		return nil, fmt.Errorf("facematch: listing markers for %s: %w", photoUID, err)
	}

	matched := matchMarkers(faces, markers, s.iouThreshold)
	names := s.assignedSubjectNames(ctx, markers)
	byUID := markersByUID(markers)

	onPhoto := make([]PersonOnPhoto, 0, len(faces)+len(markers))
	for i := range faces {
		person := PersonOnPhoto{BBox: faces[i].BBox, DetScore: faces[i].DetScore}
		if match, ok := matched[faces[i].FaceIndex]; ok {
			marker := byUID[match.MarkerUID]
			person.MarkerUID = marker.UID
			nameSubject(&person, marker, names)
		}
		onPhoto = append(onPhoto, person)
	}
	return appendUnmatchedPeople(onPhoto, markers, matched.claims(), names), nil
}

// appendUnmatchedPeople adds the face-type, non-invalid markers that no detection
// claimed in the exclusive pairing — hand-drawn regions, and stale ones left over
// from a re-detection. claimed maps a marker uid to the face index that won it.
func appendUnmatchedPeople(
	onPhoto []PersonOnPhoto, markers []people.Marker, claimed map[string]int, names map[string]string,
) []PersonOnPhoto {
	for i := range markers {
		marker := markers[i]
		if _, taken := claimed[marker.UID]; marker.Type != people.MarkerFace || marker.Invalid || taken {
			continue
		}
		person := PersonOnPhoto{MarkerUID: marker.UID, BBox: markerBox(marker)}
		nameSubject(&person, marker, names)
		onPhoto = append(onPhoto, person)
	}
	return onPhoto
}

// nameSubject copies the marker's subject onto person, leaving both name fields
// empty when the marker names nobody.
func nameSubject(person *PersonOnPhoto, marker people.Marker, names map[string]string) {
	uid := derefSubject(marker.SubjectUID)
	if uid == "" {
		return
	}
	person.SubjectUID = uid
	person.SubjectName = names[uid]
}
