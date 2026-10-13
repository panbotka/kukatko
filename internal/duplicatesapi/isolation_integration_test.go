//go:build integration

package duplicatesapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/duplicatesapi"
	"github.com/panbotka/kukatko/internal/dupmerge"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// newIsolationServer mounts the real auth API and the duplicates routes — the
// real detector and the real merge — under /api/v1/p/{project} the way the
// server does.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	photoStore := photos.NewStore(pool)
	svc := duplicates.New(duplicates.Config{
		Photos: photoStore, Phashes: photoStore, Embeddings: vectors.NewStore(pool),
		Feedback: feedback.NewStore(pool), PhashMaxDiff: 8, EmbeddingMaxDist: 0.05,
	})
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := duplicatesapi.NewAPI(duplicatesapi.Config{
		Service: svc, Merge: dupmerge.NewService(pool), RequireWrite: authAPI.RequireWrite,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// plantTwins gives every tenant's seeded photo a twin in the same project and
// all four photos the SAME perceptual hash, so by pHash alone every photo is a
// perfect duplicate of every other, across the project line too. It returns the
// twin per side.
func plantTwins(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[projecttest.Side]string {
	t.Helper()
	store := photos.NewStore(pool)
	out := map[projecttest.Side]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		twin := projecttest.MissingUID("ph")
		sum := sha256.Sum256([]byte(twin))
		if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
			file_size, file_mime, file_width, file_height, taken_at)
			VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-02T12:00:00Z')`,
			twin, tn.ProjectUID, hex.EncodeToString(sum[:]), "2026/10/"+twin+".jpg", twin+".jpg"); err != nil {
			t.Fatalf("inserting the twin into %s: %v", side, err)
		}
		for _, uid := range []string{tn.PhotoUID, twin} {
			if err := store.SetPhash(t.Context(), photos.Phash{PhotoUID: uid, Phash: 0x0F0F, Dhash: 0x0F0F}); err != nil {
				t.Fatalf("SetPhash(%s): %v", uid, err)
			}
		}
		out[side] = twin
	}
	return out
}

// TestDuplicates_neverCrossProjects lists B's duplicate groups — which must hold
// B's pair and nothing of A's, though A's photos carry the identical hash — and
// then asks B to merge a group mixing B's photo with A's: refused with the very
// 404 a missing uid gets, and neither photo changes.
func TestDuplicates_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	twins := plantTwins(t, pool, f)
	server := newIsolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)

	foreign := append(f.A.UIDs(), twins[projecttest.SideA])
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "duplicates"), foreign...)
	resp := f.Get(t, client, f.PathFor(projecttest.SideB, "duplicates"))
	if body := readAll(t, resp); !containsAll(body, f.B.PhotoUID, twins[projecttest.SideB]) {
		t.Errorf("B's duplicates do not name B's own pair; body %s", body)
	}

	merge := func(other string) *http.Response {
		return f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "duplicates/merge"),
			map[string]any{"keeper_uid": f.B.PhotoUID, "member_uids": []string{f.B.PhotoUID, other}})
	}
	respForeign := merge(f.A.PhotoUID)
	if respForeign.StatusCode != http.StatusNotFound {
		t.Errorf("merging A's photo from B = %d, want 404", respForeign.StatusCode)
	}
	projecttest.AssertSameShape(t, respForeign, merge(projecttest.MissingUID("ph")))

	store := photos.NewStore(pool)
	for _, tn := range []*projecttest.Tenant{&f.A, &f.B} {
		p, err := store.GetByUIDInProject(t.Context(), tn.ProjectUID, tn.PhotoUID)
		if err != nil {
			t.Fatalf("reloading %s: %v", tn.PhotoUID, err)
		}
		if p.ArchivedAt != nil {
			t.Errorf("photo %s was archived by a refused merge", tn.PhotoUID)
		}
	}
}

// readAll returns the in-memory body of a projecttest response.
func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	return string(body)
}

// containsAll reports whether body contains every one of the uids.
func containsAll(body string, uids ...string) bool {
	for _, uid := range uids {
		if !strings.Contains(body, uid) {
			return false
		}
	}
	return true
}
