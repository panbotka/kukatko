//go:build integration

package expandapi_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/expandapi"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// newExpandServer mounts the real auth API and the expansion routes, over the
// real expand service, under /api/v1/p/{project} the way the server does.
func newExpandServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	svc := expand.New(expand.Config{
		Vectors: vectors.NewStore(pool), Organize: organize.NewStore(pool),
		Feedback: feedback.NewStore(pool), Photos: photos.NewStore(pool),
		Media: mediaurl.NewBuilder(nil), MaxDistance: 0.5,
	})
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := expandapi.NewAPI(expandapi.Config{Service: svc, RequireCurator: authAPI.RequireCurator})
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

// imageVec returns an ImageDim vector from index→value overrides.
func imageVec(set map[int]float32) []float32 {
	v := make([]float32, vectors.ImageDim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// plantLookalikes embeds every tenant's seeded photo (on its album and label)
// at e0 and gives it a look-alike photo with an IDENTICAL vector in both
// projects, returning the look-alike per side.
func plantLookalikes(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[projecttest.Side]string {
	t.Helper()
	store := vectors.NewStore(pool)
	out := map[projecttest.Side]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		lookalike := projecttest.MissingUID("ph")
		sum := sha256.Sum256([]byte(lookalike))
		if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
			file_size, file_mime, file_width, file_height, taken_at)
			VALUES ($1, $2, $3, $4, $5, 1024, 'image/jpeg', 640, 480, '2026-06-02T12:00:00Z')`,
			lookalike, tn.ProjectUID, hex.EncodeToString(sum[:]), "2026/10/"+lookalike+".jpg",
			lookalike+".jpg"); err != nil {
			t.Fatalf("inserting the look-alike into %s: %v", side, err)
		}
		vecs := map[string][]float32{
			tn.PhotoUID: imageVec(map[int]float32{0: 1}), lookalike: imageVec(map[int]float32{0: 0.8, 1: 0.6}),
		}
		for uid, vec := range vecs {
			if _, err := store.SaveEmbedding(t.Context(), vectors.Embedding{
				PhotoUID: uid, Vector: vec, Model: "clip", Pretrained: "test",
			}); err != nil {
				t.Fatalf("SaveEmbedding(%s): %v", uid, err)
			}
		}
		out[side] = lookalike
	}
	return out
}

// TestSimilarRoutes_neverCrossProjects asks B's label and album for similar
// photos and expects B's look-alike but none of A's photos, though A's
// look-alike sits at the identical distance; asking about A's album or label
// from B answers the very 404 a missing uid gets.
func TestSimilarRoutes_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	lookalikes := plantLookalikes(t, pool, f)
	server := newExpandServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)

	foreign := append(f.A.UIDs(), lookalikes[projecttest.SideA])
	for _, route := range []string{"labels/" + f.B.LabelUID + "/similar", "albums/" + f.B.AlbumUID + "/similar"} {
		path := f.PathFor(projecttest.SideB, route)
		f.AssertListExcludes(t, client, path, foreign...)
		resp := f.Get(t, client, path)
		assertCandidates(t, resp, lookalikes[projecttest.SideB])
		_ = resp.Body.Close()
	}

	for _, kind := range []struct{ route, foreign, prefix string }{
		{"labels", f.A.LabelUID, "lb"},
		{"albums", f.A.AlbumUID, "al"},
	} {
		respForeign := f.Get(t, client, f.PathFor(projecttest.SideB, kind.route+"/"+kind.foreign+"/similar"))
		defer func() { _ = respForeign.Body.Close() }() // in memory; AssertSameShape reads it
		if respForeign.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s of A from B = %d, want 404", kind.route, respForeign.StatusCode)
		}
		missing := projecttest.MissingUID(kind.prefix)
		respMissing := f.Get(t, client, f.PathFor(projecttest.SideB, kind.route+"/"+missing+"/similar"))
		defer func() { _ = respMissing.Body.Close() }()
		projecttest.AssertSameShape(t, respForeign, respMissing)
	}
}

// assertCandidates fails t unless resp is a 200 expansion result whose
// candidates are exactly want.
func assertCandidates(t *testing.T, resp *http.Response, want ...string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the result: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", resp.StatusCode, body)
	}
	var result expand.Result
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decoding the result: %v", err)
	}
	got := make([]string, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		got = append(got, c.Photo.UID)
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidates = %v, want %v", got, want)
		}
	}
}
