//go:build integration

package searchhistoryapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/searchhistory"
	"github.com/panbotka/kukatko/internal/searchhistoryapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and search-history APIs behind an httptest server over the
// integration database.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := searchhistoryapi.NewAPI(searchhistoryapi.Config{
		Store:       searchhistory.NewStore(db.Pool()),
		RequireAuth: authAPI.RequireAuth,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// mustDo issues a request with an optional JSON body and returns the response.
func (e *env) mustDo(t *testing.T, c *http.Client, method, path string, body []byte) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

// record posts one query and asserts the expected status.
func (e *env) record(t *testing.T, c *http.Client, query string, wantStatus int) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": query})
	resp := e.mustDo(t, c, http.MethodPost, "/api/v1/search-history", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != wantStatus {
		t.Fatalf("record(%q) status = %d, want %d", query, resp.StatusCode, wantStatus)
	}
}

// list returns the queries of one client's history, newest first.
func (e *env) list(t *testing.T, c *http.Client) []string {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodGet, "/api/v1/search-history", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Searches []searchhistory.Entry `json:"searches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	queries := make([]string, len(body.Searches))
	for i, entry := range body.Searches {
		if entry.SearchedAt.IsZero() {
			t.Errorf("entry %q has no searched_at", entry.Query)
		}
		queries[i] = entry.Query
	}
	return queries
}

// TestSearchHistoryRecordsNewestFirst checks that recording several queries yields
// them back most-recent-first, and that the query is stored trimmed.
func TestSearchHistoryRecordsNewestFirst(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	if got := e.list(t, client); len(got) != 0 {
		t.Fatalf("fresh history = %v, want empty", got)
	}

	for _, q := range []string{"svatba", "  person:Anna  ", `album:"Léto 2024"`} {
		e.record(t, client, q, http.StatusNoContent)
	}

	want := []string{`album:"Léto 2024"`, "person:Anna", "svatba"}
	if got := e.list(t, client); !slices.Equal(got, want) {
		t.Errorf("history = %v, want %v", got, want)
	}
}

// TestSearchHistoryDeduplicates checks that re-running a query moves it back to the
// front instead of appending a second copy.
func TestSearchHistoryDeduplicates(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	e.record(t, client, "svatba", http.StatusNoContent)
	e.record(t, client, "hory", http.StatusNoContent)
	e.record(t, client, "svatba", http.StatusNoContent)

	want := []string{"svatba", "hory"}
	if got := e.list(t, client); !slices.Equal(got, want) {
		t.Errorf("history = %v, want %v (deduplicated, re-run moved to the front)", got, want)
	}
}

// TestSearchHistoryCapsAtMaxEntries checks that the history is a fixed-size ring:
// recording more than MaxEntries queries keeps the newest MaxEntries and drops the
// oldest, without any retention job.
func TestSearchHistoryCapsAtMaxEntries(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	const extra = 5
	for i := range searchhistory.MaxEntries + extra {
		e.record(t, client, fmt.Sprintf("query %02d", i), http.StatusNoContent)
	}

	got := e.list(t, client)
	if len(got) != searchhistory.MaxEntries {
		t.Fatalf("history holds %d entries, want %d", len(got), searchhistory.MaxEntries)
	}
	// The newest is the last one recorded; the oldest survivor is the one `extra`
	// records in, so everything before it fell off the end.
	if got[0] != fmt.Sprintf("query %02d", searchhistory.MaxEntries+extra-1) {
		t.Errorf("newest = %q, want the last query recorded", got[0])
	}
	if got[len(got)-1] != fmt.Sprintf("query %02d", extra) {
		t.Errorf("oldest survivor = %q, want %q", got[len(got)-1], fmt.Sprintf("query %02d", extra))
	}
}

// TestSearchHistoryClear checks that clearing empties the caller's history and is
// idempotent.
func TestSearchHistoryClear(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	e.record(t, client, "svatba", http.StatusNoContent)
	for range 2 {
		resp := e.mustDo(t, client, http.MethodDelete, "/api/v1/search-history", nil)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("clear status = %d, want 204", resp.StatusCode)
		}
		_ = resp.Body.Close()
		if got := e.list(t, client); len(got) != 0 {
			t.Fatalf("history after clear = %v, want empty", got)
		}
	}
}

// TestSearchHistoryRejectsBlankQuery checks that a whitespace-only query is a 400
// and leaves the history untouched.
func TestSearchHistoryRejectsBlankQuery(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	e.record(t, client, "   ", http.StatusBadRequest)
	if got := e.list(t, client); len(got) != 0 {
		t.Errorf("history = %v, want empty", got)
	}
}

// TestSearchHistoryRequiresAuth checks that an unauthenticated client is rejected
// on every verb.
func TestSearchHistoryRequiresAuth(t *testing.T) {
	e := newEnv(t)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		var body []byte
		if method == http.MethodPost {
			body = []byte(`{"query":"svatba"}`)
		}
		resp := e.mustDo(t, &http.Client{}, method, "/api/v1/search-history", body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s status = %d, want 401", method, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

// TestSearchHistoryPerUserIsolation checks that a history is strictly private: one
// user's searches never appear in another's list, and clearing one leaves the other
// untouched.
func TestSearchHistoryPerUserIsolation(t *testing.T) {
	e := newEnv(t)
	alice := e.login(t, "alice", auth.RoleViewer)
	bob := e.login(t, "bob", auth.RoleEditor)

	e.record(t, alice, "alice only", http.StatusNoContent)
	e.record(t, bob, "bob only", http.StatusNoContent)

	if got := e.list(t, alice); !slices.Equal(got, []string{"alice only"}) {
		t.Errorf("alice's history = %v, want only her own search", got)
	}
	if got := e.list(t, bob); !slices.Equal(got, []string{"bob only"}) {
		t.Errorf("bob's history = %v, want only his own search", got)
	}

	// Bob clearing his own history cannot reach Alice's.
	resp := e.mustDo(t, bob, http.MethodDelete, "/api/v1/search-history", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("bob clear status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if got := e.list(t, bob); len(got) != 0 {
		t.Errorf("bob's history after clear = %v, want empty", got)
	}
	if got := e.list(t, alice); !slices.Equal(got, []string{"alice only"}) {
		t.Errorf("alice's history after bob's clear = %v, want it untouched", got)
	}
}
