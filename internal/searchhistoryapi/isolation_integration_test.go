//go:build integration

package searchhistoryapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/searchhistory"
	"github.com/panbotka/kukatko/internal/searchhistoryapi"
)

// These are the HTTP-level tenant-isolation tests of the search history: A's
// member is a member of B as well, and what they search through one prefix is
// never offered back through the other.

// newIsolationServer mounts the real auth API and the search-history routes
// over the real store the way the server does — under /api/v1/p/{project} behind
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
	api := searchhistoryapi.NewAPI(searchhistoryapi.Config{
		Store:       searchhistory.NewStore(pool),
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

// historyIn lists, as client, the queries of the history through the prefix of
// which, newest first.
func historyIn(t *testing.T, f *projecttest.Fixture, client *http.Client, which projecttest.Side) []string {
	t.Helper()
	resp := f.Get(t, client, f.PathFor(which, "search-history"))
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading the history of %s: %v", which, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET history of %s = %d %s, want 200", which, resp.StatusCode, raw)
	}
	var body struct {
		Searches []searchhistory.Entry `json:"searches"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decoding the history of %s: %v", which, err)
	}
	out := make([]string, 0, len(body.Searches))
	for _, e := range body.Searches {
		out = append(out, e.Query)
	}
	return out
}

// recordIn records query as client through the prefix of which.
func recordIn(t *testing.T, f *projecttest.Fixture, client *http.Client, which projecttest.Side, query string) {
	t.Helper()
	resp := f.Send(t, client, http.MethodPost, f.PathFor(which, "search-history"), map[string]string{"query": query})
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST history in %s = %d, want 204", which, resp.StatusCode)
	}
}

// TestSearchHistory_recordedInAIsNotListedInB records one query through each
// prefix and asserts each history lists only its own, then clears A and asserts
// B's history survives.
func TestSearchHistory_recordedInAIsNotListedInB(t *testing.T) {
	server, f := newIsolationServer(t)
	client := f.ClientFor(t, server, projecttest.SideA)
	inA, inB := "hledano-v-"+f.A.Token, "hledano-v-"+f.B.Token

	recordIn(t, f, client, projecttest.SideA, inA)
	recordIn(t, f, client, projecttest.SideB, inB)

	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideB, "search-history"), inA)
	f.AssertListExcludes(t, client, f.PathFor(projecttest.SideA, "search-history"), inB)
	if got := historyIn(t, f, client, projecttest.SideA); !slices.Equal(got, []string{inA}) {
		t.Errorf("history in A = %q, want [%q]", got, inA)
	}
	if got := historyIn(t, f, client, projecttest.SideB); !slices.Equal(got, []string{inB}) {
		t.Errorf("history in B = %q, want [%q]", got, inB)
	}

	cleared := f.Send(t, client, http.MethodDelete, f.PathFor(projecttest.SideA, "search-history"), nil)
	_ = cleared.Body.Close()
	if cleared.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE history in A = %d, want 204", cleared.StatusCode)
	}
	if got := historyIn(t, f, client, projecttest.SideA); len(got) != 0 {
		t.Errorf("history in A after clearing = %q, want empty", got)
	}
	if got := historyIn(t, f, client, projecttest.SideB); !slices.Equal(got, []string{inB}) {
		t.Errorf("history in B after clearing A = %q, want [%q]", got, inB)
	}
}
