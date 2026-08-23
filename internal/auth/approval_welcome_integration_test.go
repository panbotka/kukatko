//go:build integration

package auth_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// migration0064Path is the migration under test, read from disk so the test runs
// the very statements that ship rather than a copy of them.
const migration0064Path = "../database/migrations/0064_users_approval_welcome.sql"

// accountFlags is the part of a user payload these tests read: the two new
// timestamps and the disabled flag they must stay distinguishable from.
type accountFlags struct {
	UID           string     `json:"uid"`
	Username      string     `json:"username"`
	Disabled      bool       `json:"disabled"`
	ApprovedAt    *time.Time `json:"approved_at"`
	WelcomeSeenAt *time.Time `json:"welcome_seen_at"`
}

// TestMigration0064_backfillsApproval replays the migration against a table of
// the shape users had before it, in a throwaway schema that is rolled back
// afterwards, and asserts what it must do to rows that already exist: every
// account is approved as of when it was created — the only way to hold one
// before this point was for an administrator to make it — and nobody is recorded
// as having seen the welcome, because nobody has.
func TestMigration0064_backfillsApproval(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()

	statements, err := os.ReadFile(migration0064Path)
	if err != nil {
		t.Fatalf("reading %s: %v", migration0064Path, err)
	}

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// The whole probe is rolled back, so the shared test database keeps neither
	// the schema nor the rows.
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `CREATE SCHEMA mig0064`); err != nil {
		t.Fatalf("CREATE SCHEMA: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = mig0064`); err != nil {
		t.Fatalf("SET search_path: %v", err)
	}
	// The pre-migration shape: neither column exists yet.
	if _, err := tx.Exec(ctx, `CREATE TABLE users (
		uid        VARCHAR(32) PRIMARY KEY,
		username   TEXT        NOT NULL UNIQUE,
		disabled   BOOLEAN     NOT NULL DEFAULT false,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	seed := []struct {
		uid, username string
		disabled      bool
		createdAt     time.Time
	}{
		{"us0000000000000000000000a", "alice", false, time.Date(2026, time.January, 2, 9, 0, 0, 0, time.UTC)},
		// A blocked account was approved once too: the migration must not read
		// the disabled flag as "never approved".
		{"us0000000000000000000000b", "bob", true, time.Date(2026, time.March, 4, 18, 30, 0, 0, time.UTC)},
	}
	for _, s := range seed {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (uid, username, disabled, created_at) VALUES ($1, $2, $3, $4)`,
			s.uid, s.username, s.disabled, s.createdAt); err != nil {
			t.Fatalf("seeding %q: %v", s.username, err)
		}
	}

	if _, err := tx.Exec(ctx, string(statements)); err != nil {
		t.Fatalf("running migration 0064: %v", err)
	}

	rows, err := tx.Query(ctx, `SELECT username, approved_at, welcome_seen_at, created_at FROM users`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	seen := 0
	for rows.Next() {
		var username string
		var approvedAt, welcomeSeenAt *time.Time
		var createdAt time.Time
		if err := rows.Scan(&username, &approvedAt, &welcomeSeenAt, &createdAt); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		seen++
		switch {
		case approvedAt == nil:
			t.Errorf("%s: approved_at = NULL, want the account backfilled as approved", username)
		case !approvedAt.Equal(createdAt):
			t.Errorf("%s: approved_at = %v, want created_at %v", username, *approvedAt, createdAt)
		}
		if welcomeSeenAt != nil {
			t.Errorf("%s: welcome_seen_at = %v, want NULL — nobody has been shown it yet",
				username, *welcomeSeenAt)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if seen != len(seed) {
		t.Fatalf("read %d rows, want %d", seen, len(seed))
	}
}

// signInAs logs username in with the shared test password and returns a client
// carrying its session cookie.
func signInAs(t *testing.T, env *httpEnv, username string) *http.Client {
	t.Helper()
	client := newClient(t)
	status, body := env.do(t, client, http.MethodPost, "/api/v1/auth/login", loginJSON(username, testPassword))
	if status != http.StatusOK {
		t.Fatalf("login %s = %d, body %s", username, status, body)
	}
	return client
}

// meFlags reads the caller's own account off GET /auth/me, which is where the
// client learns whether it still owes the welcome.
func meFlags(t *testing.T, env *httpEnv, client *http.Client) accountFlags {
	t.Helper()
	status, body := env.do(t, client, http.MethodGet, "/api/v1/auth/me", "")
	if status != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, body %s", status, body)
	}
	var resp struct {
		User accountFlags `json:"user"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decoding /auth/me: %v", err)
	}
	return resp.User
}

// TestApproval_adminCreatedAccountIsApproved covers the whole admin path: an
// account made through POST /admin/users comes back approved, because an
// administrator creating it *is* the approval, and the roster keeps reporting
// that stamp — separately from the disabled flag — after the account is blocked.
func TestApproval_adminCreatedAccountIsApproved(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "boss", auth.RoleAdmin)
	admin := signInAs(t, env, "boss")

	body := `{"username":"newcomer","password":"` + testPassword +
		`","email":"newcomer@example.test","role":"viewer"}`
	status, raw := env.do(t, admin, http.MethodPost, "/api/v1/admin/users", body)
	if status != http.StatusCreated {
		t.Fatalf("POST /admin/users = %d, body %s", status, raw)
	}
	var created accountFlags
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatalf("decoding created user: %v", err)
	}
	if created.ApprovedAt == nil {
		t.Fatal("created account has approved_at = null, want it approved on creation")
	}
	if created.WelcomeSeenAt != nil {
		t.Errorf("created account has welcome_seen_at = %v, want null", *created.WelcomeSeenAt)
	}
	approvedAt := *created.ApprovedAt

	// Blocking the account must not disturb the approval: "never approved" and
	// "approved and later blocked" are different states and the listing has to
	// tell them apart.
	status, raw = env.do(t, admin, http.MethodPost, "/api/v1/admin/users/"+created.UID+"/disable", "")
	if status != http.StatusOK {
		t.Fatalf("disable = %d, body %s", status, raw)
	}

	listed := listAccount(t, env, admin, "newcomer")
	if !listed.Disabled {
		t.Error("listed account is not disabled after the disable call")
	}
	if listed.ApprovedAt == nil {
		t.Fatal("listed account lost approved_at when it was disabled")
	}
	if !listed.ApprovedAt.Equal(approvedAt) {
		t.Errorf("listed approved_at = %v, want the creation stamp %v", *listed.ApprovedAt, approvedAt)
	}
}

// listAccount reads one account by username off the admin roster, failing the
// test when it is absent.
func listAccount(t *testing.T, env *httpEnv, client *http.Client, username string) accountFlags {
	t.Helper()
	status, raw := env.do(t, client, http.MethodGet, "/api/v1/admin/users", "")
	if status != http.StatusOK {
		t.Fatalf("GET /admin/users = %d, body %s", status, raw)
	}
	var users []accountFlags
	if err := json.Unmarshal(raw, &users); err != nil {
		t.Fatalf("decoding user list: %v", err)
	}
	for _, u := range users {
		if u.Username == username {
			return u
		}
	}
	t.Fatalf("account %q missing from the admin listing", username)
	return accountFlags{}
}

// TestWelcomeSeen_stampsOnceAndIsIdempotent walks the client's whole loop: a
// signed-in viewer starts owing the welcome, says it has been seen, and both the
// endpoint's own answer and GET /auth/me report the same stamp — which a second
// call leaves exactly where it was.
func TestWelcomeSeen_stampsOnceAndIsIdempotent(t *testing.T) {
	env := newHTTPEnv(t, 10)
	env.mustCreate(t, "reader", auth.RoleViewer)
	client := signInAs(t, env, "reader")

	if before := meFlags(t, env, client); before.WelcomeSeenAt != nil {
		t.Fatalf("/auth/me welcome_seen_at = %v before the welcome was seen, want null", *before.WelcomeSeenAt)
	}

	first := postWelcomeSeen(t, env, client)
	if first.WelcomeSeenAt == nil {
		t.Fatal("POST /auth/welcome-seen returned welcome_seen_at = null, want a stamp")
	}
	stamp := *first.WelcomeSeenAt

	if got := meFlags(t, env, client); got.WelcomeSeenAt == nil || !got.WelcomeSeenAt.Equal(stamp) {
		t.Errorf("/auth/me welcome_seen_at = %v, want the stamp %v", got.WelcomeSeenAt, stamp)
	}

	// Calling it again is harmless and must never move the timestamp — not
	// forward, and above all not backwards.
	second := postWelcomeSeen(t, env, client)
	if second.WelcomeSeenAt == nil || !second.WelcomeSeenAt.Equal(stamp) {
		t.Errorf("second POST /auth/welcome-seen = %v, want the unchanged stamp %v",
			second.WelcomeSeenAt, stamp)
	}
	if got := meFlags(t, env, client); got.WelcomeSeenAt == nil || !got.WelcomeSeenAt.Equal(stamp) {
		t.Errorf("/auth/me welcome_seen_at after the second call = %v, want %v", got.WelcomeSeenAt, stamp)
	}
}

// TestWelcomeSeen_requiresAuth asserts the endpoint is behind RequireAuth: it
// writes to the *session's* account, so an anonymous caller has none to write to.
func TestWelcomeSeen_requiresAuth(t *testing.T) {
	env := newHTTPEnv(t, 10)
	status, body := env.do(t, newClient(t), http.MethodPost, "/api/v1/auth/welcome-seen", "")
	if status != http.StatusUnauthorized {
		t.Errorf("anonymous POST /auth/welcome-seen = %d, want 401 (body %s)", status, body)
	}
}

// postWelcomeSeen calls the endpoint and returns the refreshed account it
// answers with.
func postWelcomeSeen(t *testing.T, env *httpEnv, client *http.Client) accountFlags {
	t.Helper()
	status, raw := env.do(t, client, http.MethodPost, "/api/v1/auth/welcome-seen", "")
	if status != http.StatusOK {
		t.Fatalf("POST /auth/welcome-seen = %d, body %s", status, raw)
	}
	var user accountFlags
	if err := json.Unmarshal(raw, &user); err != nil {
		t.Fatalf("decoding welcome-seen response: %v", err)
	}
	return user
}
