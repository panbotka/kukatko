//go:build integration

package candidates_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These are the tenant-isolation tests of the candidate search. Both seeded
// projects get the same scene with IDENTICAL vectors — the seeded person tagged
// on the seeded photo (the exemplar), plus a second photo with one unnamed
// look-alike face — so the other project's unnamed face is always exactly as
// close as the project's own: a search that ignored the project would find it.

// isolationSides are the two seeded projects, in order.
var isolationSides = []projecttest.Side{projecttest.SideA, projecttest.SideB}

// insertIsolationPhoto inserts one more 640x480 photo into the project and
// returns its uid.
func insertIsolationPhoto(t *testing.T, pool *pgxpool.Pool, projectUID string) string {
	t.Helper()
	uid := projecttest.MissingUID("ph")
	sum := sha256.Sum256([]byte(uid))
	if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
		file_size, file_mime, file_width, file_height, taken_at)
		VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-02T12:00:00Z')`,
		uid, projectUID, hex.EncodeToString(sum[:]), "2026/10/"+uid+".jpg", uid+".jpg"); err != nil {
		t.Fatalf("inserting a photo into %s: %v", projectUID, err)
	}
	return uid
}

// plantLookalikes gives each tenant the scene described above and returns, per
// side, the photo carrying the unnamed look-alike face.
func plantLookalikes(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[projecttest.Side]string {
	t.Helper()
	store := vectors.NewStore(pool)
	out := make(map[projecttest.Side]string, len(isolationSides))
	for _, side := range isolationSides {
		tn := f.Tenant(side)
		subject, marker := tn.SubjectUID, tn.MarkerUID
		exemplar := vectors.Face{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 1}), BBox: [4]float64{0.25, 0.25, 0.2, 0.2},
			DetScore: 0.9, Model: "buffalo_l", SubjectUID: &subject, MarkerUID: &marker,
		}
		if err := store.SaveFaces(t.Context(), tn.PhotoUID, []vectors.Face{exemplar}); err != nil {
			t.Fatalf("SaveFaces exemplar %s: %v", side, err)
		}
		other := insertIsolationPhoto(t, pool, tn.ProjectUID)
		lookalike := vectors.Face{
			FaceIndex: 0, Vector: nearE0(), BBox: reviewableBox, DetScore: 0.9, Model: "buffalo_l",
		}
		if err := store.SaveFaces(t.Context(), other, []vectors.Face{lookalike}); err != nil {
			t.Fatalf("SaveFaces look-alike %s: %v", side, err)
		}
		out[side] = other
	}
	return out
}

// TestFind_neverCrossesProjects searches for each project's person inside its
// own project and expects exactly that project's look-alike; searching for a
// person of the other project is the same not-found as a missing one.
func TestFind_neverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	lookalikes := plantLookalikes(t, pool, f)
	svc := candidates.New(candidates.Config{
		Faces: vectors.NewStore(pool), People: people.NewStore(pool),
		Feedback: feedback.NewStore(pool), Photos: photos.NewStore(pool),
		Media:       mediaurl.NewBuilder(nil),
		MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
	})

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		subject := f.A.SubjectUID
		if projectUID == f.B.ProjectUID {
			subject = f.B.SubjectUID
		}
		res, err := svc.Find(t.Context(), projectUID, subject, candidates.Request{})
		out := make([]string, 0, len(res.Candidates))
		for _, c := range res.Candidates {
			out = append(out, c.Photo.UID)
		}
		return out, err
	})
	for _, side := range isolationSides {
		tn := f.Tenant(side)
		res, err := svc.Find(t.Context(), tn.ProjectUID, tn.SubjectUID, candidates.Request{})
		if err != nil {
			t.Fatalf("Find in %s: %v", side, err)
		}
		if len(res.Candidates) != 1 || res.Candidates[0].Photo.UID != lookalikes[side] {
			t.Errorf("candidates in %s = %+v, want only the project's look-alike %s", side, res.Candidates, lookalikes[side])
		}
		if res.Candidates[0].Photo.ProjectID != tn.ProjectUID {
			t.Errorf("candidate photo in %s belongs to %s", side, res.Candidates[0].Photo.ProjectID)
		}
	}

	if _, err := svc.Find(t.Context(), f.B.ProjectUID, f.A.SubjectUID, candidates.Request{}); !errors.Is(
		err, people.ErrSubjectNotFound) {
		t.Errorf("Find of A's person inside B = %v, want people.ErrSubjectNotFound", err)
	}
	if _, err := svc.Find(t.Context(), "", f.A.SubjectUID, candidates.Request{}); !errors.Is(err, people.ErrNoProject) {
		t.Errorf("Find without a project = %v, want people.ErrNoProject", err)
	}
}
