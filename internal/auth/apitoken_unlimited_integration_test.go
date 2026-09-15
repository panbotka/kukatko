//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
)

// unlimitedOf reads the `unlimited` field out of a token JSON object, failing
// the test when it is missing — the field is not `omitempty`, so every token the
// API returns must carry it.
func unlimitedOf(t *testing.T, token map[string]any) bool {
	t.Helper()
	value, ok := token["unlimited"]
	if !ok {
		t.Fatalf("token JSON %v carries no unlimited field", token)
	}
	flag, ok := value.(bool)
	if !ok {
		t.Fatalf("token unlimited = %v (%T), want a bool", value, value)
	}
	return flag
}

// tokenByID returns the caller's token with the given id from GET /auth/tokens.
func (e *tokenEnv) tokenByID(t *testing.T, cookie *http.Cookie, id string) map[string]any {
	t.Helper()
	status, body := e.cookieRequest(t, http.MethodGet, "/api/v1/auth/tokens", cookie, "")
	if status != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body %s)", status, body)
	}
	var listed struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decoding list body: %v", err)
	}
	for _, token := range listed.Tokens {
		if token["id"] == id {
			return token
		}
	}
	t.Fatalf("token %q is not in the caller's listing", id)
	return nil
}

// countTokens returns how many api_tokens rows belong to userUID, so a refused
// creation can be shown to have created nothing at all.
func (e *tokenEnv) countTokens(t *testing.T, userUID string) int {
	t.Helper()
	var n int
	if err := e.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM api_tokens WHERE user_uid = $1", userUID).Scan(&n); err != nil {
		t.Fatalf("counting tokens: %v", err)
	}
	return n
}

// TestHTTP_apiTokenUnlimitedCreate verifies that only an admin may mint a token
// exempt from the rate limits: a non-admin asking for one is refused with 403
// and ends up with no token whatsoever, while an admin's token comes back
// carrying the flag and an audit entry recording it.
func TestHTTP_apiTokenUnlimitedCreate(t *testing.T) {
	env := newTokenEnv(t, 50)
	editor := env.user(t, "editor", auth.RoleEditor)
	admin := env.user(t, "admin", auth.RoleAdmin)

	editorCookie := env.login(t, "editor")
	status, body := env.cookieRequest(t, http.MethodPost, "/api/v1/auth/tokens", editorCookie,
		`{"name":"agent","unlimited":true}`)
	if status != http.StatusForbidden {
		t.Fatalf("editor create status = %d, want 403 (body %s)", status, body)
	}
	if n := env.countTokens(t, editor.UID); n != 0 {
		t.Errorf("refused creation left %d tokens behind, want 0", n)
	}
	if n := env.auditCount(t, audit.ActionAPITokenCreate, editor.UID); n != 0 {
		t.Errorf("refused creation wrote %d audit rows, want 0", n)
	}

	// The same editor may still mint an ordinary, throttled token.
	status, body = env.cookieRequest(t, http.MethodPost, "/api/v1/auth/tokens", editorCookie, `{"name":"plain"}`)
	if status != http.StatusCreated {
		t.Fatalf("editor plain create status = %d, want 201 (body %s)", status, body)
	}
	var plain createdToken
	if err := json.Unmarshal(body, &plain); err != nil {
		t.Fatalf("decoding create body: %v", err)
	}
	if unlimitedOf(t, plain.Token) {
		t.Error("a token minted without the flag came back unlimited")
	}

	adminCookie := env.login(t, "admin")
	status, body = env.cookieRequest(t, http.MethodPost, "/api/v1/auth/tokens", adminCookie,
		`{"name":"agent","unlimited":true}`)
	if status != http.StatusCreated {
		t.Fatalf("admin create status = %d, want 201 (body %s)", status, body)
	}
	var created createdToken
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decoding create body: %v", err)
	}
	if !unlimitedOf(t, created.Token) {
		t.Error("admin asked for an unlimited token and got a throttled one")
	}
	id, _ := created.Token["id"].(string)
	if got := unlimitedOf(t, env.tokenByID(t, adminCookie, id)); !got {
		t.Error("the listing reports the fresh token as throttled")
	}
	if n := env.auditCount(t, audit.ActionAPITokenCreate, admin.UID); n != 1 {
		t.Errorf("admin creation wrote %d audit rows, want 1", n)
	}
}

// TestHTTP_apiTokenUnlimitedPatch verifies the toggle: an admin turns the flag
// on and off again on their own token, each change leaving an audit row and a
// repeat of the same value leaving none; a non-admin is refused with 403; and
// somebody else's token — or one that does not exist — is a 404 even for an
// admin.
func TestHTTP_apiTokenUnlimitedPatch(t *testing.T) {
	env := newTokenEnv(t, 50)
	editor := env.user(t, "editor", auth.RoleEditor)
	admin := env.user(t, "admin", auth.RoleAdmin)

	editorToken, _ := env.mintToken(t, editor, "editor's", nil)
	adminToken, _ := env.mintToken(t, admin, "admin's", nil)

	editorCookie := env.login(t, "editor")
	adminCookie := env.login(t, "admin")
	path := func(id string) string { return "/api/v1/auth/tokens/" + id }

	// A non-admin may not touch the flag, not even on their own token.
	status, body := env.cookieRequest(t, http.MethodPatch, path(editorToken.ID), editorCookie, `{"unlimited":true}`)
	if status != http.StatusForbidden {
		t.Fatalf("editor patch status = %d, want 403 (body %s)", status, body)
	}
	if got := unlimitedOf(t, env.tokenByID(t, editorCookie, editorToken.ID)); got {
		t.Error("a refused patch turned the flag on anyway")
	}

	// Somebody else's token is a 404 for an admin too — revocation, not this, is
	// the power an admin holds over another person's credential.
	status, body = env.cookieRequest(t, http.MethodPatch, path(editorToken.ID), adminCookie, `{"unlimited":true}`)
	if status != http.StatusNotFound {
		t.Fatalf("admin patching a foreign token: status = %d, want 404 (body %s)", status, body)
	}
	status, body = env.cookieRequest(t, http.MethodPatch, path("at_nosuch"), adminCookie, `{"unlimited":true}`)
	if status != http.StatusNotFound {
		t.Fatalf("admin patching an unknown token: status = %d, want 404 (body %s)", status, body)
	}

	// On, then on again (a no-op), then off.
	for i, step := range []struct {
		value      string
		want       bool
		wantAudits int
	}{
		{value: `{"unlimited":true}`, want: true, wantAudits: 1},
		{value: `{"unlimited":true}`, want: true, wantAudits: 1},
		{value: `{"unlimited":false}`, want: false, wantAudits: 2},
	} {
		status, body = env.cookieRequest(t, http.MethodPatch, path(adminToken.ID), adminCookie, step.value)
		if status != http.StatusNoContent {
			t.Fatalf("step %d: patch status = %d, want 204 (body %s)", i, status, body)
		}
		if got := unlimitedOf(t, env.tokenByID(t, adminCookie, adminToken.ID)); got != step.want {
			t.Errorf("step %d: unlimited = %v, want %v", i, got, step.want)
		}
		if n := env.auditCount(t, audit.ActionAPITokenUpdate, admin.UID); n != step.wantAudits {
			t.Errorf("step %d: %d audit rows, want %d", i, n, step.wantAudits)
		}
	}
}

// TestHTTP_unlimitedTokenSkipsTheRateLimiter is the acceptance test of the whole
// feature over the real middleware chain (write guard, then the limiter with the
// real exemption predicate): an unlimited token runs well past the burst without
// a 429, a plain token of the very same person is cut off once the burst is
// spent, and the admin's own session cookie is throttled exactly like anybody
// else's — the exemption belongs to the credential, not to the role.
func TestHTTP_unlimitedTokenSkipsTheRateLimiter(t *testing.T) {
	env := newTokenEnv(t, 50)
	admin := env.user(t, "admin", auth.RoleAdmin)
	adminCookie := env.login(t, "admin")

	status, body := env.cookieRequest(t, http.MethodPost, "/api/v1/auth/tokens", adminCookie,
		`{"name":"agent","unlimited":true}`)
	if status != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 (body %s)", status, body)
	}
	var created createdToken
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decoding create body: %v", err)
	}
	_, plainSecret := env.mintToken(t, admin, "plain", nil)

	// Well past the burst, every one of them allowed.
	const beyondBurst = throttledProbeBurst*3 + 1
	for i := range beyondBurst {
		status, body = env.request(t, http.MethodGet, "/api/v1/probe/throttled", created.Secret, "")
		if status != http.StatusOK {
			t.Fatalf("unlimited token request %d: status = %d, want 200 (body %s)", i, status, body)
		}
	}

	// The plain token spends the burst and is then refused — and the exempt
	// requests above must not have eaten any of it.
	for i := range throttledProbeBurst {
		status, body = env.request(t, http.MethodGet, "/api/v1/probe/throttled", plainSecret, "")
		if status != http.StatusOK {
			t.Fatalf("plain token request %d: status = %d, want 200 (body %s)", i, status, body)
		}
	}
	status, _ = env.request(t, http.MethodGet, "/api/v1/probe/throttled", plainSecret, "")
	if status != http.StatusTooManyRequests {
		t.Fatalf("plain token past the burst: status = %d, want 429", status)
	}

	// The admin's browser session shares that exhausted bucket: a cookie is never
	// exempt, whatever the role.
	status, _ = env.cookieRequest(t, http.MethodGet, "/api/v1/probe/throttled", adminCookie, "")
	if status != http.StatusTooManyRequests {
		t.Fatalf("admin cookie after the bucket ran dry: status = %d, want 429", status)
	}
	// The unlimited token still passes through the empty bucket.
	status, _ = env.request(t, http.MethodGet, "/api/v1/probe/throttled", created.Secret, "")
	if status != http.StatusOK {
		t.Fatalf("unlimited token after the bucket ran dry: status = %d, want 200", status)
	}
}
