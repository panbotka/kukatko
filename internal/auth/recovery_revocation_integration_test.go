//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests document what account recovery revokes today, and what it does
// not (SEC-021 in docs/SECURITY_AUDIT.md). Each of the three recovery paths — a
// self-service password change, an administrator setting a password, and a
// consumed reset link — deletes session rows only. An API token and a passkey
// minted before the recovery keep working after it. The tests pin that
// behaviour, so whichever fix SEC-021 settles on has to change them on purpose.

// recoveredPassword is the password every recovery path below sets.
const recoveredPassword = "a-password-after-recovery"

// newRecoveryEnv is the passkey test environment with the administrator's reset
// link wired as well, so all three recovery paths run through one server. It
// wires no mail: a reset link is still issued and handed to the administrator,
// which is all these tests need.
func newRecoveryEnv(t *testing.T) *passkeyEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	// The real clock, frozen: see newPasskeyEnv for why never a literal date.
	now := time.Now().UTC()
	svc := auth.NewService(auth.NewStore(db.Pool()),
		auth.SessionPolicy{TTL: testTTL, MaxLifetime: testMaxLifetime}).
		WithClock(func() time.Time { return now })
	api := auth.NewAPI(auth.APIConfig{
		Service:  svc,
		Limiter:  auth.NewLimiter(50, time.Minute),
		Passkeys: newTestPasskeys(t, svc, true),
		PasswordReset: auth.NewPasswordReset(auth.PasswordResetConfig{
			Service:  svc,
			LinkBase: testResetLinkBase,
		}),
	})

	r := chi.NewRouter()
	r.Use(clientip.Middleware(nil))
	r.Route("/api/v1", api.RegisterRoutes)

	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &passkeyEnv{server: server, svc: svc, db: db, now: &now}
}

// bearerTransport adds an API token to every request it carries.
type bearerTransport struct {
	secret string
}

// RoundTrip sets the Authorization header on a clone of req and sends it.
func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+b.secret)
	return http.DefaultTransport.RoundTrip(clone)
}

// bearerClient returns a client that authenticates with the API token secret
// alone. It keeps a cookie jar only because a passkey ceremony carries its
// state in a cookie; no session cookie is ever set on it.
func bearerClient(t *testing.T, secret string) *http.Client {
	t.Helper()
	client := newClient(t)
	client.Transport = bearerTransport{secret: secret}
	return client
}

// mintToken creates a never-expiring API token through client's session and
// returns its secret.
func mintToken(t *testing.T, env *passkeyEnv, client *http.Client, name string) string {
	t.Helper()
	status, body := env.request(t, client, http.MethodPost, "/api/v1/auth/tokens",
		mustJSON(t, map[string]any{"name": name}))
	if status != http.StatusCreated {
		t.Fatalf("POST /auth/tokens = %d, want 201: %s", status, body)
	}
	var minted struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(body, &minted); err != nil {
		t.Fatalf("decoding the minted token: %v", err)
	}
	if minted.Secret == "" {
		t.Fatalf("the minted token carries no secret: %s", body)
	}
	return minted.Secret
}

// meStatus is the status GET /auth/me answers client with.
func meStatus(t *testing.T, env *passkeyEnv, client *http.Client) int {
	t.Helper()
	status, _ := env.request(t, client, http.MethodGet, "/api/v1/auth/me", nil)
	return status
}

// countRows counts the rows of one of the credential tables owned by userUID.
// The query is picked from a fixed set, never built from the argument.
func countRows(t *testing.T, env *passkeyEnv, query, userUID string) int {
	t.Helper()
	var n int
	if err := env.db.Pool().QueryRow(t.Context(), query, userUID).Scan(&n); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	return n
}

const (
	liveTokensQuery = `SELECT count(*) FROM api_tokens WHERE user_uid = $1 AND revoked_at IS NULL`
	passkeysQuery   = `SELECT count(*) FROM passkey_credentials WHERE user_uid = $1`
	sessionsQuery   = `SELECT count(*) FROM sessions WHERE user_uid = $1`
)

// recoveryPath runs one way of taking an account back. victim is the account's
// own browser, signed in with the old password.
type recoveryPath struct {
	name string
	// keepsCallerSession says whether the path leaves the session that ran it:
	// a self-service change keeps the caller signed in, the other two sign the
	// account out everywhere.
	keepsCallerSession bool
	recover            func(t *testing.T, env *passkeyEnv, victim *http.Client, user auth.User)
}

// recoveryPaths are the three ways a password is replaced today.
func recoveryPaths() []recoveryPath {
	return []recoveryPath{
		{
			name:               "self-service password change",
			keepsCallerSession: true,
			recover: func(t *testing.T, env *passkeyEnv, victim *http.Client, _ auth.User) {
				t.Helper()
				status, body := env.request(t, victim, http.MethodPost, "/api/v1/auth/password",
					mustJSON(t, map[string]any{
						"current_password": testPassword, "new_password": recoveredPassword,
					}))
				if status != http.StatusNoContent {
					t.Fatalf("POST /auth/password = %d, want 204: %s", status, body)
				}
			},
		},
		{
			name: "admin sets the password",
			recover: func(t *testing.T, env *passkeyEnv, _ *http.Client, user auth.User) {
				t.Helper()
				admin := newClient(t)
				env.signInWithPassword(t, admin, "boss")
				status, body := env.request(t, admin, http.MethodPost,
					"/api/v1/admin/users/"+user.UID+"/password",
					mustJSON(t, map[string]any{"new_password": recoveredPassword}))
				if status != http.StatusNoContent {
					t.Fatalf("POST /admin/users/{uid}/password = %d, want 204: %s", status, body)
				}
			},
		},
		{
			name: "consumed reset link",
			recover: func(t *testing.T, env *passkeyEnv, _ *http.Client, user auth.User) {
				t.Helper()
				admin := newClient(t)
				env.signInWithPassword(t, admin, "boss")
				status, body := env.request(t, admin, http.MethodPost,
					"/api/v1/admin/users/"+user.UID+"/password-reset", nil)
				if status != http.StatusOK {
					t.Fatalf("POST /admin/users/{uid}/password-reset = %d, want 200: %s", status, body)
				}
				var issued issuedReset
				if err := json.Unmarshal(body, &issued); err != nil {
					t.Fatalf("decoding the issued reset: %v", err)
				}
				token := resetToken(t, issued.ResetURL)
				status, body = env.request(t, newClient(t), http.MethodPost,
					"/api/v1/auth/password-reset/"+token,
					mustJSON(t, map[string]any{"password": recoveredPassword}))
				if status != http.StatusNoContent {
					t.Fatalf("POST /auth/password-reset/{token} = %d, want 204: %s", status, body)
				}
			},
		},
	}
}

// TestRecovery_leavesAPITokensAlive mints an API token, runs each recovery path,
// and calls GET /auth/me with the token. It answers 200: recovery revokes
// sessions, never tokens. A second session — the stolen cookie the recovery is
// meant to cut off — is dead, which is what shows the recovery did run.
func TestRecovery_leavesAPITokensAlive(t *testing.T) {
	for _, path := range recoveryPaths() {
		t.Run(path.name, func(t *testing.T) {
			env := newRecoveryEnv(t)
			env.user(t, "boss", auth.RoleAdmin)
			alice := env.user(t, "alice", auth.RoleEditor)

			victim := newClient(t)
			env.signInWithPassword(t, victim, "alice")
			stolen := newClient(t)
			env.signInWithPassword(t, stolen, "alice")
			planted := bearerClient(t, mintToken(t, env, stolen, "planted"))
			if got := meStatus(t, env, planted); got != http.StatusOK {
				t.Fatalf("GET /auth/me with the token before recovery = %d, want 200", got)
			}

			path.recover(t, env, victim, alice)

			if got := meStatus(t, env, stolen); got != http.StatusUnauthorized {
				t.Errorf("GET /auth/me with the stolen session after recovery = %d, want 401", got)
			}
			if got := meStatus(t, env, victim); (got == http.StatusOK) != path.keepsCallerSession {
				t.Errorf("GET /auth/me with the caller's session after recovery = %d, keeps = %v",
					got, path.keepsCallerSession)
			}
			// Current behaviour (SEC-021): the token survives the recovery.
			if got := meStatus(t, env, planted); got != http.StatusOK {
				t.Errorf("GET /auth/me with the token after recovery = %d, want 200 (tokens are not revoked)", got)
			}
			if got := countRows(t, env, liveTokensQuery, alice.UID); got != 1 {
				t.Errorf("unrevoked tokens after recovery = %d, want 1", got)
			}
		})
	}
}

// TestRecovery_leavesPasskeysAlive registers a passkey, runs each recovery path,
// and signs in with the passkey from a browser holding nothing. The sign-in
// succeeds and its session answers GET /auth/me with 200: recovery deletes no
// passkey, so the key goes on minting fresh sessions.
func TestRecovery_leavesPasskeysAlive(t *testing.T) {
	for _, path := range recoveryPaths() {
		t.Run(path.name, func(t *testing.T) {
			env := newRecoveryEnv(t)
			env.user(t, "boss", auth.RoleAdmin)
			alice := env.user(t, "alice", auth.RoleEditor)

			victim := newClient(t)
			env.signInWithPassword(t, victim, "alice")
			stolen := newClient(t)
			env.signInWithPassword(t, stolen, "alice")
			device := newVirtualAuthenticator(t)
			if status, body := env.addPasskey(t, stolen, device, "Planted"); status != http.StatusCreated {
				t.Fatalf("register finish status = %d, want 201: %s", status, body)
			}

			path.recover(t, env, victim, alice)

			if got := meStatus(t, env, stolen); got != http.StatusUnauthorized {
				t.Errorf("GET /auth/me with the stolen session after recovery = %d, want 401", got)
			}
			// Current behaviour (SEC-021): the passkey survives the recovery.
			if got := countRows(t, env, passkeysQuery, alice.UID); got != 1 {
				t.Errorf("passkeys after recovery = %d, want 1", got)
			}
			attacker := newClient(t)
			status, body := env.signInWithPasskey(t, attacker, device, alice.UID)
			if status != http.StatusOK {
				t.Fatalf("passkey login after recovery = %d, want 200 (passkeys are not deleted): %s", status, body)
			}
			if got := meStatus(t, env, attacker); got != http.StatusOK {
				t.Errorf("GET /auth/me with the passkey's new session = %d, want 200", got)
			}
		})
	}
}

// TestRecovery_bearerTokenAloneMintsTokenAndPasskey documents why a single
// surviving token is enough to keep an account: with nothing but the bearer
// secret — no session cookie, no password — the holder can mint a further token
// and register a passkey, and the passkey then mints an ordinary session.
// Neither write asks for the password again.
func TestRecovery_bearerTokenAloneMintsTokenAndPasskey(t *testing.T) {
	env := newRecoveryEnv(t)
	alice := env.user(t, "alice", auth.RoleEditor)

	victim := newClient(t)
	env.signInWithPassword(t, victim, "alice")
	planted := bearerClient(t, mintToken(t, env, victim, "planted"))
	if _, err := env.db.Pool().Exec(t.Context(), `DELETE FROM sessions WHERE user_uid = $1`, alice.UID); err != nil {
		t.Fatalf("deleting the sessions: %v", err)
	}
	if got := countRows(t, env, sessionsQuery, alice.UID); got != 0 {
		t.Fatalf("sessions = %d, want 0 before the bearer-only calls", got)
	}

	second := bearerClient(t, mintToken(t, env, planted, "second"))
	if got := meStatus(t, env, second); got != http.StatusOK {
		t.Errorf("GET /auth/me with the token minted by a token = %d, want 200", got)
	}

	device := newVirtualAuthenticator(t)
	if status, body := env.addPasskey(t, planted, device, "From a token"); status != http.StatusCreated {
		t.Fatalf("register a passkey with a bearer token = %d, want 201: %s", status, body)
	}
	attacker := newClient(t)
	if status, body := env.signInWithPasskey(t, attacker, device, alice.UID); status != http.StatusOK {
		t.Fatalf("passkey login = %d, want 200: %s", status, body)
	}
	if got := meStatus(t, env, attacker); got != http.StatusOK {
		t.Errorf("GET /auth/me with the passkey's session = %d, want 200", got)
	}
}
