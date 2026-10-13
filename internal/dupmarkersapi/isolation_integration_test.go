//go:build integration

package dupmarkersapi_test

import (
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
	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/dupmarkersapi"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// newIsolationServer mounts the real auth API and the repeated-marker routes
// under /api/v1/p/{project} the way the server does.
func newIsolationServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	peopleStore := people.NewStore(pool)
	authSvc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := dupmarkersapi.NewAPI(dupmarkersapi.Config{
		Service: dupmarkers.New(dupmarkers.Config{
			Markers: dupmarkers.NewStore(pool), Dismissals: feedback.NewStore(pool),
		}),
		Markers: peopleStore,
		Assigner: facematch.New(facematch.Config{
			Photos: photos.NewStore(pool), People: peopleStore, Faces: vectors.NewStore(pool),
		}),
		RequireAuth:    authAPI.RequireAuth,
		RequireCurator: authAPI.RequireCurator,
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

// TestDuplicateMarkers_neverCrossProjects marks each tenant's person a second
// time on the tenant's own photo, so both projects hold one finding, and
// asserts B's listing names B's finding and nothing of A's.
func TestDuplicateMarkers_neverCrossProjects(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	second := map[projecttest.Side]string{}
	for _, side := range []projecttest.Side{projecttest.SideA, projecttest.SideB} {
		tn := f.Tenant(side)
		second[side] = projecttest.MissingUID("mk")
		if _, err := pool.Exec(t.Context(), `INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
			VALUES ($1, $2, $3, 'face', 0.6, 0.25, 0.2, 0.2)`,
			second[side], tn.PhotoUID, tn.SubjectUID); err != nil {
			t.Fatalf("marking %s's person twice: %v", side, err)
		}
	}
	server := newIsolationServer(t, pool)
	client := f.ClientFor(t, server, projecttest.SideB)
	path := f.PathFor(projecttest.SideB, "duplicate-markers")

	f.AssertListExcludes(t, client, path, append(f.A.UIDs(), second[projecttest.SideA])...)
	resp := f.Get(t, client, path)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the listing: %v", err)
	}
	for _, uid := range []string{f.B.PhotoUID, f.B.MarkerUID, second[projecttest.SideB]} {
		if !strings.Contains(string(body), uid) {
			t.Errorf("B's listing does not name B's own %s; body %s", uid, body)
		}
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
