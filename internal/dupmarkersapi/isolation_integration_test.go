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

	"github.com/panbotka/kukatko/internal/audit/audittest"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/dupmarkers"
	"github.com/panbotka/kukatko/internal/dupmarkersapi"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/feedback"
	"github.com/panbotka/kukatko/internal/feedbackapi"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/vectors"
)

// newIsolationServer mounts the real auth API, the repeated-marker routes and
// the feedback routes that settle a group under /api/v1/p/{project} the way the
// server does.
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
	feedbackAPI := feedbackapi.NewAPI(feedbackapi.Config{
		Store: feedback.NewStore(pool), RequireCurator: authAPI.RequireCurator, RequireWrite: authAPI.RequireWrite,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(project.NewStore(pool)))
			api.RegisterRoutes(r)
			feedbackAPI.RegisterRoutes(r)
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
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	second := markTwice(t, pool, f)
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

// markTwice marks each tenant's person a second time on the tenant's own photo,
// so each project holds one repeated-marker finding, and returns the second
// marker's uid per side.
func markTwice(t *testing.T, pool *pgxpool.Pool, f *projecttest.Fixture) map[projecttest.Side]string {
	t.Helper()
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
	return second
}

// TestDuplicateMarkerDismissals_foreignGroupIsMissing has A's curator dismiss
// B's repeated-marker group through A's project, and take back a dismissal B
// recorded itself: both answer the very 404 a uid that exists nowhere gets,
// nothing is written or removed, and B's review keeps its state — the group
// still listed after the foreign dismissal, still settled after the foreign
// take-back.
func TestDuplicateMarkerDismissals_foreignGroupIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	audittest.Guard(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool, projecttest.WithRole(projecttest.SideA, project.RoleCurator))
	markTwice(t, pool, f)
	server := newIsolationServer(t, pool)
	clientA := f.ClientFor(t, server, projecttest.SideA)
	clientB := f.ClientFor(t, server, projecttest.SideB)
	route := f.PathFor(projecttest.SideA, "feedback/duplicate-marker-dismissals")
	foreign := map[string]string{"photo_uid": f.B.PhotoUID, "subject_uid": f.B.SubjectUID}
	missing := map[string]string{"photo_uid": projecttest.MissingUID("ph"), "subject_uid": f.A.SubjectUID}

	sendBoth := func(method string) {
		t.Helper()
		got := f.Send(t, clientA, method, route, foreign)
		defer func() { _ = got.Body.Close() }() // in memory; AssertSameShape reads it
		if got.StatusCode != http.StatusNotFound {
			t.Errorf("%s %s about B's group = %d, want 404", method, route, got.StatusCode)
		}
		ghost := f.Send(t, clientA, method, route, missing)
		defer func() { _ = ghost.Body.Close() }()
		projecttest.AssertSameShape(t, got, ghost)
	}

	sendBoth(http.MethodPost)
	if n := countDismissals(t, pool); n != 0 {
		t.Errorf("A's foreign dismissal left %d rows", n)
	}
	if !listsPhoto(t, f, clientB, f.B.PhotoUID) {
		t.Error("B's listing lost its group after A's foreign dismissal")
	}

	resp := f.Send(t, clientB, http.MethodPost, f.PathFor(projecttest.SideB, "feedback/duplicate-marker-dismissals"),
		foreign)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("B dismissing its own group = %d, want 204", resp.StatusCode)
	}
	sendBoth(http.MethodDelete)
	if n := countDismissals(t, pool); n != 1 {
		t.Errorf("B's dismissal rows after A's foreign take-back = %d, want 1", n)
	}
	if listsPhoto(t, f, clientB, f.B.PhotoUID) {
		t.Error("B's settled group came back after A's foreign take-back")
	}
}

// countDismissals counts every repeated-marker dismissal in the instance.
func countDismissals(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM duplicate_marker_dismissals").Scan(&n); err != nil {
		t.Fatalf("counting dismissals: %v", err)
	}
	return n
}

// listsPhoto reports whether B's repeated-marker listing names photoUID.
func listsPhoto(t *testing.T, f *projecttest.Fixture, client *http.Client, photoUID string) bool {
	t.Helper()
	resp := f.Get(t, client, f.PathFor(projecttest.SideB, "duplicate-markers"))
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the listing: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("B's listing = %d, want 200; body %s", resp.StatusCode, body)
	}
	return strings.Contains(string(body), photoUID)
}
