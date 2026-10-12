//go:build integration

package sweep_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/candidates"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/sweep"
	"github.com/panbotka/kukatko/internal/vectors"
)

// The tenant-isolation test of the recognition sweep: both seeded projects get
// the same scene with IDENTICAL vectors — the seeded person tagged on the seeded
// photo, plus a second photo with one unnamed look-alike face — so a scan that
// ignored the project would walk the other project's person and offer the other
// project's face.

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

// TestScan_windowNeverCrossesProjects scans each project's subject window and
// expects exactly its own person with its own look-alike — never the other
// project's, though both sit at the same distance.
func TestScan_windowNeverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	faces := vectors.NewStore(pool)
	lookalikes := make(map[string]string, 2)
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		subject, marker := tn.SubjectUID, tn.MarkerUID
		if err := faces.SaveFaces(t.Context(), tn.PhotoUID, []vectors.Face{{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 1}), BBox: [4]float64{0.25, 0.25, 0.2, 0.2},
			DetScore: 0.9, Model: "buffalo_l", SubjectUID: &subject, MarkerUID: &marker,
		}}); err != nil {
			t.Fatalf("SaveFaces exemplar %s: %v", side, err)
		}
		other := insertIsolationPhoto(t, pool, tn.ProjectUID)
		if err := faces.SaveFaces(t.Context(), other, []vectors.Face{{
			FaceIndex: 0, Vector: vec(map[int]float32{0: 0.8, 1: 0.6}), BBox: reviewableBox,
			DetScore: 0.9, Model: "buffalo_l",
		}}); err != nil {
			t.Fatalf("SaveFaces look-alike %s: %v", side, err)
		}
		lookalikes[tn.ProjectUID] = other
	}
	peopleStore := people.NewStore(pool)
	svc := sweep.New(sweep.Config{
		Subjects: peopleStore,
		Finder: candidates.New(candidates.Config{
			Faces: faces, People: peopleStore, Feedback: feedback.NewStore(pool), Photos: photos.NewStore(pool),
			Media:       mediaurl.NewBuilder(nil),
			MaxDistance: 0.5, SearchLimit: 1000, MinFacePx: 32, Concurrency: 2, MinFaceRel: 0.02,
		}),
		Concurrency: 2,
	})

	scan := func(projectUID string) ([]string, error) {
		var seen []string
		cov, err := svc.Scan(t.Context(), projectUID, sweep.Params{}, sweep.Window{Budget: 10},
			func(person *sweep.Person) (bool, error) {
				seen = append(seen, person.Subject.UID)
				for _, c := range person.Candidates {
					seen = append(seen, c.Photo.UID)
				}
				return false, nil
			})
		if err == nil && cov.SubjectsTotal != 1 {
			t.Errorf("scan in %s counted %d subjects, want only its own 1", projectUID, cov.SubjectsTotal)
		}
		return seen, err
	}
	f.AssertStoreScoped(t, scan)
	for _, tn := range []projecttest.Tenant{f.A, f.B} {
		got, err := scan(tn.ProjectUID)
		if err != nil {
			t.Fatalf("Scan in %s: %v", tn.Slug, err)
		}
		if want := []string{tn.SubjectUID, lookalikes[tn.ProjectUID]}; len(got) != 2 || got[0] != want[0] ||
			got[1] != want[1] {
			t.Errorf("scan in %s saw %v, want exactly %v", tn.Slug, got, want)
		}
	}
}
