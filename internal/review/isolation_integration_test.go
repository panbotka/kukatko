//go:build integration

package review_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/review"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These are the tenant-isolation tests of the review game. Both seeded projects
// get the same scene with IDENTICAL vectors — the seeded person tagged on the
// seeded photo, plus a second photo with one unnamed face sitting in the
// uncertainty band — so the other project's face is always exactly as good a
// question as the project's own.

// plantBandCandidates gives each tenant the scene described above and returns
// the uid of the photo carrying the unnamed face, keyed by project uid.
func plantBandCandidates(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[string]string {
	t.Helper()
	faces := vectors.NewStore(pool)
	out := make(map[string]string, 2)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		subject, marker := tn.SubjectUID, tn.MarkerUID
		if err := faces.SaveFaces(t.Context(), tn.PhotoUID, []vectors.Face{{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 1}), BBox: [4]float64{0.25, 0.25, 0.2, 0.2},
			DetScore: 0.95, SubjectUID: &subject, MarkerUID: &marker,
		}}); err != nil {
			t.Fatalf("SaveFaces exemplar %s: %v", tn.Slug, err)
		}
		uid := projecttest.MissingUID("ph")
		sum := sha256.Sum256([]byte(uid))
		if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path,
			file_name, file_size, file_mime, file_width, file_height, taken_at)
			VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 1000, 800, '2026-06-02T12:00:00Z')`,
			uid, tn.ProjectUID, hex.EncodeToString(sum[:]), "2026/10/"+uid+".jpg", uid+".jpg"); err != nil {
			t.Fatalf("inserting a photo into %s: %v", tn.Slug, err)
		}
		if err := faces.SaveFaces(t.Context(), uid, []vectors.Face{{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 0.6, 2: 0.8}), BBox: reviewableBox, DetScore: 0.9,
		}}); err != nil {
			t.Fatalf("SaveFaces candidate %s: %v", tn.Slug, err)
		}
		out[tn.ProjectUID] = uid
	}
	return out
}

// isolationService composes the review game over the real stores of pool,
// asking about faces and outliers only.
func isolationService(pool *pgxpool.Pool) *review.Service {
	faces := vectors.NewStore(pool)
	peopleStore := people.NewStore(pool)
	photoStore := photos.NewStore(pool)
	feedbackStore := feedback.NewStore(pool)
	organizeStore := organize.NewStore(pool)
	cand := candidates.New(candidates.Config{
		Faces: faces, People: peopleStore, Feedback: feedbackStore, Photos: photoStore,
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
	})
	return review.New(review.Config{
		Sweeper: sweep.New(sweep.Config{Subjects: peopleStore, Finder: cand, Concurrency: 2}),
		Expander: expand.New(expand.Config{
			Vectors: faces, Organize: organizeStore, Feedback: feedbackStore, Photos: photoStore,
			Media: mediaurl.NewBuilder(nil),
		}),
		Organize: organizeStore,
		Faces:    faces,
		Feedback: feedbackStore,
		Assigner: facematch.New(facematch.Config{Photos: photoStore, Faces: faces, People: peopleStore}),
		Scope:    review.NewScopeStore(pool),
		Outliers: outliers.New(outliers.Config{Faces: faces, People: peopleStore, Feedback: feedbackStore}),
		Subjects: peopleStore,
		Photos:   photoStore,
		KindShares: map[review.Kind]float64{
			review.KindFace: 1, review.KindOutlier: 1,
		},
		BandMin: 0.45, BandMax: 0.75,
	})
}

// TestQueue_neverCrossesProjects builds the same player's queue in each project
// and asserts every question is about the project's own photos and people —
// A's queue never holds B's face, nor B's queue A's — and that each project's
// band candidate is asked about in its own queue.
func TestQueue_neverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	candidatesOf := plantBandCandidates(t, pool, f)
	svc := isolationService(pool)

	queue := func(projectUID string) ([]string, error) {
		res, err := svc.Queue(t.Context(), projectUID, f.A.UserUID, review.SourcePeople, 100)
		var out []string
		for _, q := range res.Questions {
			out = append(out, q.Photo.UID)
			if q.Subject != nil {
				out = append(out, q.Subject.UID)
			}
		}
		return out, err
	}
	f.AssertStoreScoped(t, queue)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		res, err := svc.Queue(t.Context(), tn.ProjectUID, f.A.UserUID, review.SourcePeople, 100)
		if err != nil {
			t.Fatalf("Queue in %s: %v", tn.Slug, err)
		}
		own := map[string]bool{tn.PhotoUID: true, candidatesOf[tn.ProjectUID]: true}
		asked := false
		for _, q := range res.Questions {
			if !own[q.Photo.UID] || q.Photo.ProjectID != tn.ProjectUID {
				t.Errorf("queue in %s asks about photo %s of project %s", tn.Slug, q.Photo.UID, q.Photo.ProjectID)
			}
			if q.Subject != nil && q.Subject.UID != tn.SubjectUID {
				t.Errorf("queue in %s asks about person %s", tn.Slug, q.Subject.UID)
			}
			asked = asked || q.Photo.UID == candidatesOf[tn.ProjectUID]
		}
		if !asked {
			t.Errorf("queue in %s = %+v, want its own band candidate asked about", tn.Slug, res.Questions)
		}
	}
	if _, err := svc.Queue(t.Context(), "", f.A.UserUID, review.SourcePeople, 100); !errors.Is(err, review.ErrNoProject) {
		t.Errorf("Queue without a project = %v, want review.ErrNoProject", err)
	}
}
