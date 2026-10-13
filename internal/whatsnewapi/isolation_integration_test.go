//go:build integration

package whatsnewapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/whatsnew"
	"github.com/panbotka/kukatko/internal/whatsnewapi"
)

// TestWhatsNew_countsOnlyTheProject opens a visit for both members, then lets
// project B receive an upload, an album, a named person and a comment while
// both are away. A's digest must stay silent — none of it is A's news — and B's
// must report exactly what arrived in B. The visit bookkeeping lives on each
// membership, so the rotation in one project does not move the other's.
func TestWhatsNew_countsOnlyTheProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)

	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: 24 * time.Hour, MaxLifetime: 48 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := whatsnewapi.NewAPI(whatsnewapi.Config{
		Store:       whatsnew.NewStore(pool, phototask.NewStore(pool)).WithGap(testGap),
		RequireAuth: authAPI.RequireAuth,
		Now:         func() time.Time { return now },
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

	clients := map[projecttest.Side]*http.Client{
		projecttest.SideA: f.ClientFor(t, server, projecttest.SideA),
		projecttest.SideB: f.ClientFor(t, server, projecttest.SideB),
	}
	digest := func(side projecttest.Side) whatsnew.Summary {
		t.Helper()
		resp := f.Get(t, clients[side], f.PathFor(side, "whats-new"))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET whats-new in %s = %d, want 200", side, resp.StatusCode)
		}
		var got whatsnew.Summary
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decoding the digest of %s: %v", side, err)
		}
		return got
	}

	// The first read of each membership has no reference point.
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		if got := digest(side); got.HasNews {
			t.Fatalf("first read in %s = %+v, want has_news false", side, got)
		}
	}
	now = now.Add(2 * testGap)

	at := now.Add(-time.Second)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}
	exec(`INSERT INTO photos (uid, project_id, file_hash, file_path, created_at, updated_at)
	      VALUES ('phnewinb', $1, 'hash-new-in-b', 'new-in-b.jpg', $2, $2)`, f.B.ProjectUID, at)
	exec(`INSERT INTO albums (uid, project_id, slug, title, type, created_at, updated_at)
	      VALUES ('alnewinb', $1, 'alnewinb', 'Nové v B', 'album', $2, $2)`, f.B.ProjectUID, at)
	exec(`INSERT INTO subjects (uid, project_id, slug, name, type, created_at, updated_at)
	      VALUES ('sunewinb', $1, 'sunewinb', 'Nový v B', 'person', $2, $2)`, f.B.ProjectUID, at)
	exec(`INSERT INTO comments (uid, photo_uid, body, created_at) VALUES ('cmnewinb', 'phnewinb', 'kdo?', $1)`, at)

	if got := digest(projecttest.SideA); got.HasNews {
		t.Errorf("A's digest = %+v, want has_news false — everything new happened in B", got)
	}
	got := digest(projecttest.SideB)
	if !got.HasNews || got.Photos != 1 || got.AlbumCount != 1 || got.PersonCount != 1 || got.Comments != 1 {
		t.Errorf("B's digest = %+v, want 1 photo, 1 album, 1 person, 1 comment", got)
	}
}
