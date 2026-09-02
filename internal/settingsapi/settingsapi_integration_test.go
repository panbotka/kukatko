//go:build integration

package settingsapi_test

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

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/settings"
	"github.com/panbotka/kukatko/internal/settingsapi"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel.

const testPassword = "correct horse battery staple"

// env wires the auth and settings APIs behind an httptest server over the
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

	api := settingsapi.NewAPI(settingsapi.Config{
		Store:        settings.NewStore(db.Pool()),
		RequireAuth:  authAPI.RequireAuth,
		RequireAdmin: authAPI.RequireAdmin,
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
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
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

// decodeBody decodes a response body into a generic map and closes it.
func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return body
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

// openRegistration is a PUT body that opens registration with a secret and a
// welcome text.
var openRegistration = []byte(
	`{"registration_enabled":true,"registration_secret":"rodina2026","welcome_markdown":"# Vítej"}`)

// TestAdminRoundTripAndAudit: an administrator saves all three values, reads the
// full record back including the secret, and the change is audited.
func TestAdminRoundTripAndAudit(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)

	resp := env.mustDo(t, admin, http.MethodPut, "/api/v1/settings", openRegistration)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	saved := decodeBody(t, resp)
	if saved["registration_secret"] != "rodina2026" {
		t.Fatalf("PUT response secret = %v, want the stored secret", saved["registration_secret"])
	}

	got := decodeBody(t, env.mustDo(t, admin, http.MethodGet, "/api/v1/settings", nil))
	if got["registration_enabled"] != true || got["registration_secret"] != "rodina2026" ||
		got["welcome_markdown"] != "# Vítej" {
		t.Fatalf("GET /settings = %v, want the saved record", got)
	}
	if n := env.countAudit(t, audit.ActionSettingsUpdate); n != 1 {
		t.Fatalf("settings.update audit rows = %d, want 1", n)
	}
}

// TestPublicIsAnonymousAndLeaksNothing: the sign-in screen's endpoint answers
// without a session and carries only the two flags that screen needs.
func TestPublicIsAnonymousAndLeaksNothing(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)
	resp := env.mustDo(t, admin, http.MethodPut, "/api/v1/settings", openRegistration)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	anonymous := &http.Client{}
	resp = env.mustDo(t, anonymous, http.MethodGet, "/api/v1/settings/public", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous GET /settings/public status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	if len(body) != 2 || body["registration_enabled"] != true {
		t.Fatalf("public body = %v, want only registration_enabled=true and passkeys_enabled", body)
	}
	if _, ok := body["passkeys_enabled"]; !ok {
		t.Fatalf("public body = %v, want a passkeys_enabled flag", body)
	}
}

// TestWelcomeNeedsASessionAndHidesTheSecret: the welcome text is behind any
// session, is refused without one, and never carries the secret.
func TestWelcomeNeedsASessionAndHidesTheSecret(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)
	resp := env.mustDo(t, admin, http.MethodPut, "/api/v1/settings", openRegistration)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/settings/welcome", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous GET /settings/welcome status = %d, want 401", resp.StatusCode)
	}
	_ = resp.Body.Close()

	viewer := env.login(t, "viewer", auth.RoleViewer)
	body := decodeBody(t, env.mustDo(t, viewer, http.MethodGet, "/api/v1/settings/welcome", nil))
	if len(body) != 1 || body["welcome_markdown"] != "# Vítej" {
		t.Fatalf("welcome body = %v, want only welcome_markdown", body)
	}
}

// TestFullRecordIsAdminOnly: everyone below an administrator is refused both the
// full record and the update; an anonymous caller gets 401, a signed-in
// non-admin 403. A maintainer sits above admin on the ladder, so it may.
func TestFullRecordIsAdminOnly(t *testing.T) {
	env := newEnv(t)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		resp := env.mustDo(t, &http.Client{}, method, "/api/v1/settings", openRegistration)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s /settings status = %d, want 401", method, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleEditor} {
		client := env.login(t, "u_"+string(role), role)
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			resp := env.mustDo(t, client, method, "/api/v1/settings", openRegistration)
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("%s /settings as %s status = %d, want 403", method, role, resp.StatusCode)
			}
			_ = resp.Body.Close()
		}
	}

	maintainer := env.login(t, "boss", auth.RoleMaintainer)
	resp := env.mustDo(t, maintainer, http.MethodGet, "/api/v1/settings", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("maintainer GET /settings status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// TestOpeningRegistrationNeedsASecret: an open door with no lock is refused, and
// nothing is stored.
func TestOpeningRegistrationNeedsASecret(t *testing.T) {
	env := newEnv(t)
	admin := env.login(t, "admin", auth.RoleAdmin)

	resp := env.mustDo(t, admin, http.MethodPut, "/api/v1/settings",
		[]byte(`{"registration_enabled":true,"registration_secret":"  ","welcome_markdown":""}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT with a blank secret status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	body := decodeBody(t, env.mustDo(t, &http.Client{}, http.MethodGet, "/api/v1/settings/public", nil))
	if body["registration_enabled"] != false {
		t.Fatalf("registration_enabled after the refusal = %v, want false", body["registration_enabled"])
	}
	if n := env.countAudit(t, audit.ActionSettingsUpdate); n != 0 {
		t.Fatalf("refused update wrote %d audit rows, want 0", n)
	}
}
