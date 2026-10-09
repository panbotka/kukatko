//go:build integration

package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
)

// These tests pin the behaviour SEC-023 in docs/SECURITY_AUDIT.md describes:
// the only CSRF defence on a cookie-authenticated write is the session cookie's
// SameSite=Strict attribute. Nothing on the request path looks at Content-Type,
// Origin or Sec-Fetch-Site, so a body a browser sends without a CORS preflight —
// `text/plain`, from `fetch(…, {mode: "no-cors"})` or an HTML form with
// `enctype="text/plain"` — is decoded as JSON exactly like the SPA's. They are
// kept on purpose: a fix has to flip them deliberately.

// siblingOrigin stands in for another host under the same registrable domain as
// the instance: same-site, so a SameSite=Strict cookie rides along, but not the
// same origin.
const siblingOrigin = "https://sibling.example.test"

// siblingHeaders are the headers a browser attaches to a request a page on
// siblingOrigin sends to the instance without a preflight.
func siblingHeaders() map[string]string {
	return map[string]string{
		"Content-Type":   "text/plain;charset=UTF-8",
		"Origin":         siblingOrigin,
		"Sec-Fetch-Site": "same-site",
		"Sec-Fetch-Mode": "no-cors",
	}
}

// textPlainForm encodes one field the way a browser serialises an HTML form
// with enctype="text/plain": the name, "=", the value and a CRLF, none of it
// escaped. Splitting a JSON document at a "=" inside a string value gives a
// form whose body is that document plus a trailing CRLF.
func textPlainForm(name, value string) string {
	return name + "=" + value + "\r\n"
}

// doRaw issues a request with exactly the given headers (no implicit
// Content-Type) and returns the status and the body.
func (e *httpEnv) doRaw(t *testing.T, client *http.Client,
	method, path, body string, headers map[string]string,
) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
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

// assertCreatedAdmin checks a 201 from POST /admin/users and that the account it
// reports, and the one stored, carries the admin role.
func (e *httpEnv) assertCreatedAdmin(t *testing.T, status int, body []byte, username string) map[string]any {
	t.Helper()
	if status != http.StatusCreated {
		t.Fatalf("POST /admin/users status = %d, want 201 (body %s)", status, body)
	}
	created := decodeUser(t, body)
	if created["role"] != string(auth.RoleAdmin) {
		t.Fatalf("created role = %v, want admin", created["role"])
	}
	stored, err := e.store.GetUserByUsername(t.Context(), username)
	if err != nil {
		t.Fatalf("GetUserByUsername(%q): %v", username, err)
	}
	if stored.Role != auth.RoleAdmin {
		t.Fatalf("stored role = %q, want admin", stored.Role)
	}
	return created
}

// TestCSRF_textPlainJSONCreatesAdmin shows that the strongest blind write, an
// admin creating an admin account, accepts a raw JSON body declared as
// text/plain — what `fetch(url, {method: "POST", mode: "no-cors", credentials:
// "include", body: json})` sends from a same-site page — with a sibling Origin
// and `Sec-Fetch-Site: same-site` on it.
func TestCSRF_textPlainJSONCreatesAdmin(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "victim-admin", auth.RoleAdmin)
	victim := env.loginClient(t, "victim-admin")

	body := adminUserBody(t, map[string]any{
		"username": "csrf-fetch",
		"email":    "csrf-fetch@example.test",
		"password": testPassword, // attacker-chosen; loginClient signs in with it
		"role":     string(auth.RoleAdmin),
	})
	status, data := env.doRaw(t, victim, http.MethodPost, "/api/v1/admin/users", body, siblingHeaders())
	env.assertCreatedAdmin(t, status, data, "csrf-fetch")

	// The planted account is a working sign-in for whoever chose its password.
	env.loginClient(t, "csrf-fetch")
}

// TestCSRF_textPlainFormCreatesAdmin shows the same write from a plain HTML form
// with enctype="text/plain", which needs no script at all. The JSON document is
// split at a "=" inside the note's value, so the decoder (which refuses unknown
// fields) sees only known ones, and the trailing CRLF after the document is
// never read.
func TestCSRF_textPlainFormCreatesAdmin(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "victim-admin", auth.RoleAdmin)
	victim := env.loginClient(t, "victim-admin")

	name := `{"username":"csrf-form","email":"csrf-form@example.test",` +
		`"password":"attacker-chosen-pass","role":"admin","note":"`
	body := textPlainForm(name, `"}`)
	if !json.Valid([]byte(strings.TrimSpace(body))) {
		t.Fatalf("form body is not a JSON document: %q", body)
	}

	status, data := env.doRaw(t, victim, http.MethodPost, "/api/v1/admin/users", body, siblingHeaders())
	created := env.assertCreatedAdmin(t, status, data, "csrf-form")
	assertNote(t, created, "=")
}

// TestCSRF_textPlainFormLogsIn shows that the public login endpoint decodes a
// text/plain form body too and answers it with a session cookie: the request
// half of login CSRF. Whether the browser keeps that Strict cookie from a
// cross-site top-level POST is a browser fact, recorded in SEC-023, not
// something an HTTP test can show.
func TestCSRF_textPlainFormLogsIn(t *testing.T) {
	env := newHTTPEnv(t, 50)
	// The attacker's own account, with a password that carries the "=" the
	// form encoding needs.
	if _, err := env.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "attacker", Email: "attacker@example.test", Password: "attacker-pass=", Role: auth.RoleViewer,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	body := textPlainForm(`{"username":"attacker","password":"attacker-pass`, `"}`)
	headers := siblingHeaders()
	headers["Origin"] = "https://unrelated.example.org"
	headers["Sec-Fetch-Site"] = "cross-site"
	headers["Sec-Fetch-Mode"] = "navigate"

	client := newClient(t)
	status, data := env.doRaw(t, client, http.MethodPost, "/api/v1/auth/login", body, headers)
	if status != http.StatusOK {
		t.Fatalf("POST /auth/login status = %d, want 200 (body %s)", status, data)
	}
	assertStatus(t, env, client, http.MethodGet, "/api/v1/auth/me", "", http.StatusOK)
}
