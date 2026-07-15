package outliers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakeFaces is a FaceStore returning a fixed face set (or error) and a fixed
// count of unscored markers.
type fakeFaces struct {
	faces       []vectors.Face
	noEmbedding int
	err         error
}

// ListFacesBySubject returns the canned faces and error regardless of subject.
func (f fakeFaces) ListFacesBySubject(_ context.Context, _ string) ([]vectors.Face, error) {
	return f.faces, f.err
}

// CountMarkersWithoutFace returns the canned unscored-marker count.
func (f fakeFaces) CountMarkersWithoutFace(_ context.Context, _ string) (int, error) {
	return f.noEmbedding, nil
}

// fakePeople is a PeopleStore that reports a subject exists unless err is set.
type fakePeople struct {
	err error
}

// GetSubjectByUID returns a minimal subject, or the canned error.
func (p fakePeople) GetSubjectByUID(_ context.Context, uid string) (people.Subject, error) {
	if p.err != nil {
		return people.Subject{}, p.err
	}
	return people.Subject{UID: uid}, nil
}

// fakeFeedback is a FeedbackStore returning a fixed confirmed-face set.
type fakeFeedback struct {
	confirmed []feedback.FaceRef
}

// FaceConfirmationsForSubject returns the canned confirmations regardless of
// subject.
func (f fakeFeedback) FaceConfirmationsForSubject(_ context.Context, _ string) ([]feedback.FaceRef, error) {
	return f.confirmed, nil
}

// newService builds a Service over the given faces with pass-through people and
// no confirmations.
func newService(faces fakeFaces) *outliers.Service {
	return outliers.New(outliers.Config{Faces: faces, People: fakePeople{}, Feedback: fakeFeedback{}})
}

// face builds a face with the given photo uid, index and embedding.
func face(photoUID string, index int, vec []float32) vectors.Face {
	return vectors.Face{PhotoUID: photoUID, FaceIndex: index, Vector: vec, PhotoWidth: 100, PhotoHeight: 80}
}

// clusterWithOutliers builds a tight ten-face cluster plus two faces orthogonal
// to it, the planted outliers.
func clusterWithOutliers() []vectors.Face {
	faces := make([]vectors.Face, 0, 12)
	for i := range 10 {
		faces = append(faces, face(fmt.Sprintf("member-%02d", i), 0, []float32{1, 0.01 * float32(i)}))
	}
	faces = append(faces,
		face("planted-a", 0, []float32{0, 1}),
		face("planted-b", 0, []float32{0.05, 1}),
	)
	return faces
}

// TestOutliers_rankingOrder plants one face orthogonal to several consistent ones
// and checks it ranks first with strictly non-increasing distances behind it.
func TestOutliers_rankingOrder(t *testing.T) {
	t.Parallel()
	faces := []vectors.Face{
		face("p1", 0, []float32{1, 0}),
		face("p2", 0, []float32{1, 0.1}),
		face("p3", 0, []float32{1, 0.05}),
		face("outlier", 0, []float32{0, 1}),
	}
	svc := newService(fakeFaces{faces: faces})

	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 4 || !res.Meaningful {
		t.Fatalf("Count=%d Meaningful=%v, want 4 true", res.Count, res.Meaningful)
	}
	if res.Faces[0].PhotoUID != "outlier" {
		t.Errorf("outlier not ranked first: %+v", res.Faces)
	}
	for i := 1; i < len(res.Faces); i++ {
		if res.Faces[i].Distance > res.Faces[i-1].Distance {
			t.Errorf("distances not descending at %d: %+v", i, res.Faces)
		}
	}
	if res.AvgDistance <= 0 {
		t.Errorf("AvgDistance = %g, want > 0", res.AvgDistance)
	}
}

// TestOutliers_trimmedCentroidScoresOutliersHigher plants two obvious outliers
// among ten consistent faces and checks they score strictly higher against the
// trimmed centroid than they would against the plain (untrimmed) centroid — the
// outliers no longer drag the centre toward themselves.
func TestOutliers_trimmedCentroidScoresOutliersHigher(t *testing.T) {
	t.Parallel()
	faces := clusterWithOutliers()
	svc := newService(fakeFaces{faces: faces})

	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}

	vecs := make([][]float32, len(faces))
	for i := range faces {
		vecs[i] = faces[i].Vector
	}
	plain := vectors.Centroid(vecs)

	byPhoto := make(map[string]float64, len(res.Faces))
	for _, f := range res.Faces {
		byPhoto[f.PhotoUID] = f.Distance
	}
	for i, planted := range []string{"planted-a", "planted-b"} {
		untrimmed := vectors.CosineDistance(plain, faces[10+i].Vector)
		if byPhoto[planted] <= untrimmed {
			t.Errorf("%s trimmed distance %g <= untrimmed %g, want strictly higher",
				planted, byPhoto[planted], untrimmed)
		}
	}
	if res.Faces[0].PhotoUID != "planted-a" && res.Faces[0].PhotoUID != "planted-b" {
		t.Errorf("planted outlier not ranked first: %+v", res.Faces[0])
	}
}

// TestOutliers_threshold checks only faces at or above the minimum distance are
// returned while the stats keep describing the full scored set.
func TestOutliers_threshold(t *testing.T) {
	t.Parallel()
	svc := newService(fakeFaces{faces: clusterWithOutliers()})

	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{Threshold: 0.5})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if len(res.Faces) != 2 {
		t.Fatalf("faces above threshold = %d, want the 2 planted outliers: %+v", len(res.Faces), res.Faces)
	}
	for _, f := range res.Faces {
		if f.Distance < 0.5 {
			t.Errorf("face %s below threshold: %g", f.PhotoUID, f.Distance)
		}
	}
	if res.Count != 12 {
		t.Errorf("Count = %d, want 12 (stats describe the full set)", res.Count)
	}
}

// TestOutliers_limit checks the limit caps the list at the most suspicious faces.
func TestOutliers_limit(t *testing.T) {
	t.Parallel()
	svc := newService(fakeFaces{faces: clusterWithOutliers()})

	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{Limit: 3})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if len(res.Faces) != 3 {
		t.Fatalf("faces = %d, want 3", len(res.Faces))
	}
	top := map[string]bool{res.Faces[0].PhotoUID: true, res.Faces[1].PhotoUID: true}
	if !top["planted-a"] || !top["planted-b"] {
		t.Errorf("limit did not keep the most suspicious faces: %+v", res.Faces)
	}
	if res.Count != 12 {
		t.Errorf("Count = %d, want 12 (stats describe the full set)", res.Count)
	}
}

// TestOutliers_confirmedExcluded checks a face confirmed as correct disappears
// from the list while the scored-set stats still include it.
func TestOutliers_confirmedExcluded(t *testing.T) {
	t.Parallel()
	faces := clusterWithOutliers()
	svc := outliers.New(outliers.Config{
		Faces:  fakeFaces{faces: faces},
		People: fakePeople{},
		Feedback: fakeFeedback{confirmed: []feedback.FaceRef{
			{PhotoUID: "planted-a", FaceIndex: 0},
		}},
	})

	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	for _, f := range res.Faces {
		if f.PhotoUID == "planted-a" {
			t.Errorf("confirmed face still returned: %+v", f)
		}
	}
	if len(res.Faces) != 11 || res.Count != 12 {
		t.Errorf("faces=%d Count=%d, want 11 returned of 12 scored", len(res.Faces), res.Count)
	}
}

// TestOutliers_noEmbeddingCount surfaces the store's unscored-marker count.
func TestOutliers_noEmbeddingCount(t *testing.T) {
	t.Parallel()
	svc := newService(fakeFaces{faces: clusterWithOutliers(), noEmbedding: 5})
	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.NoEmbedding != 5 {
		t.Errorf("NoEmbedding = %d, want 5", res.NoEmbedding)
	}
}

// TestOutliers_deterministicTieBreak checks equal distances order by
// (photo_uid, face_index).
func TestOutliers_deterministicTieBreak(t *testing.T) {
	t.Parallel()
	faces := []vectors.Face{
		face("pb", 1, []float32{1, 0}),
		face("pa", 0, []float32{1, 0}),
		face("pa", 1, []float32{1, 0}),
	}
	svc := newService(fakeFaces{faces: faces})

	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	want := []struct {
		uid string
		idx int
	}{{"pa", 0}, {"pa", 1}, {"pb", 1}}
	for i, w := range want {
		if res.Faces[i].PhotoUID != w.uid || res.Faces[i].FaceIndex != w.idx {
			t.Errorf("tie-break order[%d] = %s/%d, want %s/%d",
				i, res.Faces[i].PhotoUID, res.Faces[i].FaceIndex, w.uid, w.idx)
		}
	}
}

// TestOutliers_smallSet checks that one or two faces are returned but flagged as
// not meaningful, without error.
func TestOutliers_smallSet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		faces []vectors.Face
	}{
		{name: "single face", faces: []vectors.Face{face("p1", 0, []float32{1, 0})}},
		{name: "two faces", faces: []vectors.Face{
			face("p1", 0, []float32{1, 0}),
			face("p2", 0, []float32{0, 1}),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newService(fakeFaces{faces: tt.faces})
			res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
			if err != nil {
				t.Fatalf("Outliers: %v", err)
			}
			if res.Meaningful {
				t.Errorf("Meaningful = true, want false for %d faces", len(tt.faces))
			}
			if res.Count != len(tt.faces) || len(res.Faces) != len(tt.faces) {
				t.Errorf("Count=%d Faces=%d, want %d", res.Count, len(res.Faces), len(tt.faces))
			}
		})
	}
}

// TestOutliers_empty checks a subject with no assigned faces yields an empty,
// non-nil face list.
func TestOutliers_empty(t *testing.T) {
	t.Parallel()
	svc := newService(fakeFaces{})
	res, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 0 || res.Meaningful || res.Faces == nil || len(res.Faces) != 0 {
		t.Errorf("empty result mismatch: %+v", res)
	}
	if res.AvgDistance != 0 {
		t.Errorf("AvgDistance = %g, want 0 for an empty set", res.AvgDistance)
	}
}

// TestOutliers_subjectNotFound surfaces the people sentinel unchanged.
func TestOutliers_subjectNotFound(t *testing.T) {
	t.Parallel()
	svc := outliers.New(outliers.Config{
		Faces:    fakeFaces{},
		People:   fakePeople{err: people.ErrSubjectNotFound},
		Feedback: fakeFeedback{},
	})
	_, err := svc.Outliers(context.Background(), "su_missing", outliers.Options{})
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("Outliers = %v, want ErrSubjectNotFound", err)
	}
}

// TestOutliers_faceStoreError wraps and returns a face-store failure.
func TestOutliers_faceStoreError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	svc := newService(fakeFaces{err: sentinel})
	_, err := svc.Outliers(context.Background(), "su_alice", outliers.Options{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Outliers = %v, want wrapped sentinel", err)
	}
}
