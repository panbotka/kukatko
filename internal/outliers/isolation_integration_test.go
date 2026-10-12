//go:build integration

package outliers_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestOutliers_neverCrossesProjects gives each project's seeded person one
// face with an IDENTICAL vector and ranks them: each project sees only its own
// person's face, and asking one project about the other's person is the same
// not-found as asking about nobody.
func TestOutliers_neverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	faces := vectors.NewStore(pool)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		subject, marker := tn.SubjectUID, tn.MarkerUID
		if err := faces.SaveFaces(t.Context(), tn.PhotoUID, []vectors.Face{{
			FaceIndex: 0, Vector: faceVec(map[int]float32{0: 1}), BBox: [4]float64{0.25, 0.25, 0.2, 0.2},
			DetScore: 0.9, Model: "buffalo_l", SubjectUID: &subject, MarkerUID: &marker,
		}}); err != nil {
			t.Fatalf("SaveFaces %s: %v", tn.Slug, err)
		}
	}
	svc := outliers.New(outliers.Config{
		Faces: faces, People: people.NewStore(pool), Feedback: feedback.NewStore(pool),
	})

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		subject := f.A.SubjectUID
		if projectUID == f.B.ProjectUID {
			subject = f.B.SubjectUID
		}
		res, err := svc.Outliers(t.Context(), projectUID, subject, outliers.Options{})
		out := make([]string, 0, len(res.Faces))
		for _, face := range res.Faces {
			out = append(out, face.PhotoUID)
		}
		return out, err
	})
	if _, err := svc.Outliers(t.Context(), f.B.ProjectUID, f.A.SubjectUID, outliers.Options{}); !errors.Is(
		err, people.ErrSubjectNotFound) {
		t.Errorf("outliers of A's person inside B = %v, want people.ErrSubjectNotFound", err)
	}
	if _, err := svc.Outliers(t.Context(), "", f.A.SubjectUID, outliers.Options{}); !errors.Is(
		err, people.ErrNoProject) {
		t.Errorf("outliers without a project = %v, want people.ErrNoProject", err)
	}
}
