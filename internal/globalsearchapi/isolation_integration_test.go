//go:build integration

package globalsearchapi_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/globalsearchapi"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// newIsolationEnv mounts the global search the way the server does — behind the
// real auth guard, under /api/v1/p/{project} — over the two tenants of
// projecttest.Seed, and returns the server with the fixture.
func newIsolationEnv(t *testing.T) (*httptest.Server, *projecttest.Fixture) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authSvc := auth.NewService(auth.NewStore(db.Pool()),
		auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := globalsearchapi.NewAPI(globalsearchapi.Config{
		Organizer: organize.NewStore(db.Pool()), People: people.NewStore(db.Pool()),
		Photos: photos.NewStore(db.Pool()), Limit: 10, RequireAuth: authAPI.RequireAuth,
	})
	projects := project.NewStore(db.Pool())
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterRoutes(r)
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, projecttest.Seed(t, db.Pool())
}

// TestGlobalSearch_directHitsStayInTheProject pastes A's photo uid and A's
// marker uid into B's search box: both must resolve to "no such id" — the
// answer a made-up id gets — and the body must carry none of the photo's
// details. B's own photo uid, by contrast, resolves.
func TestGlobalSearch_directHitsStayInTheProject(t *testing.T) {
	srv, f := newIsolationEnv(t)
	client := f.ClientFor(t, srv, projecttest.SideB)
	search := func(q string) string {
		return f.PathFor(projecttest.SideB, "search/global?q="+url.QueryEscape(q))
	}

	f.AssertListExcludes(t, client, search(f.A.PhotoUID), f.A.PhotoFileHash, f.A.Token)
	f.AssertListExcludes(t, client, search(f.A.MarkerUID), f.A.PhotoUID, f.A.PhotoFileHash, f.A.Token)
	assertBodyHas(t, f.Get(t, client, search(f.A.PhotoUID)), `"found":false`)
	assertBodyHas(t, f.Get(t, client, search(f.A.MarkerUID)), `"found":false`)
	assertBodyHas(t, f.Get(t, client, search(f.B.PhotoUID)), `"found":true`)
}

// TestGlobalSearch_photoGroupStaysInTheProject runs the fuzzy search over a
// word every seeded photo title carries — and only the photo titles, so the
// album, label and people groups stay empty on both sides: B's photo group
// holds B's photo and never A's.
func TestGlobalSearch_photoGroupStaysInTheProject(t *testing.T) {
	srv, f := newIsolationEnv(t)
	client := f.ClientFor(t, srv, projecttest.SideB)
	path := f.PathFor(projecttest.SideB, "search/global?q=Fotka")

	f.AssertListExcludes(t, client, path, f.A.PhotoUID, f.A.PhotoFileHash)
	assertBodyHas(t, f.Get(t, client, path), f.B.PhotoUID)
}

// assertBodyHas asserts resp is a 200 whose body contains want.
func assertBodyHas(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(want)) {
		t.Errorf("answer %d does not contain %s: %s", resp.StatusCode, want, body)
	}
}
