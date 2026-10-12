//go:build integration

package expand_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These are the tenant-isolation tests of the collection expansion. Both seeded
// projects get the same scene with IDENTICAL vectors — the seeded photo (on the
// seeded album and label) embedded at e0, plus a look-alike photo next to it —
// so the other project's look-alike is always exactly as close as the
// project's own: a search that ignored the project would find it.

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

// plantLookalikes embeds every tenant's seeded photo at e0 and gives it a
// look-alike photo at nearE0, returning the look-alike per project uid.
func plantLookalikes(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[string]string {
	t.Helper()
	store := vectors.NewStore(pool)
	out := map[string]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		lookalike := insertIsolationPhoto(t, pool, tn.ProjectUID)
		for uid, vec := range map[string][]float32{tn.PhotoUID: imgVec(map[int]float32{0: 1}), lookalike: nearE0()} {
			if _, err := store.SaveEmbedding(t.Context(), vectors.Embedding{
				PhotoUID: uid, Vector: vec, Model: "clip", Pretrained: "test",
			}); err != nil {
				t.Fatalf("SaveEmbedding(%s): %v", uid, err)
			}
		}
		out[tn.ProjectUID] = lookalike
	}
	return out
}

// TestExpand_neverCrossesProjects expands each project's album and label inside
// its own project and expects exactly that project's look-alike; expanding a
// collection of the other project is the same not-found as a missing one.
func TestExpand_neverCrossesProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	lookalikes := plantLookalikes(t, pool, f)
	svc := newService(pool, 0)

	collections := map[string]struct{ album, label string }{
		f.A.ProjectUID: {f.A.AlbumUID, f.A.LabelUID},
		f.B.ProjectUID: {f.B.AlbumUID, f.B.LabelUID},
	}
	for _, kind := range []string{expand.KindAlbum, expand.KindLabel} {
		f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
			search, uid := svc.Label, collections[projectUID].label
			if kind == expand.KindAlbum {
				search, uid = svc.Album, collections[projectUID].album
			}
			res, err := search(t.Context(), projectUID, uid, expand.Request{})
			out := make([]string, 0, len(res.Candidates))
			for _, c := range res.Candidates {
				out = append(out, c.Photo.UID)
				if c.Photo.ProjectID != projectUID {
					t.Errorf("%s similar in %s returned %s of project %s", kind, projectUID, c.Photo.UID, c.Photo.ProjectID)
				}
			}
			if len(out) != 1 || out[0] != lookalikes[projectUID] {
				t.Errorf("%s similar in %s = %v, want only the project's look-alike %s",
					kind, projectUID, out, lookalikes[projectUID])
			}
			return out, err
		})
	}

	if _, err := svc.Album(t.Context(), f.B.ProjectUID, f.A.AlbumUID, expand.Request{}); !errors.Is(
		err, organize.ErrAlbumNotFound) {
		t.Errorf("Album of A inside B = %v, want organize.ErrAlbumNotFound", err)
	}
	if _, err := svc.Label(t.Context(), f.B.ProjectUID, f.A.LabelUID, expand.Request{}); !errors.Is(
		err, organize.ErrLabelNotFound) {
		t.Errorf("Label of A inside B = %v, want organize.ErrLabelNotFound", err)
	}
	if _, err := svc.Label(t.Context(), "", f.A.LabelUID, expand.Request{}); !errors.Is(err, expand.ErrNoProject) {
		t.Errorf("Label without a project = %v, want expand.ErrNoProject", err)
	}
}
