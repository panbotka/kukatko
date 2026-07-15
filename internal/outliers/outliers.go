// Package outliers ranks a subject's assigned faces by how far each sits from the
// subject's embedding centroid, surfacing likely-misassigned faces (most distant
// first). The centroid is a robust (trimmed) mean of the subject's ArcFace
// embeddings: the plain mean is computed, the faces furthest from it are dropped,
// and the centre is recomputed, so a handful of badly misassigned faces cannot
// drag the centroid toward themselves and mask exactly what the ranking is meant
// to find. Each face is scored by cosine distance to the trimmed centroid. Faces
// a user has confirmed as correct (internal/feedback) are excluded from the
// returned list so reviewed false alarms are not offered again. The result feeds
// a review UI that unassigns a wrong face through the existing face-assignment
// API, so this package adds no mutation of its own.
package outliers

import (
	"context"
	"fmt"
	"sort"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// MinMeaningful is the smallest number of assigned faces for which outlier ranking
// carries signal. With one or two faces every face is (near) equidistant from the
// centroid, so such a result is reported with Meaningful=false.
const MinMeaningful = 3

// FaceStore is the subset of vectors.Store the service reads: the faces cached as
// assigned to a subject, plus the count of assignments that have no embedding to
// score. It is an interface so the service is unit-testable with a fake;
// vectors.Store satisfies it.
type FaceStore interface {
	// ListFacesBySubject returns every face cached as assigned to subjectUID.
	ListFacesBySubject(ctx context.Context, subjectUID string) ([]vectors.Face, error)
	// CountMarkersWithoutFace returns how many of the subject's valid markers
	// have no embedded face row, so the result can name what cannot be scored.
	CountMarkersWithoutFace(ctx context.Context, subjectUID string) (int, error)
}

// PeopleStore is the subset of people.Store the service reads: the subject is
// validated before ranking so a missing subject answers 404 rather than an empty
// list. people.Store satisfies it.
type PeopleStore interface {
	// GetSubjectByUID returns the subject with uid, or people.ErrSubjectNotFound.
	GetSubjectByUID(ctx context.Context, uid string) (people.Subject, error)
}

// FeedbackStore is the subset of feedback.Store the service reads: the faces a
// user has confirmed as really being the subject, which are excluded from the
// returned outliers so a reviewed false alarm is not offered again.
// *feedback.Store satisfies it.
type FeedbackStore interface {
	// FaceConfirmationsForSubject returns the faces confirmed as "really this
	// person" for the subject.
	FaceConfirmationsForSubject(ctx context.Context, subjectUID string) ([]feedback.FaceRef, error)
}

// Config bundles the Service's collaborators. All fields are required.
type Config struct {
	// Faces reads the faces assigned to a subject.
	Faces FaceStore
	// People validates the subject before ranking.
	People PeopleStore
	// Feedback reads the confirmed faces excluded from the results.
	Feedback FeedbackStore
}

// Service computes per-subject face outliers.
type Service struct {
	faces    FaceStore
	people   PeopleStore
	feedback FeedbackStore
}

// New returns a Service from cfg.
func New(cfg Config) *Service {
	return &Service{faces: cfg.Faces, people: cfg.People, feedback: cfg.Feedback}
}

// Options narrows an outlier query. The zero value keeps the historical
// behaviour: every face, ranked.
type Options struct {
	// Threshold is the minimum cosine distance from the centroid a face must
	// have to be returned; 0 returns everything.
	Threshold float64
	// Limit caps how many faces are returned (after the threshold filter);
	// 0 means no cap.
	Limit int
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
// first (largest distance from the trimmed centroid), filtered by the query's
// threshold and limit and with confirmed faces excluded. Count, Meaningful and
// AvgDistance describe the full scored set, before any filtering, so the stats
// stay honest whatever the caller narrows the list to. Meaningful is false for
// sets too small (fewer than MinMeaningful faces) to single out any one face.
type Result struct {
	SubjectUID string `json:"subject_uid"`
	// Count is the number of assigned faces with an embedding — the scored set.
	Count      int  `json:"count"`
	Meaningful bool `json:"meaningful"`
	// AvgDistance is the mean centroid distance over the scored set, 0 when
	// empty.
	AvgDistance float64 `json:"avg_distance"`
	// NoEmbedding is how many of the subject's assignments have no embedding and
	// therefore cannot be checked (for example a face tagged while the embedding
	// sidecar was offline).
	NoEmbedding int           `json:"no_embedding"`
	Faces       []OutlierFace `json:"faces"`
}

// Outliers loads every face assigned to subjectUID, scores each by cosine
// distance from the subject's trimmed centroid and returns them most-distant
// (most likely misassigned) first, narrowed by opts: faces a user has confirmed
// as correct are excluded, then the threshold and limit apply. It returns
// people.ErrSubjectNotFound when no such subject exists. Small sets (fewer than
// MinMeaningful faces) are returned ranked but with Meaningful=false, because no
// face stands out from so few.
func (s *Service) Outliers(ctx context.Context, subjectUID string, opts Options) (Result, error) {
	if _, err := s.people.GetSubjectByUID(ctx, subjectUID); err != nil {
		return Result{}, fmt.Errorf("loading subject %s: %w", subjectUID, err)
	}
	faces, err := s.faces.ListFacesBySubject(ctx, subjectUID)
	if err != nil {
		return Result{}, fmt.Errorf("listing faces for subject %s: %w", subjectUID, err)
	}
	noEmbedding, err := s.faces.CountMarkersWithoutFace(ctx, subjectUID)
	if err != nil {
		return Result{}, fmt.Errorf("counting unscored markers for subject %s: %w", subjectUID, err)
	}
	confirmed, err := s.feedback.FaceConfirmationsForSubject(ctx, subjectUID)
	if err != nil {
		return Result{}, fmt.Errorf("listing confirmed faces for subject %s: %w", subjectUID, err)
	}
	ranked := rankByDistance(faces)
	return Result{
		SubjectUID:  subjectUID,
		Count:       len(faces),
		Meaningful:  len(faces) >= MinMeaningful,
		AvgDistance: averageDistance(ranked),
		NoEmbedding: noEmbedding,
		Faces:       narrow(ranked, confirmed, opts),
	}, nil
}

// trimCount returns how many of n faces the robust centroid drops before being
// recomputed: the top decile by distance, rounded up so small sets still shed
// their worst face, floored so at least MinMeaningful faces always remain (a
// person with 4 faces loses 1, never half). Sets of MinMeaningful or fewer are
// never trimmed.
func trimCount(n int) int {
	trim := (n + 9) / 10
	if n-trim < MinMeaningful {
		trim = n - MinMeaningful
	}
	if trim < 0 {
		return 0
	}
	return trim
}

// rankByDistance scores each face by cosine distance from the trimmed centroid
// of the faces' embeddings and returns them most-distant first. Every face is
// scored — including the ones the trim excluded from the centroid — so likely
// outliers rank high instead of pulling the centre toward themselves. Ties break
// on (photo_uid, face_index) for a deterministic order. An empty input yields an
// empty (non-nil) slice.
func rankByDistance(faces []vectors.Face) []OutlierFace {
	vecs := make([][]float32, len(faces))
	for i := range faces {
		vecs[i] = faces[i].Vector
	}
	centroid := vectors.TrimmedCentroid(vecs, trimCount(len(faces)))

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

// averageDistance returns the mean centroid distance over the ranked faces, or 0
// for an empty set.
func averageDistance(ranked []OutlierFace) float64 {
	if len(ranked) == 0 {
		return 0
	}
	var sum float64
	for i := range ranked {
		sum += ranked[i].Distance
	}
	return sum / float64(len(ranked))
}

// narrow filters the ranked faces down to what the caller asked to see: faces a
// user confirmed as correct are dropped, faces below the distance threshold are
// dropped, and the limit caps what is left. The input is already sorted, so the
// cap keeps the most suspicious faces. It always returns a non-nil slice.
func narrow(ranked []OutlierFace, confirmed []feedback.FaceRef, opts Options) []OutlierFace {
	skip := make(map[vectors.FaceKey]struct{}, len(confirmed))
	for _, ref := range confirmed {
		skip[vectors.FaceKey{PhotoUID: ref.PhotoUID, FaceIndex: ref.FaceIndex}] = struct{}{}
	}
	out := make([]OutlierFace, 0, len(ranked))
	for _, face := range ranked {
		if _, ok := skip[vectors.FaceKey{PhotoUID: face.PhotoUID, FaceIndex: face.FaceIndex}]; ok {
			continue
		}
		if face.Distance < opts.Threshold {
			continue
		}
		if opts.Limit > 0 && len(out) == opts.Limit {
			break
		}
		out = append(out, face)
	}
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
