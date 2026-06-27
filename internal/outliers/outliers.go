// Package outliers ranks a subject's assigned faces by how far each sits from the
// subject's embedding centroid, surfacing likely-misassigned faces (most distant
// first). The centroid is the element-wise mean of the subject's ArcFace
// embeddings and each face is scored by cosine distance to it, mirroring the
// photo-sorter heuristic. The result feeds a review UI that unassigns a wrong face
// through the existing face-assignment API, so this package adds no mutation of
// its own.
package outliers

import (
	"context"
	"fmt"
	"sort"

	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// MinMeaningful is the smallest number of assigned faces for which outlier ranking
// carries signal. With one or two faces every face is (near) equidistant from the
// centroid, so such a result is reported with Meaningful=false.
const MinMeaningful = 3

// FaceStore is the subset of vectors.Store the service reads: the faces cached as
// assigned to a subject. It is an interface so the service is unit-testable with a
// fake; vectors.Store satisfies it.
type FaceStore interface {
	// ListFacesBySubject returns every face cached as assigned to subjectUID.
	ListFacesBySubject(ctx context.Context, subjectUID string) ([]vectors.Face, error)
}

// PeopleStore is the subset of people.Store the service reads: the subject is
// validated before ranking so a missing subject answers 404 rather than an empty
// list. people.Store satisfies it.
type PeopleStore interface {
	// GetSubjectByUID returns the subject with uid, or people.ErrSubjectNotFound.
	GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error)
}

// Config bundles the Service's collaborators. Both fields are required.
type Config struct {
	// Faces reads the faces assigned to a subject.
	Faces FaceStore
	// People validates the subject before ranking.
	People PeopleStore
}

// Service computes per-subject face outliers.
type Service struct {
	faces  FaceStore
	people PeopleStore
}

// New returns a Service from cfg.
func New(cfg Config) *Service {
	return &Service{faces: cfg.Faces, people: cfg.People}
}

// OutlierFace is one assigned face scored by its cosine distance from the
// subject's centroid, with the photo/render hints a UI needs to render a cropped
// thumbnail and to unassign the face via the existing assignment API.
type OutlierFace struct {
	// PhotoUID and FaceIndex identify the face within its photo.
	PhotoUID  string `json:"photo_uid"`
	FaceIndex int    `json:"face_index"`
	// BBox is the normalised bounding box [x, y, w, h] in 0..1.
	BBox [4]float64 `json:"bbox"`
	// DetScore is the detector confidence for the face.
	DetScore float64 `json:"det_score"`
	// Distance is the cosine distance from the subject's centroid; larger means
	// more suspicious.
	Distance float64 `json:"distance"`
	// MarkerUID is the marker the face is tied to, empty when unmatched.
	MarkerUID string `json:"marker_uid,omitempty"`
	// Width, Height and Orientation are the photo's display hints for cropping.
	Width       int `json:"width"`
	Height      int `json:"height"`
	Orientation int `json:"orientation"`
}

// Result is the outlier ranking for one subject: its faces ordered most-suspicious
// first (largest distance from the centroid). Meaningful is false for sets too
// small (fewer than MinMeaningful faces) to single out any one face.
type Result struct {
	SubjectUID string        `json:"subject_uid"`
	Count      int           `json:"count"`
	Meaningful bool          `json:"meaningful"`
	Faces      []OutlierFace `json:"faces"`
}

// Outliers loads every face assigned to subjectUID, computes their centroid and
// returns the faces ranked by cosine distance from it (most distant — most likely
// misassigned — first). It returns people.ErrSubjectNotFound when no such subject
// exists. Small sets (fewer than MinMeaningful faces) are returned ranked but with
// Meaningful=false, because no face stands out from so few.
func (s *Service) Outliers(ctx context.Context, subjectUID string) (Result, error) {
	if _, err := s.people.GetSubjectByUID(ctx, subjectUID); err != nil {
		return Result{}, fmt.Errorf("loading subject %s: %w", subjectUID, err)
	}
	faces, err := s.faces.ListFacesBySubject(ctx, subjectUID)
	if err != nil {
		return Result{}, fmt.Errorf("listing faces for subject %s: %w", subjectUID, err)
	}
	return Result{
		SubjectUID: subjectUID,
		Count:      len(faces),
		Meaningful: len(faces) >= MinMeaningful,
		Faces:      rankByDistance(faces),
	}, nil
}

// rankByDistance scores each face by cosine distance from the centroid of all the
// faces' embeddings and returns them most-distant first. Ties break on
// (photo_uid, face_index) for a deterministic order. An empty input yields an
// empty (non-nil) slice.
func rankByDistance(faces []vectors.Face) []OutlierFace {
	vecs := make([][]float32, len(faces))
	for i := range faces {
		vecs[i] = faces[i].Vector
	}
	centroid := vectors.Centroid(vecs)

	out := make([]OutlierFace, 0, len(faces))
	for i := range faces {
		out = append(out, toOutlierFace(faces[i], vectors.CosineDistance(centroid, faces[i].Vector)))
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch {
		case out[i].Distance != out[j].Distance:
			return out[i].Distance > out[j].Distance
		case out[i].PhotoUID != out[j].PhotoUID:
			return out[i].PhotoUID < out[j].PhotoUID
		default:
			return out[i].FaceIndex < out[j].FaceIndex
		}
	})
	return out
}

// toOutlierFace projects a stored face plus its centroid distance into the API
// shape, dereferencing the nullable marker uid.
func toOutlierFace(f vectors.Face, distance float64) OutlierFace {
	marker := ""
	if f.MarkerUID != nil {
		marker = *f.MarkerUID
	}
	return OutlierFace{
		PhotoUID:    f.PhotoUID,
		FaceIndex:   f.FaceIndex,
		BBox:        f.BBox,
		DetScore:    f.DetScore,
		Distance:    distance,
		MarkerUID:   marker,
		Width:       f.PhotoWidth,
		Height:      f.PhotoHeight,
		Orientation: f.Orientation,
	}
}
