//go:build integration

package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// tokenEnv is an httptest server over the auth API with a service clock the test
// drives, plus direct database access so audit rows and last_used_at stamps can
// be asserted.
type tokenEnv struct {
	server *httptest.Server
	svc    *auth.Service
	db     *database.DB
	now    *time.Time
}

// newTokenEnv builds the API-token test environment. createLimit caps token
// creations per user+IP (the login limiter is reused for that).
func newTokenEnv(t *testing.T, createLimit int) *tokenEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := auth.NewStore(db.Pool())
	svc := auth.NewService(store, auth.SessionPolicy{TTL: testTTL, MaxLifetime: testMaxLifetime}).
		WithClock(func() time.Time { return now })
	api := auth.NewAPI(auth.APIConfig{
		Service: svc,
		Limiter: auth.NewLimiter(createLimit, time.Minute),
	})

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Route("/api/v1", func(r chi.Router) {
		api.RegisterRoutes(r)
		r.With(api.RequireAuth).Get("/probe/auth", probeOK)
		r.With(api.RequireWrite).Get("/probe/write", probeOK)
		r.With(api.RequireAdmin).Get("/probe/admin", probeOK)
	})

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &tokenEnv{server: server, svc: svc, db: db, now: &now}
}

// user creates a user through the service, failing the test on error.
func (e *tokenEnv) user(t *testing.T, username string, role auth.Role) auth.User {
	t.Helper()
	user, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	return user
}

// mintToken creates an API token straight through the service (bypassing HTTP)
// and returns the stored token plus its plaintext credential.
func (e *tokenEnv) mintToken(
	t *testing.T, userUID, name string, expiresAt *time.Time,
) (auth.APIToken, string) {
	t.Helper()
	entry := audit.Entry{ActorUID: userUID, Action: audit.ActionAPITokenCreate, TargetType: "api_tokens"}
	tok, secret, err := e.svc.CreateAPIToken(t.Context(), userUID,
		auth.CreateAPITokenInput{Name: name, ExpiresAt: expiresAt}, entry)
	if err != nil {
		t.Fatalf("CreateAPIToken(%q): %v", name, err)
	}
	return tok, secret
}

// request issues a request carrying the optional bearer credential and returns
// the status and body, closing the response body.
func (e *tokenEnv) request(t *testing.T, method, path, bearer, body string) (int, []byte) {
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
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
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

// login authenticates username with the shared test password and returns the
// session cookie value, so cookie-authenticated requests can be issued.
func (e *tokenEnv) login(t *testing.T, username string) *http.Cookie {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		e.server.URL+"/api/v1/auth/login", strings.NewReader(loginJSON(username, testPassword)))
	if err != nil {
		t.Fatalf("new login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("login %q: %v", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %q status = %d, want 200", username, resp.StatusCode)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "kukatko_session" {
			return c
		}
	}
	t.Fatalf("login %q set no session cookie", username)
	return nil
}

// cookieRequest issues a request authenticated by the session cookie.
func (e *tokenEnv) cookieRequest(
	t *testing.T, method, path string, cookie *http.Cookie, body string,
) (int, []byte) {
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
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
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

// createdToken mirrors the JSON body of POST /auth/tokens.
type createdToken struct {
	Token  map[string]any `json:"token"`
	Secret string         `json:"secret"`
}

// auditCount returns how many audit_log rows carry the given action and actor.
func (e *tokenEnv) auditCount(t *testing.T, action, actorUID string) int {
	t.Helper()
	var n int
	err := e.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM audit_log WHERE action = $1 AND actor_uid = $2", action, actorUID).Scan(&n)
	if err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	return n
}

// lastUsedAt reads the token's last_used_at stamp, nil when never used.
func (e *tokenEnv) lastUsedAt(t *testing.T, id string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT last_used_at FROM api_tokens WHERE id = $1", id).Scan(&at); err != nil {
		t.Fatalf("reading last_used_at: %v", err)
	}
	return at
}

func TestHTTP_apiTokenCreateListRevoke(t *testing.T) {
	env := newTokenEnv(t, 50)
	alice := env.user(t, "alice", auth.RoleEditor)
	cookie := env.login(t, "alice")

	status, body := env.cookieRequest(t, http.MethodPost, "/api/v1/auth/tokens", cookie, `{"name":"backup cli"}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", status, body)
	}
	var created createdToken
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decoding create body: %v", err)
	}
	if !strings.HasPrefix(created.Secret, "kkt_") {
		t.Errorf("secret %q does not start with kkt_", created.Secret)
	}
	id, _ := created.Token["id"].(string)
	if id == "" {
		t.Fatal("create response carries no token id")
	}
	if !strings.Contains(created.Secret, "_"+id+"_") {
		t.Errorf("secret %q does not embed token id %q", created.Secret, id)
	}
	if _, leaked := created.Token["secret_hash"]; leaked {
		t.Error("create response leaks secret_hash")
	}

	// The list shows the token's metadata but never the secret.
	status, body = env.cookieRequest(t, http.MethodGet, "/api/v1/auth/tokens", cookie, "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200", status)
	}
	if strings.Contains(string(body), created.Secret) {
		t.Error("list response leaks the token secret")
	}
	if strings.Contains(string(body), "secret_hash") {
		t.Error("list response leaks secret_hash")
	}
	var listed struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decoding list body: %v", err)
	}
	if len(listed.Tokens) != 1 {
		t.Fatalf("listed %d tokens, want 1", len(listed.Tokens))
	}
	if listed.Tokens[0]["name"] != "backup cli" {
		t.Errorf("listed name = %v, want %q", listed.Tokens[0]["name"], "backup cli")
	}
	if _, ok := listed.Tokens[0]["revoked_at"]; ok {
		t.Error("a fresh token reports revoked_at")
	}

	// The token authenticates before revocation and not after.
	if status, _ := env.request(t, http.MethodGet, "/api/v1/probe/auth", created.Secret, ""); status != http.StatusOK {
		t.Errorf("bearer probe before revoke = %d, want 200", status)
	}
	if status, body := env.cookieRequest(t, http.MethodDelete, "/api/v1/auth/tokens/"+id, cookie, ""); status != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204 (body %s)", status, body)
	}
	if status, _ := env.request(t, http.MethodGet, "/api/v1/probe/auth", created.Secret, ""); status != http.StatusUnauthorized {
		t.Errorf("bearer probe after revoke = %d, want 401", status)
	}

	// Revocation is idempotent and the revoked token stays listed.
	if status, _ := env.cookieRequest(t, http.MethodDelete, "/api/v1/auth/tokens/"+id, cookie, ""); status != http.StatusNoContent {
		t.Errorf("second revoke status = %d, want 204", status)
	}
	_, body = env.cookieRequest(t, http.MethodGet, "/api/v1/auth/tokens", cookie, "")
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decoding list body: %v", err)
	}
	if len(listed.Tokens) != 1 || listed.Tokens[0]["revoked_at"] == nil {
		t.Errorf("revoked token missing or unstamped: %v", listed.Tokens)
	}

	// Both mutations were audited exactly once, against the acting user.
	if got := env.auditCount(t, audit.ActionAPITokenCreate, alice.UID); got != 1 {
		t.Errorf("api_token.create audit rows = %d, want 1", got)
	}
	if got := env.auditCount(t, audit.ActionAPITokenRevoke, alice.UID); got != 1 {
		t.Errorf("api_token.revoke audit rows = %d, want 1 (idempotent re-revoke writes none)", got)
	}
}

func TestHTTP_bearerInheritsUserRole(t *testing.T) {
	env := newTokenEnv(t, 50)
	viewer := env.user(t, "viewer", auth.RoleViewer)
	admin := env.user(t, "admin", auth.RoleAdmin)
	_, viewerSecret := env.mintToken(t, viewer.UID, "viewer token", nil)
	_, adminSecret := env.mintToken(t, admin.UID, "admin token", nil)

	tests := []struct {
		name   string
		secret string
		path   string
		want   int
	}{
		{"viewer reads", viewerSecret, "/api/v1/probe/auth", http.StatusOK},
		{"viewer cannot write", viewerSecret, "/api/v1/probe/write", http.StatusForbidden},
		{"viewer cannot administer", viewerSecret, "/api/v1/probe/admin", http.StatusForbidden},
		{"admin writes", adminSecret, "/api/v1/probe/write", http.StatusOK},
		{"admin administers", adminSecret, "/api/v1/probe/admin", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if status, body := env.request(t, http.MethodGet, tt.path, tt.secret, ""); status != tt.want {
				t.Errorf("%s = %d, want %d (body %s)", tt.path, status, tt.want, body)
			}
		})
	}

	// The bearer identity reaches the handlers, not just the middleware.
	status, body := env.request(t, http.MethodGet, "/api/v1/auth/me", viewerSecret, "")
	if status != http.StatusOK {
		t.Fatalf("/auth/me with bearer = %d, want 200", status)
	}
	var me struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(body, &me); err != nil {
		t.Fatalf("decoding /auth/me: %v", err)
	}
	if me.User["username"] != "viewer" {
		t.Errorf("/auth/me username = %v, want viewer", me.User["username"])
	}
}

func TestHTTP_bearerRejectsBadCredentials(t *testing.T) {
	env := newTokenEnv(t, 50)
	alice := env.user(t, "alice", auth.RoleEditor)

	expiry := env.now.Add(time.Hour)
	_, expiredSecret := env.mintToken(t, alice.UID, "expiring", &expiry)
	revoked, revokedSecret := env.mintToken(t, alice.UID, "doomed", nil)
	valid, validSecret := env.mintToken(t, alice.UID, "good", nil)
	disabledUser := env.user(t, "gone", auth.RoleEditor)
	_, disabledSecret := env.mintToken(t, disabledUser.UID, "orphan", nil)

	entry := audit.Entry{ActorUID: alice.UID, Action: audit.ActionAPITokenRevoke, TargetType: "api_tokens"}
	if err := env.svc.RevokeAPIToken(t.Context(), revoked.ID, alice, entry); err != nil {
		t.Fatalf("RevokeAPIToken: %v", err)
	}
	if _, err := env.svc.SetUserDisabled(t.Context(), disabledUser.UID, true); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	// Move the clock past the expiring token's expiry.
	*env.now = env.now.Add(2 * time.Hour)

	// The valid token still works after the clock moves, anchoring the negatives.
	if status, _ := env.request(t, http.MethodGet, "/api/v1/probe/auth", validSecret, ""); status != http.StatusOK {
		t.Fatalf("valid token = %d, want 200", status)
	}

	// A wrong secret against a real token id, and an unknown id in a well-formed
	// token, must be as opaque as outright garbage.
	wrongSecret := "kkt_" + valid.ID + "_deadbeef"
	unknownID := "kkt_atzzzzzzzzzzzzzzzzzzzzzzzz_deadbeef"

	tests := []struct {
		name   string
		bearer string
	}{
		{"expired", expiredSecret},
		{"revoked", revokedSecret},
		{"disabled owner", disabledSecret},
		{"wrong secret", wrongSecret},
		{"unknown id", unknownID},
		{"garbage", "not-a-token-at-all"},
		{"session-shaped", "8Xf3zQdeadbeef"},
		{"scheme only", "kkt_"},
	}
	var bodies []string
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := env.request(t, http.MethodGet, "/api/v1/probe/auth", tt.bearer, "")
			if status != http.StatusUnauthorized {
				t.Errorf("%s = %d, want 401 (never 403; body %s)", tt.name, status, body)
			}
			bodies = append(bodies, string(body))
		})
	}
	// The body must not reveal which failure it was: all responses are identical.
	for i, body := range bodies {
		if body != bodies[0] {
			t.Errorf("response body for %q differs from the first: %s vs %s", tests[i].name, body, bodies[0])
		}
	}
}

func TestHTTP_bearerDoesNotDisturbCookieAuth(t *testing.T) {
	env := newTokenEnv(t, 50)
	env.user(t, "alice", auth.RoleEditor)
	cookie := env.login(t, "alice")

	// No Authorization header: the cookie path is untouched.
	if status, _ := env.cookieRequest(t, http.MethodGet, "/api/v1/probe/auth", cookie, ""); status != http.StatusOK {
		t.Errorf("cookie probe = %d, want 200", status)
	}

	// A bad bearer credential is final: it is not retried against the cookie the
	// same request happens to carry.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/api/v1/probe/auth", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Authorization", "Bearer kkt_atnope_nope")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad bearer alongside a good cookie = %d, want 401", resp.StatusCode)
	}

	// A non-Bearer Authorization scheme falls through to the cookie.
	req2, err := http.NewRequestWithContext(t.Context(), http.MethodGet, env.server.URL+"/api/v1/probe/auth", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req2.AddCookie(cookie)
	req2.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("Basic scheme alongside a good cookie = %d, want 200 (cookie honoured)", resp2.StatusCode)
	}
}

func TestHTTP_apiTokenLastUsedThrottled(t *testing.T) {
	env := newTokenEnv(t, 50)
	alice := env.user(t, "alice", auth.RoleEditor)
	tok, secret := env.mintToken(t, alice.UID, "busy client", nil)

	if at := env.lastUsedAt(t, tok.ID); at != nil {
		t.Fatalf("fresh token has last_used_at = %v, want NULL", at)
	}

	// First use stamps the token.
	env.request(t, http.MethodGet, "/api/v1/probe/auth", secret, "")
	first := env.lastUsedAt(t, tok.ID)
	if first == nil {
		t.Fatal("first use did not stamp last_used_at")
	}
	if !first.Equal(*env.now) {
		t.Errorf("last_used_at = %v, want %v", first, *env.now)
	}

	// A burst of requests inside the same minute must not rewrite the stamp.
	*env.now = env.now.Add(30 * time.Second)
	for range 5 {
		env.request(t, http.MethodGet, "/api/v1/probe/auth", secret, "")
	}
	if at := env.lastUsedAt(t, tok.ID); !at.Equal(*first) {
		t.Errorf("last_used_at moved to %v within the guard interval, want %v", at, first)
	}

	// Past the interval, the next request refreshes it.
	*env.now = env.now.Add(time.Minute)
	env.request(t, http.MethodGet, "/api/v1/probe/auth", secret, "")
	if at := env.lastUsedAt(t, tok.ID); !at.Equal(*env.now) {
		t.Errorf("last_used_at = %v after the guard interval, want %v", at, *env.now)
	}
}

func TestHTTP_apiTokenRevokeOwnership(t *testing.T) {
	env := newTokenEnv(t, 50)
	alice := env.user(t, "alice", auth.RoleEditor)
	env.user(t, "bob", auth.RoleEditor)
	env.user(t, "root", auth.RoleAdmin)

	aliceToken, aliceSecret := env.mintToken(t, alice.UID, "alice's", nil)
	otherToken, _ := env.mintToken(t, alice.UID, "alice's second", nil)

	// Bob sees someone else's token as absent, not forbidden.
	bobCookie := env.login(t, "bob")
	if status, _ := env.cookieRequest(t, http.MethodDelete,
		"/api/v1/auth/tokens/"+aliceToken.ID, bobCookie, ""); status != http.StatusNotFound {
		t.Errorf("bob revoking alice's token = %d, want 404", status)
	}
	// A wholly unknown id is the same 404.
	if status, _ := env.cookieRequest(t, http.MethodDelete,
		"/api/v1/auth/tokens/atnosuchtoken", bobCookie, ""); status != http.StatusNotFound {
		t.Errorf("revoking an unknown id = %d, want 404", status)
	}
	// Bob's list does not show alice's tokens.
	_, body := env.cookieRequest(t, http.MethodGet, "/api/v1/auth/tokens", bobCookie, "")
	var listed struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decoding list body: %v", err)
	}
	if len(listed.Tokens) != 0 {
		t.Errorf("bob sees %d tokens, want 0", len(listed.Tokens))
	}
	// Alice's token still authenticates: bob's attempt changed nothing.
	if status, _ := env.request(t, http.MethodGet, "/api/v1/probe/auth", aliceSecret, ""); status != http.StatusOK {
		t.Errorf("alice's token after bob's attempt = %d, want 200", status)
	}

	// The admin may revoke anyone's token.
	rootCookie := env.login(t, "root")
	if status, _ := env.cookieRequest(t, http.MethodDelete,
		"/api/v1/auth/tokens/"+otherToken.ID, rootCookie, ""); status != http.StatusNoContent {
		t.Errorf("admin revoking alice's token = %d, want 204", status)
	}

	// An API token may manage its owner's tokens too.
	if status, _ := env.request(t, http.MethodDelete,
		"/api/v1/auth/tokens/"+aliceToken.ID, aliceSecret, ""); status != http.StatusNoContent {
		t.Errorf("token revoking itself = %d, want 204", status)
	}
	if status, _ := env.request(t, http.MethodGet, "/api/v1/probe/auth", aliceSecret, ""); status != http.StatusUnauthorized {
		t.Errorf("self-revoked token = %d, want 401", status)
	}
}

func TestHTTP_apiTokenCreateValidation(t *testing.T) {
	env := newTokenEnv(t, 50)
	env.user(t, "alice", auth.RoleEditor)
	cookie := env.login(t, "alice")

	past := env.now.Add(-time.Hour).Format(time.RFC3339)
	future := env.now.Add(24 * time.Hour).Format(time.RFC3339)

	tests := []struct {
		name string
		body string
		want int
	}{
		{"empty name", `{"name":""}`, http.StatusBadRequest},
		{"blank name", `{"name":"   "}`, http.StatusBadRequest},
		{"expiry in the past", `{"name":"cli","expires_at":"` + past + `"}`, http.StatusBadRequest},
		{"unknown field", `{"name":"cli","role":"admin"}`, http.StatusBadRequest},
		{"malformed json", `{`, http.StatusBadRequest},
		{"expiry in the future", `{"name":"cli","expires_at":"` + future + `"}`, http.StatusCreated},
		{"no expiry", `{"name":"forever"}`, http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if status, body := env.cookieRequest(t, http.MethodPost,
				"/api/v1/auth/tokens", cookie, tt.body); status != tt.want {
				t.Errorf("create %s = %d, want %d (body %s)", tt.name, status, tt.want, body)
			}
		})
	}
}

func TestHTTP_apiTokenCreateRateLimited(t *testing.T) {
	// The login limiter is shared, so the same key space caps token creation.
	env := newTokenEnv(t, 2)
	env.user(t, "alice", auth.RoleEditor)
	cookie := env.login(t, "alice")

	for i := range 2 {
		if status, body := env.cookieRequest(t, http.MethodPost,
			"/api/v1/auth/tokens", cookie, `{"name":"cli"}`); status != http.StatusCreated {
			t.Fatalf("create %d status = %d, want 201 (body %s)", i+1, status, body)
		}
	}
	if status, _ := env.cookieRequest(t, http.MethodPost,
		"/api/v1/auth/tokens", cookie, `{"name":"cli"}`); status != http.StatusTooManyRequests {
		t.Errorf("third create status = %d, want 429", status)
	}
	// Reading and revoking stay available while creation is throttled.
	if status, _ := env.cookieRequest(t, http.MethodGet, "/api/v1/auth/tokens", cookie, ""); status != http.StatusOK {
		t.Errorf("list while throttled = %d, want 200", status)
	}
}

func TestHTTP_apiTokenUnauthenticated(t *testing.T) {
	env := newTokenEnv(t, 50)

	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/auth/tokens", `{"name":"cli"}`},
		{http.MethodGet, "/api/v1/auth/tokens", ""},
		{http.MethodDelete, "/api/v1/auth/tokens/atsomething", ""},
	} {
		if status, _ := env.request(t, tc.method, tc.path, "", tc.body); status != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", tc.method, tc.path, status)
		}
	}
}
