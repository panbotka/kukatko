//go:build integration

package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// httpEnv is an httptest server wrapping the auth API plus two probe routes that
// exercise the RequireWrite and RequireAuth middlewares directly.
type httpEnv struct {
	server *httptest.Server
	svc    *auth.Service
}

// newHTTPEnv builds the HTTP test environment with a small login rate limit so
// the throttling test is fast.
func newHTTPEnv(t *testing.T, loginLimit int) *httpEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	limiter := auth.NewLimiter(loginLimit, time.Minute)
	api := auth.NewAPI(auth.APIConfig{Service: svc, Limiter: limiter})

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Route("/api/v1", func(r chi.Router) {
		api.RegisterRoutes(r)
		r.With(api.RequireWrite).Get("/probe/write", probeOK)
		r.With(api.RequireAuth).Get("/probe/auth", probeOK)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &httpEnv{server: server, svc: svc}
}

// probeOK is a trivial handler used behind RBAC middleware in tests.
func probeOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// newClient returns an HTTP client with a cookie jar so session cookies persist
// across requests.
func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &http.Client{Jar: jar}
}

// do issues a request with the test's context and returns the status code and
// body, closing the response body so callers need not.
func (e *httpEnv) do(t *testing.T, client *http.Client, method, path, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp.StatusCode, data
}

// loginJSON builds a login request body.
func loginJSON(username, password string) string {
	b, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return string(b)
}

// mustCreate creates a user through the service and fails the test on error.
func (e *httpEnv) mustCreate(t *testing.T, username string, role auth.Role) auth.User {
	t.Helper()
	user, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	return user
}

func TestHTTP_loginMeLogoutFlow(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "alice", auth.RoleEditor)
	client := newClient(t)

	status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login", loginJSON("alice", testPassword))
	if status != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", status, body)
	}
	var lr struct {
		User          map[string]any `json:"user"`
		DownloadToken string         `json:"download_token"`
	}
	if err := json.Unmarshal(body, &lr); err != nil {
		t.Fatalf("decoding login body: %v", err)
	}
	if lr.DownloadToken == "" {
		t.Error("login response missing download_token")
	}
	if lr.User["username"] != "alice" {
		t.Errorf("login user = %v, want alice", lr.User["username"])
	}
	if _, ok := lr.User["password_hash"]; ok {
		t.Error("login response leaks password_hash")
	}

	if status, _ := env.do(t, client, http.MethodGet, "/api/v1/auth/me", ""); status != http.StatusOK {
		t.Errorf("me status = %d, want 200", status)
	}
	if status, _ := env.do(t, client, http.MethodPost, "/api/v1/auth/logout", ""); status != http.StatusNoContent {
		t.Errorf("logout status = %d, want 204", status)
	}
	if status, _ := env.do(t, client, http.MethodGet, "/api/v1/auth/me", ""); status != http.StatusUnauthorized {
		t.Errorf("me after logout status = %d, want 401", status)
	}
}

func TestHTTP_loginSetsSecureCookieAttributes(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "mia", auth.RoleViewer)
	client := newClient(t)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		env.server.URL+"/api/v1/auth/login", strings.NewReader(loginJSON("mia", testPassword)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer resp.Body.Close()

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "kukatko_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("login did not set kukatko_session cookie")
	}
	if !sessionCookie.HttpOnly {
		t.Error("session cookie is not HttpOnly")
	}
	if sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("session cookie SameSite = %v, want Strict", sessionCookie.SameSite)
	}
}

func TestHTTP_loginBadCredentials(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "nate", auth.RoleViewer)
	client := newClient(t)

	if status, _ := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("nate", "wrong")); status != http.StatusUnauthorized {
		t.Errorf("bad-credentials status = %d, want 401", status)
	}
}

func TestHTTP_rbacEnforcement(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "viewer", auth.RoleViewer)
	env.mustCreate(t, "editor", auth.RoleEditor)
	env.mustCreate(t, "admin", auth.RoleAdmin)

	t.Run("unauthenticated blocked", func(t *testing.T) {
		client := newClient(t)
		if status, _ := env.do(t, client, http.MethodGet, "/api/v1/probe/auth", ""); status != http.StatusUnauthorized {
			t.Errorf("anon probe/auth = %d, want 401", status)
		}
	})

	t.Run("viewer is read-only", func(t *testing.T) {
		client := env.loginClient(t, "viewer")
		assertStatus(t, env, client, http.MethodGet, "/api/v1/probe/auth", "", http.StatusOK)
		assertStatus(t, env, client, http.MethodGet, "/api/v1/probe/write", "", http.StatusForbidden)
		assertStatus(t, env, client, http.MethodGet, "/api/v1/admin/users", "", http.StatusForbidden)
		assertStatus(t, env, client, http.MethodPost, "/api/v1/admin/users",
			`{"username":"x","password":"password123","role":"viewer"}`, http.StatusForbidden)
	})

	t.Run("editor can write but not administer", func(t *testing.T) {
		client := env.loginClient(t, "editor")
		assertStatus(t, env, client, http.MethodGet, "/api/v1/probe/write", "", http.StatusOK)
		assertStatus(t, env, client, http.MethodGet, "/api/v1/admin/users", "", http.StatusForbidden)
	})

	t.Run("admin can administer", func(t *testing.T) {
		client := env.loginClient(t, "admin")
		assertStatus(t, env, client, http.MethodGet, "/api/v1/admin/users", "", http.StatusOK)
		assertStatus(t, env, client, http.MethodPost, "/api/v1/admin/users",
			`{"username":"newbie","password":"password123","role":"editor"}`, http.StatusCreated)
	})
}

// loginClient returns a fresh cookie-jar client already logged in as username
// (with the shared test password).
func (e *httpEnv) loginClient(t *testing.T, username string) *http.Client {
	t.Helper()
	client := newClient(t)
	if status, body := e.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON(username, testPassword)); status != http.StatusOK {
		t.Fatalf("login %q status = %d, want 200 (body %s)", username, status, body)
	}
	return client
}

// assertStatus issues a request and asserts the response status code.
func assertStatus(t *testing.T, env *httpEnv, client *http.Client, method, path, body string, want int) {
	t.Helper()
	if status, data := env.do(t, client, method, path, body); status != want {
		t.Errorf("%s %s status = %d, want %d (body %s)", method, path, status, want, data)
	}
}

func TestHTTP_loginRateLimited(t *testing.T) {
	env := newHTTPEnv(t, 3)
	env.mustCreate(t, "target", auth.RoleViewer)
	client := newClient(t)

	// The first 3 wrong attempts are answered 401; the 4th is throttled with 429.
	for i := range 3 {
		if status, _ := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
			loginJSON("target", "wrong")); status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, status)
		}
	}
	if status, _ := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("target", "wrong")); status != http.StatusTooManyRequests {
		t.Errorf("4th attempt status = %d, want 429", status)
	}
	// Even correct credentials are blocked once throttled.
	if status, _ := env.do(t, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("target", testPassword)); status != http.StatusTooManyRequests {
		t.Errorf("throttled correct-login status = %d, want 429", status)
	}
}

func TestHTTP_changePassword(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "olive", auth.RoleEditor)
	client := env.loginClient(t, "olive")

	assertStatus(t, env, client, http.MethodPost, "/api/v1/auth/password",
		`{"current_password":"wrong","new_password":"a-good-new-password"}`, http.StatusUnauthorized)
	assertStatus(t, env, client, http.MethodPost, "/api/v1/auth/password",
		`{"current_password":"`+testPassword+`","new_password":"a-good-new-password"}`, http.StatusNoContent)

	// The original session is kept, so /auth/me still works after the change.
	assertStatus(t, env, client, http.MethodGet, "/api/v1/auth/me", "", http.StatusOK)
}

func TestHTTP_adminResetPassword(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "rootadmin", auth.RoleAdmin)
	target := env.mustCreate(t, "victim", auth.RoleViewer)
	admin := env.loginClient(t, "rootadmin")

	assertStatus(t, env, admin, http.MethodPost, "/api/v1/admin/users/"+target.UID+"/password",
		`{"new_password":"reset-by-admin-pw"}`, http.StatusNoContent)

	// The target can log in with the admin-set password.
	user := newClient(t)
	assertStatus(t, env, user, http.MethodPost, "/api/v1/auth/login",
		loginJSON("victim", "reset-by-admin-pw"), http.StatusOK)
}
