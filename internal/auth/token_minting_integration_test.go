//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
)

// These tests document what an expiring API token may mint today (SEC-022 in
// docs/SECURITY_AUDIT.md). A bearer principal carries the owner's full standing
// and nothing about the token it came from: neither its id nor its expiry. The
// credential-management routes need only RequireAuth, so a token that expires in
// an hour mints a token that never expires and registers a passkey, and both
// outlive the token that made them, whether it expires or is revoked. The role
// never grows; what is lost is the parent's bounded lifetime and its single
// revocation. The tests pin that behaviour, so whichever fix SEC-022 settles on
// has to change them on purpose.

// parentLifetime is how long the presenting token lives in these tests: the
// "one hour for this job" token an owner hands to a script or an agent.
const parentLifetime = time.Hour

// mintedToken is the part of the POST /auth/tokens response these tests read.
type mintedToken struct {
	Token struct {
		ID        string     `json:"id"`
		ExpiresAt *time.Time `json:"expires_at"`
		Unlimited bool       `json:"unlimited"`
	} `json:"token"`
	Secret string `json:"secret"`
}

// mint creates a token through client with the given body and returns the
// decoded response. It fails the test unless the server answers 201.
func mint(t *testing.T, env *passkeyEnv, client *http.Client, body map[string]any) mintedToken {
	t.Helper()
	status, raw := env.request(t, client, http.MethodPost, "/api/v1/auth/tokens", mustJSON(t, body))
	if status != http.StatusCreated {
		t.Fatalf("POST /auth/tokens %v = %d, want 201: %s", body, status, raw)
	}
	var minted mintedToken
	if err := json.Unmarshal(raw, &minted); err != nil {
		t.Fatalf("decoding the minted token: %v", err)
	}
	if minted.Secret == "" || minted.Token.ID == "" {
		t.Fatalf("the minted token carries no id or secret: %s", raw)
	}
	return minted
}

// storedExpiry reads a token's expires_at straight from the database, so the
// assertion does not rest on the response's omitempty.
func storedExpiry(t *testing.T, env *passkeyEnv, id string) *time.Time {
	t.Helper()
	var expiresAt *time.Time
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT expires_at FROM api_tokens WHERE id = $1`, id).Scan(&expiresAt); err != nil {
		t.Fatalf("reading the token's expiry: %v", err)
	}
	return expiresAt
}

// parentSetup is one account, alice, signed in from her own browser, and an
// hour-long token minted from that browser.
type parentSetup struct {
	env   *passkeyEnv
	alice auth.User
	// owner is alice's browser, for acting as the owner (revoking the token).
	owner *http.Client
	// parent is the hour-long token's metadata.
	parent mintedToken
	// presenter is a client that presents the parent token and nothing else.
	presenter *http.Client
}

// newParentSetup builds a parentSetup on the recovery environment, with alice
// holding role.
func newParentSetup(t *testing.T, role auth.Role) parentSetup {
	t.Helper()
	env := newRecoveryEnv(t)
	alice := env.user(t, "alice", role)
	owner := newClient(t)
	env.signInWithPassword(t, owner, "alice")
	parent := mint(t, env, owner, map[string]any{
		"name": "one hour for a script", "expires_at": env.now.Add(parentLifetime),
	})
	return parentSetup{
		env: env, alice: alice, owner: owner, parent: parent,
		presenter: bearerClient(t, parent.Secret),
	}
}

// TestTokenMinting_expiringTokenMintsLongerLivedToken presents an hour-long
// token and asks for a child with no expiry, and for one that outlives the
// parent by a year. Both are 201: the service checks only that an expiry is not
// in the past, never against the presenting credential.
func TestTokenMinting_expiringTokenMintsLongerLivedToken(t *testing.T) {
	s := newParentSetup(t, auth.RoleEditor)
	env, parent, presenter := s.env, s.parent, s.presenter
	parentExpiry := storedExpiry(t, env, parent.Token.ID)
	if parentExpiry == nil {
		t.Fatal("the parent token has no expiry; the setup is wrong")
	}

	// Current behaviour (SEC-022): a nil expiry is accepted from a token principal.
	forever := mint(t, env, presenter, map[string]any{"name": "never expires"})
	if forever.Token.ExpiresAt != nil {
		t.Errorf("the child's expires_at in the response = %v, want none", forever.Token.ExpiresAt)
	}
	if got := storedExpiry(t, env, forever.Token.ID); got != nil {
		t.Errorf("the child's stored expires_at = %v, want NULL (never expires)", got)
	}

	// Current behaviour (SEC-022): nor is a later expiry capped at the parent's.
	later := env.now.Add(365 * 24 * time.Hour)
	longer := mint(t, env, presenter, map[string]any{"name": "a year", "expires_at": later})
	got := storedExpiry(t, env, longer.Token.ID)
	if got == nil || !got.After(*parentExpiry) {
		t.Errorf("the child's stored expires_at = %v, want later than the parent's %v", got, *parentExpiry)
	}

	if got := meStatus(t, env, bearerClient(t, forever.Secret)); got != http.StatusOK {
		t.Errorf("GET /auth/me with the never-expiring child = %d, want 200", got)
	}
}

// TestTokenMinting_childOutlivesExpiredParent mints a never-expiring child with
// an hour-long parent, then moves the clock past the parent's expiry. The
// parent is refused; the child still authenticates.
func TestTokenMinting_childOutlivesExpiredParent(t *testing.T) {
	s := newParentSetup(t, auth.RoleEditor)
	env, presenter := s.env, s.presenter
	child := bearerClient(t, mint(t, env, presenter, map[string]any{"name": "child"}).Secret)

	*env.now = env.now.Add(parentLifetime + time.Minute)

	if got := meStatus(t, env, presenter); got != http.StatusUnauthorized {
		t.Errorf("GET /auth/me with the expired parent = %d, want 401", got)
	}
	// Current behaviour (SEC-022): the parent's expiry does not bind the child.
	if got := meStatus(t, env, child); got != http.StatusOK {
		t.Errorf("GET /auth/me with the child after the parent expired = %d, want 200", got)
	}
}

// TestTokenMinting_childOutlivesRevokedParent mints a child with a token, then
// has the owner revoke the parent from their browser, the documented response
// to a leaked token. The parent is refused; the child still authenticates and
// is the one live token left.
func TestTokenMinting_childOutlivesRevokedParent(t *testing.T) {
	s := newParentSetup(t, auth.RoleEditor)
	env, presenter := s.env, s.presenter
	child := bearerClient(t, mint(t, env, presenter, map[string]any{"name": "child"}).Secret)

	status, body := env.request(t, s.owner, http.MethodDelete, "/api/v1/auth/tokens/"+s.parent.Token.ID, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /auth/tokens/{parent} = %d, want 204: %s", status, body)
	}

	if got := meStatus(t, env, presenter); got != http.StatusUnauthorized {
		t.Errorf("GET /auth/me with the revoked parent = %d, want 401", got)
	}
	// Current behaviour (SEC-022): revoking the parent does not reach the child.
	if got := meStatus(t, env, child); got != http.StatusOK {
		t.Errorf("GET /auth/me with the child after the parent was revoked = %d, want 200", got)
	}
	if got := countRows(t, env, liveTokensQuery, s.alice.UID); got != 1 {
		t.Errorf("unrevoked tokens after revoking the parent = %d, want 1 (the child)", got)
	}
}

// TestTokenMinting_expiringTokenRegistersPasskey completes a passkey
// registration with nothing but an hour-long token (virtual authenticator), lets
// the token expire, and signs in with the passkey from an empty browser. The
// sign-in yields an ordinary session: a bounded bearer credential has turned into
// an unbounded interactive one.
func TestTokenMinting_expiringTokenRegistersPasskey(t *testing.T) {
	s := newParentSetup(t, auth.RoleEditor)
	env, presenter := s.env, s.presenter
	device := newVirtualAuthenticator(t)
	// Current behaviour (SEC-022): passkey registration accepts a bearer principal.
	if status, body := env.addPasskey(t, presenter, device, "From a token"); status != http.StatusCreated {
		t.Fatalf("register a passkey with an expiring token = %d, want 201: %s", status, body)
	}

	*env.now = env.now.Add(parentLifetime + time.Minute)
	if got := meStatus(t, env, presenter); got != http.StatusUnauthorized {
		t.Errorf("GET /auth/me with the expired token = %d, want 401", got)
	}

	browser := newClient(t)
	if status, body := env.signInWithPasskey(t, browser, device, s.alice.UID); status != http.StatusOK {
		t.Fatalf("passkey login after the token expired = %d, want 200: %s", status, body)
	}
	if got := meStatus(t, env, browser); got != http.StatusOK {
		t.Errorf("GET /auth/me with the passkey's session = %d, want 200", got)
	}
}

// TestTokenMinting_adminTokenMintsUnlimitedChild shows the rate-limit exemption
// follows the owner's role, not the presenting credential: an admin's throttled,
// expiring token mints an unlimited child that never expires. The exemption is
// documented as belonging to "one revocable credential", and it still does —
// but that credential can be a fresh one minted by any other.
func TestTokenMinting_adminTokenMintsUnlimitedChild(t *testing.T) {
	s := newParentSetup(t, auth.RoleAdmin)
	env, parent, presenter := s.env, s.parent, s.presenter
	if parent.Token.Unlimited {
		t.Fatal("the parent token is unlimited; the setup is wrong")
	}

	// Current behaviour (SEC-022): the exemption is the owner's to grant, from any credential.
	child := mint(t, env, presenter, map[string]any{"name": "agent", "unlimited": true})
	if !child.Token.Unlimited {
		t.Error("the child minted by a throttled admin token is not unlimited")
	}
	if got := storedExpiry(t, env, child.Token.ID); got != nil {
		t.Errorf("the unlimited child's stored expires_at = %v, want NULL", got)
	}
}
