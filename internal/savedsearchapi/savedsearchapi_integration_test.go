//go:build integration

package savedsearchapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/savedsearch"
	"github.com/panbotka/kukatko/internal/savedsearchapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and saved-search APIs behind an httptest server over the
// integration database.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
}

// searchView mirrors the JSON shape returned for a saved search.
type searchView struct {
	UID       string          `json:"uid"`
	Name      string          `json:"name"`
	Params    json.RawMessage `json:"params"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := savedsearchapi.NewAPI(savedsearchapi.Config{
		Store:       savedsearch.NewStore(db.Pool()),
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

// jsonEqual reports whether got and the want literal are the same JSON value,
// ignoring whitespace and object key order. The params column is JSONB, which
// normalises both on round-trip, so a byte-exact comparison would be wrong.
func jsonEqual(t *testing.T, got json.RawMessage, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("unmarshal got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("unmarshal want %s: %v", want, err)
	}
	return reflect.DeepEqual(a, b)
}

// decodeBody decodes the JSON response body into dst and closes it.
func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// TestSavedSearchLifecycle exercises create, list, get, patch and delete over HTTP
// for a single owner.
func TestSavedSearchLifecycle(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/saved-searches",
		[]byte(`{"name":"Recent","params":{"sort":"newest","q":"hory"}}`))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created searchView
	decodeBody(t, resp, &created)
	if created.UID == "" || created.Name != "Recent" || !jsonEqual(t, created.Params, `{"sort":"newest","q":"hory"}`) {
		t.Fatalf("unexpected created search: %+v", created)
	}

	resp = e.mustDo(t, client, http.MethodGet, "/api/v1/saved-searches", nil)
	var list struct {
		SavedSearches []searchView `json:"saved_searches"`
	}
	decodeBody(t, resp, &list)
	if len(list.SavedSearches) != 1 || list.SavedSearches[0].UID != created.UID {
		t.Fatalf("list mismatch: %+v", list.SavedSearches)
	}

	resp = e.mustDo(t, client, http.MethodGet, "/api/v1/saved-searches/"+created.UID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = e.mustDo(t, client, http.MethodPatch, "/api/v1/saved-searches/"+created.UID,
		[]byte(`{"name":"Older","params":{"sort":"oldest"}}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp.StatusCode)
	}
	var updated searchView
	decodeBody(t, resp, &updated)
	if updated.Name != "Older" || !jsonEqual(t, updated.Params, `{"sort":"oldest"}`) {
		t.Fatalf("unexpected updated search: %+v", updated)
	}

	resp = e.mustDo(t, client, http.MethodDelete, "/api/v1/saved-searches/"+created.UID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()
	resp = e.mustDo(t, client, http.MethodGet, "/api/v1/saved-searches/"+created.UID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestSavedSearchCreateEmptyName checks that a blank name is rejected with 400.
func TestSavedSearchCreateEmptyName(t *testing.T) {
	e := newEnv(t)
	client := e.login(t, "alice", auth.RoleViewer)

	resp := e.mustDo(t, client, http.MethodPost, "/api/v1/saved-searches", []byte(`{"name":"   "}`))
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400", resp.StatusCode)
	}
}

// TestSavedSearchRequiresAuth checks that an unauthenticated client is rejected.
func TestSavedSearchRequiresAuth(t *testing.T) {
	e := newEnv(t)
	resp := e.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/saved-searches", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestSavedSearchOwnerIsolation checks that user B cannot read, patch or delete
// user A's saved search: every cross-owner access is reported as 404, and A's
// search survives B's attempts.
func TestSavedSearchOwnerIsolation(t *testing.T) {
	e := newEnv(t)
	alice := e.login(t, "alice", auth.RoleViewer)
	bob := e.login(t, "bob", auth.RoleViewer)

	resp := e.mustDo(t, alice, http.MethodPost, "/api/v1/saved-searches",
		[]byte(`{"name":"Alice only","params":{}}`))
	var created searchView
	decodeBody(t, resp, &created)
	if created.UID == "" {
		t.Fatalf("alice create failed: %+v", created)
	}
	path := "/api/v1/saved-searches/" + created.UID

	// Bob cannot read, patch or delete Alice's search — each is a 404.
	for _, tc := range []struct {
		method string
		body   []byte
	}{
		{http.MethodGet, nil},
		{http.MethodPatch, []byte(`{"name":"Hijacked"}`)},
		{http.MethodDelete, nil},
	} {
		r := e.mustDo(t, bob, tc.method, path, tc.body)
		if r.StatusCode != http.StatusNotFound {
			t.Fatalf("bob %s status = %d, want 404", tc.method, r.StatusCode)
		}
		_ = r.Body.Close()
	}

	// Bob's own list does not include Alice's search.
	r := e.mustDo(t, bob, http.MethodGet, "/api/v1/saved-searches", nil)
	var bobList struct {
		SavedSearches []searchView `json:"saved_searches"`
	}
	decodeBody(t, r, &bobList)
	if len(bobList.SavedSearches) != 0 {
		t.Fatalf("bob's list = %+v, want empty", bobList.SavedSearches)
	}

	// Alice's search survives Bob's attempts unchanged.
	r = e.mustDo(t, alice, http.MethodGet, path, nil)
	if r.StatusCode != http.StatusOK {
		t.Fatalf("alice get status = %d, want 200", r.StatusCode)
	}
	var survived searchView
	decodeBody(t, r, &survived)
	if survived.Name != "Alice only" {
		t.Fatalf("alice's search was modified: %+v", survived)
	}
}
