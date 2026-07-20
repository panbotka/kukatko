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

// TestHTTP_loginRejectsOverLongUsername verifies the public login endpoint
// refuses an oversized username with 400 before it can be turned into a
// rate-limiter key. Without the cap, each such request would leave a
// megabyte-scale key in the process-global limiter map.
func TestHTTP_loginRejectsOverLongUsername(t *testing.T) {
	env := newHTTPEnv(t, 3)
	client := newClient(t)

	long := strings.Repeat("a", auth.MaxUsernameLen+1)
	status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login", loginJSON(long, testPassword))
	if status != http.StatusBadRequest {
		t.Fatalf("over-long username status = %d, want 400 (body %s)", status, body)
	}

	// Rejection must not have consumed the caller's throttling budget either: a
	// normal login for a real account still works right after.
	env.mustCreate(t, "quinn", auth.RoleViewer)
	assertStatus(t, env, client, http.MethodPost, "/api/v1/auth/login",
		loginJSON("quinn", testPassword), http.StatusOK)
}

// TestHTTP_createUserRejectsOverLongUsername verifies an admin cannot create an
// account whose username exceeds the login cap, which could never log in.
func TestHTTP_createUserRejectsOverLongUsername(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "root", auth.RoleAdmin)
	client := env.loginClient(t, "root")

	body := adminUserBody(t, map[string]any{
		"username": strings.Repeat("a", auth.MaxUsernameLen+1),
		"password": testPassword,
		"role":     string(auth.RoleViewer),
	})
	assertStatus(t, env, client, http.MethodPost, "/api/v1/admin/users", body, http.StatusBadRequest)
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

// adminUserBody marshals fields into a JSON body for the admin user endpoints.
// A key that is simply absent from fields is absent from the body, which is how
// the partial-update tests express "leave the note untouched".
func adminUserBody(t *testing.T, fields map[string]any) string {
	t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshalling body: %v", err)
	}
	return string(b)
}

// decodeUser unmarshals a single user object from a response body into a generic
// map, so tests can assert on the presence or absence of a JSON key.
func decodeUser(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var user map[string]any
	if err := json.Unmarshal(body, &user); err != nil {
		t.Fatalf("decoding user: %v (body %s)", err, body)
	}
	return user
}

// assertNote asserts the user object carries a note field equal to want.
func assertNote(t *testing.T, user map[string]any, want string) {
	t.Helper()
	got, ok := user["note"]
	if !ok {
		t.Fatalf("admin user payload has no note field: %v", user)
	}
	if got != want {
		t.Errorf("note = %q, want %q", got, want)
	}
}

// createNotedUser creates a user through the admin API with the given note and
// returns the decoded response body.
func (e *httpEnv) createNotedUser(t *testing.T, admin *http.Client, username, note string) map[string]any {
	t.Helper()
	body := adminUserBody(t, map[string]any{
		"username": username, "password": testPassword, "role": "viewer", "note": note,
	})
	status, data := e.do(t, admin, http.MethodPost, "/api/v1/admin/users", body)
	if status != http.StatusCreated {
		t.Fatalf("create user status = %d, want 201 (body %s)", status, data)
	}
	return decodeUser(t, data)
}

func TestHTTP_adminUserNoteLifecycle(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "noteadmin", auth.RoleAdmin)
	admin := env.loginClient(t, "noteadmin")

	const note = "Contractor; account kept for the 2026 audit."
	created := env.createNotedUser(t, admin, "noted", note)
	assertNote(t, created, note)
	uid, _ := created["uid"].(string)
	path := "/api/v1/admin/users/" + uid

	// A create that omits display_name and note stays valid and defaults to empty.
	bare := adminUserBody(t, map[string]any{
		"username": "bare", "password": testPassword, "role": "viewer",
	})
	status, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", bare)
	if status != http.StatusCreated {
		t.Fatalf("bare create status = %d, want 201 (body %s)", status, data)
	}
	assertNote(t, decodeUser(t, data), "")

	// Omitting note from the update leaves the stored note untouched.
	partial := adminUserBody(t, map[string]any{
		"display_name": "Noted User", "email": "noted@example.com", "role": "viewer",
	})
	status, data = env.do(t, admin, http.MethodPatch, path, partial)
	if status != http.StatusOK {
		t.Fatalf("partial update status = %d, want 200 (body %s)", status, data)
	}
	updated := decodeUser(t, data)
	assertNote(t, updated, note)
	if updated["display_name"] != "Noted User" {
		t.Errorf("display_name = %v, want %q", updated["display_name"], "Noted User")
	}

	// Sending an empty string clears the note.
	clearBody := adminUserBody(t, map[string]any{
		"display_name": "Noted User", "email": "noted@example.com", "role": "viewer", "note": "",
	})
	status, data = env.do(t, admin, http.MethodPatch, path, clearBody)
	if status != http.StatusOK {
		t.Fatalf("clear-note status = %d, want 200 (body %s)", status, data)
	}
	assertNote(t, decodeUser(t, data), "")
}

func TestHTTP_adminUserNoteListedAndPersisted(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "listadmin", auth.RoleAdmin)
	admin := env.loginClient(t, "listadmin")

	const note = "Owner of the shared family album."
	created := env.createNotedUser(t, admin, "listed", note)
	uid, _ := created["uid"].(string)

	status, data := env.do(t, admin, http.MethodGet, "/api/v1/admin/users", "")
	if status != http.StatusOK {
		t.Fatalf("list users status = %d, want 200 (body %s)", status, data)
	}
	var users []map[string]any
	if err := json.Unmarshal(data, &users); err != nil {
		t.Fatalf("decoding user list: %v (body %s)", err, data)
	}
	for _, u := range users {
		if u["uid"] == uid {
			assertNote(t, u, note)
			return
		}
	}
	t.Fatalf("created user %q missing from list (body %s)", uid, data)
}

func TestHTTP_adminUserNoteTooLong(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "lenadmin", auth.RoleAdmin)
	admin := env.loginClient(t, "lenadmin")

	tooLong := strings.Repeat("a", auth.MaxNoteLen+1)

	create := adminUserBody(t, map[string]any{
		"username": "toolong", "password": testPassword, "role": "viewer", "note": tooLong,
	})
	status, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", create)
	if status != http.StatusBadRequest {
		t.Fatalf("over-length create status = %d, want 400 (body %s)", status, data)
	}
	if !strings.Contains(string(data), "note") {
		t.Errorf("over-length create error does not name the note field: %s", data)
	}

	// The same limit applies on update.
	target := env.createNotedUser(t, admin, "lentarget", "short")
	uid, _ := target["uid"].(string)
	update := adminUserBody(t, map[string]any{"role": "viewer", "note": tooLong})
	status, data = env.do(t, admin, http.MethodPatch, "/api/v1/admin/users/"+uid, update)
	if status != http.StatusBadRequest {
		t.Fatalf("over-length update status = %d, want 400 (body %s)", status, data)
	}
	if !strings.Contains(string(data), "note") {
		t.Errorf("over-length update error does not name the note field: %s", data)
	}

	// A note of exactly MaxNoteLen runes is accepted.
	atLimit := adminUserBody(t, map[string]any{
		"username": "atlimit", "password": testPassword, "role": "viewer",
		"note": strings.Repeat("a", auth.MaxNoteLen),
	})
	if status, data := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", atLimit); status != http.StatusCreated {
		t.Errorf("at-limit create status = %d, want 201 (body %s)", status, data)
	}
}

func TestHTTP_userNoteNeverReachesNonAdmin(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "secretadmin", auth.RoleAdmin)
	admin := env.loginClient(t, "secretadmin")

	const note = "Do not surface this to the account holder."
	env.createNotedUser(t, admin, "subject", note)

	// The noted user logs in as a plain viewer: neither the login payload nor
	// /auth/me may carry the note.
	client := newClient(t)
	status, data := env.do(t, client, http.MethodPost, "/api/v1/auth/login", loginJSON("subject", testPassword))
	if status != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body %s)", status, data)
	}
	assertNoteAbsent(t, t.Name()+" login", data)

	status, data = env.do(t, client, http.MethodGet, "/api/v1/auth/me", "")
	if status != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (body %s)", status, data)
	}
	assertNoteAbsent(t, t.Name()+" me", data)

	// And the viewer cannot reach the admin endpoint that would expose it.
	assertStatus(t, env, client, http.MethodGet, "/api/v1/admin/users", "", http.StatusForbidden)
}

// assertNoteAbsent asserts that a session payload's embedded user object carries
// no note key and that the note text appears nowhere in the raw body.
func assertNoteAbsent(t *testing.T, label string, body []byte) {
	t.Helper()
	var resp struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("%s: decoding body: %v (body %s)", label, err, body)
	}
	if _, ok := resp.User["note"]; ok {
		t.Errorf("%s: payload leaks note field: %v", label, resp.User)
	}
	if strings.Contains(string(body), "Do not surface") {
		t.Errorf("%s: payload leaks note text: %s", label, body)
	}
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
