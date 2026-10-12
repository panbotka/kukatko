//go:build integration

package savedsearchapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/savedsearch"
	"github.com/panbotka/kukatko/internal/savedsearchapi"
)

// These are the HTTP-level tenant-isolation tests of the saved searches: A's
// member is a member of B as well, and the search they saved in B is, through
// A's prefix, exactly a search that does not exist.

// newIsolationServer mounts the real auth API and the saved-search routes over
// the real store the way the server does — under /api/v1/p/{project} behind
// auth.WithProject — and makes A's member an approved viewer of B too.
func newIsolationServer(t *testing.T) (*httptest.Server, *projecttest.Fixture) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	projects := project.NewStore(pool)
	if _, err := projects.AddMember(t.Context(), audit.Meta{}, f.B.ProjectUID, f.A.UserUID,
		project.RoleViewer, true); err != nil {
		t.Fatalf("adding A's member to B: %v", err)
	}

	svc := auth.NewService(auth.NewStore(pool), auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: auth.NewLimiter(100, time.Minute)})
	api := savedsearchapi.NewAPI(savedsearchapi.Config{
		Store:       savedsearch.NewStore(pool),
		RequireAuth: authAPI.RequireAuth,
	})
	r := chi.NewRouter()
	r.Route(auth.APIBasePath, func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		r.Route("/p/{"+auth.ProjectURLParam+"}", func(r chi.Router) {
			r.Use(auth.WithProject(projects))
			api.RegisterRoutes(r)
		})
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server, f
}

// TestSavedSearch_sameUserOtherProjectIsMissing saves a search in B as A's
// member and asserts that, through A's prefix, reading, editing and deleting it
// are each the very 404 a missing uid gets, that A's list does not name it, and
// that it is unchanged in B afterwards.
func TestSavedSearch_sameUserOtherProjectIsMissing(t *testing.T) {
	server, f := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideA)

	created := f.Send(t, client, http.MethodPost, f.PathFor(projecttest.SideB, "saved-searches"),
		map[string]any{"name": "Jen v B", "params": map[string]string{"q": "b"}})
	defer func() { _ = created.Body.Close() }()
	var inB searchView
	decodeBody(t, created, &inB)
	if created.StatusCode != http.StatusCreated || inB.UID == "" {
		t.Fatalf("create in B = %d %+v, want 201", created.StatusCode, inB)
	}

	foreignPath := f.PathFor(projecttest.SideA, "saved-searches/"+inB.UID)
	missingPath := f.PathFor(projecttest.SideA, "saved-searches/"+projecttest.MissingUID("ss"))
	f.AssertInvisible(t, client, foreignPath)
	for _, tc := range []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPatch, map[string]string{"name": "Uneseno"}},
		{http.MethodDelete, nil},
	} {
		foreign := f.Send(t, client, tc.method, foreignPath, tc.body)
		missing := f.Send(t, client, tc.method, missingPath, tc.body)
		if foreign.StatusCode != http.StatusNotFound {
			t.Errorf("%s of B's search through A = %d, want 404", tc.method, foreign.StatusCode)
		}
		projecttest.AssertSameShape(t, foreign, missing)
		_, _ = foreign.Body.Close(), missing.Body.Close()
	}
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideA, "saved-searches"), inB.UID, f.B.SavedSearchUID)

	survived := f.Get(t, client, f.PathFor(projecttest.SideB, "saved-searches/"+inB.UID))
	defer func() { _ = survived.Body.Close() }()
	var got searchView
	decodeBody(t, survived, &got)
	if survived.StatusCode != http.StatusOK || got.Name != "Jen v B" {
		t.Fatalf("B's search after A's attempts = %d %+v, want 200 and unchanged", survived.StatusCode, got)
	}
}

// TestSavedSearch_listIsPerProject asserts the list through each prefix names
// only the caller's search of that project.
func TestSavedSearch_listIsPerProject(t *testing.T) {
	server, f := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideA)

	resp := f.Get(t, client, f.PathFor(projecttest.SideA, "saved-searches"))
	_ = resp.Body.Close() // Get buffered the body; decodeBody reads the copy.
	var list struct {
		SavedSearches []searchView `json:"saved_searches"`
	}
	decodeBody(t, resp, &list)
	if len(list.SavedSearches) != 1 || list.SavedSearches[0].UID != f.A.SavedSearchUID {
		t.Fatalf("A's list = %+v, want only %s", list.SavedSearches, f.A.SavedSearchUID)
	}

	// A's member is in B too but owns nothing there; B's member's search is not
	// theirs to see either.
	resp = f.Get(t, client, f.PathFor(projecttest.SideB, "saved-searches"))
	_ = resp.Body.Close()
	var inB struct {
		SavedSearches []searchView `json:"saved_searches"`
	}
	decodeBody(t, resp, &inB)
	if resp.StatusCode != http.StatusOK || len(inB.SavedSearches) != 0 {
		t.Fatalf("A's member's list in B = %d %+v, want 200 and empty", resp.StatusCode, inB.SavedSearches)
	}
}
