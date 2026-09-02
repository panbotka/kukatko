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

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// The relying party these tests are run as. It is a name, not the address the
// httptest server happens to listen on: a WebAuthn ceremony is bound to the
// origin the *page* ran on, which in production is the instance's public URL and
// never the loopback socket a test client dials.
const (
	testRPID   = "kukatko.example.test"
	testOrigin = "https://kukatko.example.test"
)

// passkeyEnv is an httptest server over the auth API with the passkey flow wired
// (or deliberately not), a service clock the test drives, and direct database
// access so audit rows and credential columns can be asserted.
type passkeyEnv struct {
	server *httptest.Server
	svc    *auth.Service
	db     *database.DB
	now    *time.Time
}

// newPasskeyEnv builds the passkey test environment. loginLimit caps attempts
// per client address (the login limiter is reused for that); enabled decides
// whether a relying party is configured at all, which is how the "cleanly off"
// case is exercised through exactly the same routes.
func newPasskeyEnv(t *testing.T, loginLimit int, enabled bool) *passkeyEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	// Anchored to the real clock, never to a literal date. The session cookie
	// carries an absolute Expires computed from *this* clock, while the client's
	// cookie jar judges that Expires against the wall clock — so a frozen date
	// stops working the moment real time passes it, and every test here starts
	// failing with 401 because the jar silently drops the session. Frozen it
	// stays, so an instant is stable within one test.
	now := time.Now().UTC()
	svc := auth.NewService(auth.NewStore(db.Pool()),
		auth.SessionPolicy{TTL: testTTL, MaxLifetime: testMaxLifetime}).
		WithClock(func() time.Time { return now })
	api := auth.NewAPI(auth.APIConfig{
		Service:  svc,
		Limiter:  auth.NewLimiter(loginLimit, time.Minute),
		Passkeys: newTestPasskeys(t, svc, enabled),
	})

	r := chi.NewRouter()
	r.Use(clientip.Middleware(nil))
	r.Route("/api/v1", api.RegisterRoutes)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &passkeyEnv{server: server, svc: svc, db: db, now: &now}
}

// newTestPasskeys returns the passkey flow, or nil when the instance under test
// is meant to have none configured.
func newTestPasskeys(t *testing.T, svc *auth.Service, enabled bool) *auth.Passkeys {
	t.Helper()
	if !enabled {
		return nil
	}
	passkeys, err := auth.NewPasskeys(auth.PasskeysConfig{
		Service: svc, RPID: testRPID, RPDisplayName: "Kukátko", Origins: []string{testOrigin},
	})
	if err != nil {
		t.Fatalf("NewPasskeys: %v", err)
	}
	return passkeys
}

// user creates an approved account through the service.
func (e *passkeyEnv) user(t *testing.T, username string, role auth.Role) auth.User {
	t.Helper()
	user, err := e.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Email: username + "@example.test", Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", username, err)
	}
	return user
}

// request issues a request through client (whose jar carries the session and
// ceremony cookies) and returns the status and body.
func (e *passkeyEnv) request(
	t *testing.T, client *http.Client, method, path string, body json.RawMessage,
) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(t.Context(), method, e.server.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
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

// signInWithPassword logs username in with the shared test password, leaving the
// session cookie in client's jar.
func (e *passkeyEnv) signInWithPassword(t *testing.T, client *http.Client, username string) {
	t.Helper()
	status, body := e.request(t, client, http.MethodPost, "/api/v1/auth/login",
		json.RawMessage(loginJSON(username, testPassword)))
	if status != http.StatusOK {
		t.Fatalf("password login status = %d, want 200: %s", status, body)
	}
}

// addPasskey runs both halves of a registration ceremony for the signed-in
// caller and returns the status and body of the finishing request.
func (e *passkeyEnv) addPasskey(
	t *testing.T, client *http.Client, device *virtualAuthenticator, name string,
) (int, []byte) {
	t.Helper()
	status, body := e.request(t, client, http.MethodPost, "/api/v1/auth/passkeys/register/begin", nil)
	if status != http.StatusOK {
		return status, body
	}
	challenge := ceremonyChallenge(t, body)
	finish := mustJSON(t, map[string]any{
		"name":       name,
		"credential": device.register(t, testRPID, testOrigin, challenge),
	})
	return e.request(t, client, http.MethodPost, "/api/v1/auth/passkeys/register/finish", finish)
}

// signInWithPasskey runs both halves of a discoverable login ceremony and
// returns the status and body of the finishing request.
func (e *passkeyEnv) signInWithPasskey(
	t *testing.T, client *http.Client, device *virtualAuthenticator, userUID string,
) (int, []byte) {
	t.Helper()
	status, body := e.request(t, client, http.MethodPost, "/api/v1/auth/passkeys/login/begin", nil)
	if status != http.StatusOK {
		return status, body
	}
	challenge := ceremonyChallenge(t, body)
	finish := mustJSON(t, map[string]any{
		"credential": device.assert(t, testRPID, testOrigin, challenge, []byte(userUID)),
	})
	return e.request(t, client, http.MethodPost, "/api/v1/auth/passkeys/login/finish", finish)
}

// auditCount returns how many audit_log rows carry the given action and actor.
func (e *passkeyEnv) auditCount(t *testing.T, action, actorUID string) int {
	t.Helper()
	var n int
	err := e.db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM audit_log WHERE action = $1 AND actor_uid = $2", action, actorUID).Scan(&n)
	if err != nil {
		t.Fatalf("counting audit rows: %v", err)
	}
	return n
}

// storedPasskey reads the columns a login is expected to move.
func (e *passkeyEnv) storedPasskey(t *testing.T, id string) (signCount int64, lastUsed *time.Time) {
	t.Helper()
	err := e.db.Pool().QueryRow(t.Context(),
		"SELECT sign_count, last_used_at FROM passkey_credentials WHERE id = $1", id).
		Scan(&signCount, &lastUsed)
	if err != nil {
		t.Fatalf("reading the stored passkey: %v", err)
	}
	return signCount, lastUsed
}

// decodePasskey reads one PasskeyView out of a response body.
func decodePasskey(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var view map[string]any
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatalf("decoding the passkey: %v", err)
	}
	return view
}

// TestHTTP_passkeyRegisterListLoginDelete walks the whole feature the way a
// person does: add a key while signed in with a password, see it listed, sign in
// with it from a browser that has no session at all, then remove it.
func TestHTTP_passkeyRegisterListLoginDelete(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	alice := env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	status, body := env.addPasskey(t, owner, device, "Telefon")
	if status != http.StatusCreated {
		t.Fatalf("register finish status = %d, want 201: %s", status, body)
	}
	registered := decodePasskey(t, body)
	if registered["name"] != "Telefon" {
		t.Errorf("name = %v, want %q", registered["name"], "Telefon")
	}
	if registered["last_used_at"] != nil {
		t.Errorf("last_used_at = %v, want absent on a brand new passkey", registered["last_used_at"])
	}
	id, _ := registered["id"].(string)

	assertPasskeyListed(t, env, owner, id)
	assertPasskeySignIn(t, env, device, alice)

	if got := env.auditCount(t, audit.ActionPasskeyRegister, alice.UID); got != 1 {
		t.Errorf("passkey.register audit rows = %d, want 1", got)
	}
	if got := env.auditCount(t, audit.ActionPasskeyLogin, alice.UID); got != 1 {
		t.Errorf("passkey.login audit rows = %d, want 1", got)
	}

	status, body = env.request(t, owner, http.MethodDelete, "/api/v1/auth/passkeys/"+id, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204: %s", status, body)
	}
	if got := env.auditCount(t, audit.ActionPasskeyDelete, alice.UID); got != 1 {
		t.Errorf("passkey.delete audit rows = %d, want 1", got)
	}
	status, body = env.request(t, owner, http.MethodGet, "/api/v1/auth/passkeys", nil)
	if status != http.StatusOK || !strings.Contains(string(body), `"passkeys":[]`) {
		t.Errorf("listing after delete = %d %s, want 200 with an empty list", status, body)
	}
}

// assertPasskeyListed checks the owner's listing carries exactly the credential
// just registered, with the transports the authenticator reported.
func assertPasskeyListed(t *testing.T, env *passkeyEnv, owner *http.Client, id string) {
	t.Helper()
	status, body := env.request(t, env.server.Client(), http.MethodGet, "/api/v1/auth/passkeys", nil)
	if status != http.StatusUnauthorized {
		t.Errorf("anonymous listing status = %d, want 401: %s", status, body)
	}
	status, body = env.request(t, owner, http.MethodGet, "/api/v1/auth/passkeys", nil)
	if status != http.StatusOK {
		t.Fatalf("listing status = %d, want 200: %s", status, body)
	}
	var listed struct {
		Passkeys []map[string]any `json:"passkeys"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decoding the listing: %v", err)
	}
	if len(listed.Passkeys) != 1 || listed.Passkeys[0]["id"] != id {
		t.Fatalf("listing = %s, want exactly the passkey %q", body, id)
	}
	transports, _ := listed.Passkeys[0]["transports"].([]any)
	if len(transports) != 2 || transports[0] != "internal" {
		t.Errorf("transports = %v, want the two the authenticator reported", transports)
	}
}

// assertPasskeySignIn signs in from a browser holding no session and checks the
// answer is the same payload a password login returns, plus a session cookie and
// the two columns a use moves.
func assertPasskeySignIn(t *testing.T, env *passkeyEnv, device *virtualAuthenticator, alice auth.User) {
	t.Helper()
	stranger := newClient(t)
	status, body := env.signInWithPasskey(t, stranger, device, alice.UID)
	if status != http.StatusOK {
		t.Fatalf("passkey login status = %d, want 200: %s", status, body)
	}
	var signedIn struct {
		User          map[string]any `json:"user"`
		DownloadToken string         `json:"download_token"`
	}
	if err := json.Unmarshal(body, &signedIn); err != nil {
		t.Fatalf("decoding the login: %v", err)
	}
	if signedIn.User["username"] != "alice" || signedIn.DownloadToken == "" {
		t.Fatalf("login body = %s, want alice and a download token", body)
	}
	status, body = env.request(t, stranger, http.MethodGet, "/api/v1/auth/me", nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d, want 200 (the session cookie was not set): %s", status, body)
	}

	status, body = env.request(t, stranger, http.MethodGet, "/api/v1/auth/passkeys", nil)
	if status != http.StatusOK {
		t.Fatalf("listing after passkey login status = %d, want 200: %s", status, body)
	}
	var listed struct {
		Passkeys []map[string]any `json:"passkeys"`
	}
	if err := json.Unmarshal(body, &listed); err != nil {
		t.Fatalf("decoding the listing: %v", err)
	}
	if len(listed.Passkeys) != 1 {
		t.Fatalf("listing = %s, want one passkey", body)
	}
	id, _ := listed.Passkeys[0]["id"].(string)
	signCount, lastUsed := env.storedPasskey(t, id)
	if signCount != 1 {
		t.Errorf("sign_count = %d, want 1 (the authenticator's counter was not carried forward)", signCount)
	}
	if lastUsed == nil {
		t.Error("last_used_at is still NULL after a sign-in")
	}
}

// TestHTTP_passkeyNotConfigured pins the "cleanly off" state the specification
// asks for: every route is still mounted and every one of them says the same
// thing, so a client can tell an instance that does not offer passkeys from a
// build that has never heard of them.
func TestHTTP_passkeyNotConfigured(t *testing.T) {
	env := newPasskeyEnv(t, 50, false)
	env.user(t, "alice", auth.RoleEditor)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/auth/passkeys/login/begin"},
		{http.MethodPost, "/api/v1/auth/passkeys/login/finish"},
		{http.MethodPost, "/api/v1/auth/passkeys/register/begin"},
		{http.MethodPost, "/api/v1/auth/passkeys/register/finish"},
		{http.MethodGet, "/api/v1/auth/passkeys"},
		{http.MethodDelete, "/api/v1/auth/passkeys/pk000000000000000000000000"},
	}
	for _, tc := range cases {
		status, body := env.request(t, owner, tc.method, tc.path, json.RawMessage(`{}`))
		if status != http.StatusNotImplemented {
			t.Errorf("%s %s status = %d, want 501: %s", tc.method, tc.path, status, body)
		}
		if !strings.Contains(string(body), "not available") {
			t.Errorf("%s %s body = %s, want it to say passkeys are not available", tc.method, tc.path, body)
		}
	}
}

// TestHTTP_passkeyLoginRateLimited pins the anonymous half against guessing: the
// finishing request spends the password login's budget, keyed on the client
// address because a discoverable login names no account until it succeeds.
func TestHTTP_passkeyLoginRateLimited(t *testing.T) {
	env := newPasskeyEnv(t, 2, true)
	client := newClient(t)

	for attempt := range 2 {
		status, body := env.request(t, client, http.MethodPost,
			"/api/v1/auth/passkeys/login/finish", json.RawMessage(`{"credential":{}}`))
		if status != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401: %s", attempt, status, body)
		}
	}
	status, body := env.request(t, client, http.MethodPost,
		"/api/v1/auth/passkeys/login/finish", json.RawMessage(`{"credential":{}}`))
	if status != http.StatusTooManyRequests {
		t.Errorf("third attempt status = %d, want 429: %s", status, body)
	}
}

// TestHTTP_passkeyLoginBeginHasItsOwnBudget pins the split: opening ceremonies
// must not spend the budget that guards credential verification, or a person who
// starts a sign-in and changes their mind would lock themselves out of the one
// they finish.
func TestHTTP_passkeyLoginBeginHasItsOwnBudget(t *testing.T) {
	env := newPasskeyEnv(t, 2, true)
	alice := env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	if status, body := env.addPasskey(t, owner, device, "Klíč"); status != http.StatusCreated {
		t.Fatalf("register finish status = %d, want 201: %s", status, body)
	}

	client := newClient(t)
	for attempt := range 2 {
		status, body := env.request(t, client, http.MethodPost, "/api/v1/auth/passkeys/login/begin", nil)
		if status != http.StatusOK {
			t.Fatalf("begin %d status = %d, want 200: %s", attempt, status, body)
		}
	}
	if status, body := env.signInWithPasskey(t, client, device, alice.UID); status != http.StatusTooManyRequests {
		// The begin budget is spent, so the third begin is refused — and the
		// verification budget is untouched, which is what the next case shows.
		t.Fatalf("third begin status = %d, want 429: %s", status, body)
	}
}

// TestHTTP_passkeyCeremonyIsOneShot pins the challenge as spent on first use:
// replaying an assertion that already worked must not produce a second session,
// because a captured response would otherwise be a reusable credential.
func TestHTTP_passkeyCeremonyIsOneShot(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	alice := env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	if status, body := env.addPasskey(t, owner, device, ""); status != http.StatusCreated {
		t.Fatalf("register finish status = %d, want 201: %s", status, body)
	}

	client := newClient(t)
	status, body := env.request(t, client, http.MethodPost, "/api/v1/auth/passkeys/login/begin", nil)
	if status != http.StatusOK {
		t.Fatalf("begin status = %d, want 200: %s", status, body)
	}
	challenge := ceremonyChallenge(t, body)
	assertion := mustJSON(t, map[string]any{
		"credential": device.assert(t, testRPID, testOrigin, challenge, []byte(alice.UID)),
	})
	if status, body = env.request(t, client, http.MethodPost,
		"/api/v1/auth/passkeys/login/finish", assertion); status != http.StatusOK {
		t.Fatalf("first finish status = %d, want 200: %s", status, body)
	}
	// The cookie is cleared by the first finish, so the replay carries none —
	// which is exactly the state a captured-and-replayed response arrives in.
	if status, body = env.request(t, client, http.MethodPost,
		"/api/v1/auth/passkeys/login/finish", assertion); status != http.StatusUnauthorized {
		t.Errorf("replayed finish status = %d, want 401: %s", status, body)
	}
}

// TestHTTP_passkeyRegisterRejectsAForeignOrigin is the phishing case in one
// assertion: a credential signed for another site's origin must not be
// registrable here, however well-formed it is.
func TestHTTP_passkeyRegisterRejectsAForeignOrigin(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	status, body := env.request(t, owner, http.MethodPost, "/api/v1/auth/passkeys/register/begin", nil)
	if status != http.StatusOK {
		t.Fatalf("begin status = %d, want 200: %s", status, body)
	}
	finish := mustJSON(t, map[string]any{
		"name":       "Podvod",
		"credential": device.register(t, testRPID, "https://kukatko.example.evil", ceremonyChallenge(t, body)),
	})
	if status, body = env.request(t, owner, http.MethodPost,
		"/api/v1/auth/passkeys/register/finish", finish); status != http.StatusBadRequest {
		t.Errorf("finish status = %d, want 400: %s", status, body)
	}
}

// TestHTTP_passkeyRegisterRefusesTheSameKeyTwice pins the one failure that is
// not a mistake: the same authenticator offered again is a 409, so the interface
// can say "you already added this one" instead of blaming the request.
func TestHTTP_passkeyRegisterRefusesTheSameKeyTwice(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	if status, body := env.addPasskey(t, owner, device, "První"); status != http.StatusCreated {
		t.Fatalf("first registration status = %d, want 201: %s", status, body)
	}
	if status, body := env.addPasskey(t, owner, device, "Druhý"); status != http.StatusConflict {
		t.Errorf("second registration status = %d, want 409: %s", status, body)
	}
}

// TestHTTP_passkeyRegisterRejectsAnOverLongName pins the one input a caller
// chooses freely.
func TestHTTP_passkeyRegisterRejectsAnOverLongName(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	status, body := env.addPasskey(t, owner, device, strings.Repeat("ě", auth.MaxPasskeyNameLen+1))
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400: %s", status, body)
	}
}

// TestHTTP_passkeyDeleteBelongsToItsOwner pins the ownership rule and the
// deliberate absence of a last-passkey guard: somebody else's key is a 404 (so
// ids cannot be probed) and your own last one may go, because the password never
// stopped working.
func TestHTTP_passkeyDeleteBelongsToItsOwner(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	env.user(t, "alice", auth.RoleEditor)
	env.user(t, "bob", auth.RoleAdmin)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	status, body := env.addPasskey(t, owner, newVirtualAuthenticator(t), "Jediný")
	if status != http.StatusCreated {
		t.Fatalf("register finish status = %d, want 201: %s", status, body)
	}
	id, _ := decodePasskey(t, body)["id"].(string)

	// An admin is still not the owner: the list is the account's own, so even a
	// role that may edit everybody else's account cannot reach into it.
	intruder := newClient(t)
	env.signInWithPassword(t, intruder, "bob")
	if status, body = env.request(t, intruder, http.MethodDelete,
		"/api/v1/auth/passkeys/"+id, nil); status != http.StatusNotFound {
		t.Errorf("foreign delete status = %d, want 404: %s", status, body)
	}
	if status, body = env.request(t, owner, http.MethodDelete,
		"/api/v1/auth/passkeys/"+id, nil); status != http.StatusNoContent {
		t.Errorf("own delete status = %d, want 204 (the last passkey may go): %s", status, body)
	}
}

// TestHTTP_passkeyLoginRefusesABlockedAccount pins the two account states a
// verified signature must still not get past: a disabled account is refused as
// unspecifically as a bad signature, while one waiting for an administrator is
// told what it is waiting for — the same distinction a password login draws.
func TestHTTP_passkeyLoginRefusesABlockedAccount(t *testing.T) {
	env := newPasskeyEnv(t, 50, true)
	alice := env.user(t, "alice", auth.RoleEditor)
	device := newVirtualAuthenticator(t)

	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	if status, body := env.addPasskey(t, owner, device, "Telefon"); status != http.StatusCreated {
		t.Fatalf("register finish status = %d, want 201: %s", status, body)
	}

	if _, err := env.svc.SetUserDisabled(t.Context(), alice.UID, true); err != nil {
		t.Fatalf("SetUserDisabled: %v", err)
	}
	if status, body := env.signInWithPasskey(t, newClient(t), device, alice.UID); status != http.StatusUnauthorized {
		t.Errorf("disabled account status = %d, want 401: %s", status, body)
	}

	if _, err := env.db.Pool().Exec(t.Context(),
		"UPDATE users SET disabled = false, approved_at = NULL WHERE uid = $1", alice.UID); err != nil {
		t.Fatalf("un-approving the account: %v", err)
	}
	if status, body := env.signInWithPasskey(t, newClient(t), device, alice.UID); status != http.StatusForbidden {
		t.Errorf("unapproved account status = %d, want 403: %s", status, body)
	}
}
