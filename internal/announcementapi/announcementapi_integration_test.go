//go:build integration

package announcementapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/announcement"
	"github.com/panbotka/kukatko/internal/announcementapi"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and announcement APIs behind an httptest server over the
// integration database.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	db      *database.DB
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	api := announcementapi.NewAPI(announcementapi.Config{
		Store:             announcement.NewStore(db.Pool()),
		RequireAuth:       authAPI.RequireAuth,
		RequireMaintainer: authAPI.RequireMaintainer,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc, db: db}
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

// getMessage GETs /announcement and returns the decoded message field.
func (e *env) getMessage(t *testing.T, c *http.Client) string {
	t.Helper()
	resp := e.mustDo(t, c, http.MethodGet, "/api/v1/announcement", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /announcement status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Message string `json:"message"`
		Level   string `json:"level"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET body: %v", err)
	}
	return body.Message
}

// countAudit returns how many audit_log rows exist for the given action.
func (e *env) countAudit(t *testing.T, action string) int {
	t.Helper()
	n, err := audit.NewStore(e.db.Pool()).Count(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("counting audit %q: %v", action, err)
	}
	return n
}

// TestReadableByAnyAuthedUser: a viewer can read, and an empty library returns a
// 200 with an empty message rather than a 404.
func TestReadableByAnyAuthedUser(t *testing.T) {
	env := newEnv(t)
	viewer := env.login(t, "viewer", auth.RoleViewer)
	if msg := env.getMessage(t, viewer); msg != "" {
		t.Fatalf("empty announcement message = %q, want empty", msg)
	}
}

// TestPublishRequiresMaintainer: viewer/editor/admin are forbidden from PUT and
// DELETE; a maintainer may publish and clear.
func TestPublishRequiresMaintainer(t *testing.T) {
	env := newEnv(t)
	body := []byte(`{"message":"Downtime","level":"warning"}`)

	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleEditor, auth.RoleAdmin} {
		client := env.login(t, "u_"+string(role), role)
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			resp := env.mustDo(t, client, method, "/api/v1/announcement", body)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s as %s status = %d, want 403", method, role, resp.StatusCode)
			}
			_ = resp.Body.Close()
		}
	}

	maintainer := env.login(t, "boss", auth.RoleMaintainer)
	resp := env.mustDo(t, maintainer, http.MethodPut, "/api/v1/announcement", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("maintainer PUT status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestPublishThenReadAndClear: a published message is visible to a plain viewer,
// and clearing takes it back down. Both actions write an audit row.
func TestPublishThenReadAndClear(t *testing.T) {
	env := newEnv(t)
	maintainer := env.login(t, "boss", auth.RoleMaintainer)
	viewer := env.login(t, "viewer", auth.RoleViewer)

	resp := env.mustDo(t, maintainer, http.MethodPut, "/api/v1/announcement",
		[]byte(`{"message":"Údržba ve 22:00","level":"warning"}`))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("publish status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	if msg := env.getMessage(t, viewer); msg != "Údržba ve 22:00" {
		t.Fatalf("published message read as %q", msg)
	}
	if n := env.countAudit(t, audit.ActionAnnouncementSet); n != 1 {
		t.Fatalf("announcement.set audit rows = %d, want 1", n)
	}

	resp = env.mustDo(t, maintainer, http.MethodDelete, "/api/v1/announcement", nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clear status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	if msg := env.getMessage(t, viewer); msg != "" {
		t.Fatalf("message after clear = %q, want empty", msg)
	}
	if n := env.countAudit(t, audit.ActionAnnouncementClear); n != 1 {
		t.Fatalf("announcement.clear audit rows = %d, want 1", n)
	}
}

// TestUnauthenticatedRejected: no session cookie is 401 even for a read.
func TestUnauthenticatedRejected(t *testing.T) {
	env := newEnv(t)
	resp := env.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/announcement", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestPublishRejectsBadLevel: a maintainer publishing an unknown level gets 400.
func TestPublishRejectsBadLevel(t *testing.T) {
	env := newEnv(t)
	maintainer := env.login(t, "boss", auth.RoleMaintainer)
	resp := env.mustDo(t, maintainer, http.MethodPut, "/api/v1/announcement",
		[]byte(`{"message":"x","level":"bogus"}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad-level publish status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}
