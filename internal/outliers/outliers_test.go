package outliers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakeFaces is a FaceStore returning a fixed face set (or error).
type fakeFaces struct {
	faces []vectors.Face
	err   error
}

// ListFacesBySubject returns the canned faces and error regardless of subject.
func (f fakeFaces) ListFacesBySubject(_ context.Context, _ string) ([]vectors.Face, error) {
	return f.faces, f.err
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

// face builds a face with the given photo uid, index and embedding.
func face(photoUID string, index int, vec []float32) vectors.Face {
	return vectors.Face{PhotoUID: photoUID, FaceIndex: index, Vector: vec, PhotoWidth: 100, PhotoHeight: 80}
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
	svc := outliers.New(outliers.Config{Faces: fakeFaces{faces: faces}, People: fakePeople{}})

	res, err := svc.Outliers(context.Background(), "su_alice")
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
	svc := outliers.New(outliers.Config{Faces: fakeFaces{faces: faces}, People: fakePeople{}})

	res, err := svc.Outliers(context.Background(), "su_alice")
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
// not meaningful.
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
			svc := outliers.New(outliers.Config{Faces: fakeFaces{faces: tt.faces}, People: fakePeople{}})
			res, err := svc.Outliers(context.Background(), "su_alice")
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
	svc := outliers.New(outliers.Config{Faces: fakeFaces{}, People: fakePeople{}})
	res, err := svc.Outliers(context.Background(), "su_alice")
	if err != nil {
		t.Fatalf("Outliers: %v", err)
	}
	if res.Count != 0 || res.Meaningful || res.Faces == nil || len(res.Faces) != 0 {
		t.Errorf("empty result mismatch: %+v", res)
	}
}

// TestOutliers_subjectNotFound surfaces the people sentinel unchanged.
func TestOutliers_subjectNotFound(t *testing.T) {
	t.Parallel()
	svc := outliers.New(outliers.Config{
		Faces:  fakeFaces{},
		People: fakePeople{err: people.ErrSubjectNotFound},
	})
	_, err := svc.Outliers(context.Background(), "su_missing")
	if !errors.Is(err, people.ErrSubjectNotFound) {
		t.Fatalf("Outliers = %v, want ErrSubjectNotFound", err)
	}
}

// TestOutliers_faceStoreError wraps and returns a face-store failure.
func TestOutliers_faceStoreError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	svc := outliers.New(outliers.Config{
		Faces:  fakeFaces{err: sentinel},
		People: fakePeople{},
	})
	_, err := svc.Outliers(context.Background(), "su_alice")
	if !errors.Is(err, sentinel) {
		t.Fatalf("Outliers = %v, want wrapped sentinel", err)
	}
}
