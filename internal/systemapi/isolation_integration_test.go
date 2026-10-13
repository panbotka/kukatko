//go:build integration

package systemapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/system"
	"github.com/panbotka/kukatko/internal/systemapi"
)

// newIsolationServer mounts the real auth API and the statistics routes under
// /api/v1/p/{project} the way the server does, over a real system service.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	store := system.NewStore(pool)
	api := systemapi.NewAPI(systemapi.Config{
		Service:           system.New(system.Config{Library: store, Charts: store}),
		RequireMaintainer: authAPI.RequireMaintainer,
		RequireAuth:       authAPI.RequireAuth,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterProjectRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

// TestStats_neverCrossProjects seeds the two-tenant fixture and gives B a
// second photo — a video with a camera — so the two libraries differ, then
// reads each project's statistics and charts as its own member: each answer
// counts that project's rows only, and a member of B cannot open A's
// statistics at all.
func TestStats_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	if _, err := pool.Exec(t.Context(), `INSERT INTO photos (uid, project_id, file_hash, file_path, file_name,
		file_size, media_type, camera_make, camera_model, taken_at)
		VALUES ('pbxextra', $1, 'hash-pbxextra', 'b/pbxextra.mp4', 'pbxextra.mp4', 4096, 'video',
		'Nikon', 'Z6', '2019-05-01T12:00:00Z')`, f.B.ProjectUID); err != nil {
		t.Fatalf("seeding B's extra photo: %v", err)
	}

	server := newIsolationServer(t, pool)
	for _, tc := range []struct {
		side           projecttest.Side
		photos, videos int
		cameras        int
	}{
		{side: projecttest.SideB, photos: 2, videos: 1, cameras: 1},
		{side: projecttest.SideA, photos: 1, videos: 0, cameras: 0},
	} {
		client := f.ClientFor(t, server, tc.side)

		var stats system.Library
		statsResp := f.Get(t, client, f.PathFor(tc.side, "stats"))
		decodeJSON(t, statsResp, &stats)
		_ = statsResp.Body.Close()
		if stats.Photos != tc.photos || stats.Videos != tc.videos || stats.Labels != 1 || stats.Subjects != 1 {
			t.Errorf("%s stats = %+v, want %d photos (%d videos), 1 label, 1 subject",
				tc.side, stats, tc.photos, tc.videos)
		}
		var charts system.Charts
		chartsResp := f.Get(t, client, f.PathFor(tc.side, "stats/charts"))
		decodeJSON(t, chartsResp, &charts)
		_ = chartsResp.Body.Close()
		if len(charts.TopCameras) != tc.cameras {
			t.Errorf("%s top cameras = %+v, want %d", tc.side, charts.TopCameras, tc.cameras)
		}

		other := tc.side.Other()
		f.AssertInvisible(t, client, f.PathFor(other, "stats"))
		f.AssertInvisible(t, client, f.PathFor(other, "stats/charts"))
	}
}

// decodeJSON decodes a projecttest response's 200 body into out.
func decodeJSON(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}
}
