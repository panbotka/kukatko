//go:build integration

package auth_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// migration0063Path is the migration under test, read from disk so the test runs
// the very statements that ship rather than a copy of them.
const migration0063Path = "../database/migrations/0063_users_email_required.sql"

// TestMigration0063_fillsPlaceholders replays the migration against a table of
// the shape users had before it, in a throwaway schema that is rolled back
// afterwards, and asserts what it must do to rows that already exist: every
// blank address is replaced by a distinct, syntactically valid placeholder in
// the undeliverable .invalid domain, and an address somebody already has is left
// exactly as it was — including one that two accounts share.
func TestMigration0063_fillsPlaceholders(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()

	statements, err := os.ReadFile(migration0063Path)
	if err != nil {
		t.Fatalf("reading %s: %v", migration0063Path, err)
	}

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// The whole probe is rolled back, so the shared test database keeps neither
	// the schema nor the rows.
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `CREATE SCHEMA mig0063`); err != nil {
		t.Fatalf("CREATE SCHEMA: %v", err)
	}
	if _, err := tx.Exec(ctx, `SET LOCAL search_path = mig0063`); err != nil {
		t.Fatalf("SET search_path: %v", err)
	}
	// The pre-migration shape of the column: NOT NULL with an empty default.
	if _, err := tx.Exec(ctx, `CREATE TABLE users (
		uid      VARCHAR(32) PRIMARY KEY,
		username TEXT        NOT NULL UNIQUE,
		email    TEXT        NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	seed := []struct{ uid, username, email string }{
		{"us0000000000000000000000a", "alice", "alice@example.com"},
		{"us0000000000000000000000b", "bob", ""},
		// The two usernames collide once reduced to a safe local part, which is
		// why the placeholder carries the uid.
		{"us0000000000000000000000c", "Jan Novák", ""},
		{"us0000000000000000000000d", "jan-novak", ""},
		// Nothing usable survives the reduction, and whitespace is as empty as
		// empty.
		{"us0000000000000000000000e", "☺", ""},
		{"us0000000000000000000000f", "spacey", "   "},
		// A household mailbox two accounts share must survive untouched.
		{"us0000000000000000000000g", "house-one", "house@example.com"},
		{"us0000000000000000000000h", "house-two", "house@example.com"},
	}
	for _, s := range seed {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (uid, username, email) VALUES ($1, $2, $3)`,
			s.uid, s.username, s.email); err != nil {
			t.Fatalf("seeding %q: %v", s.username, err)
		}
	}

	if _, err := tx.Exec(ctx, string(statements)); err != nil {
		t.Fatalf("running migration 0063: %v", err)
	}

	got := map[string]string{}
	rows, err := tx.Query(ctx, `SELECT username, email FROM users`)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	for rows.Next() {
		var username, email string
		if err := rows.Scan(&username, &email); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		got[username] = email
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	// Addresses that were already there are untouched, shared one included.
	for username, want := range map[string]string{
		"alice":     "alice@example.com",
		"house-one": "house@example.com",
		"house-two": "house@example.com",
	} {
		if got[username] != want {
			t.Errorf("%s kept email %q, want %q", username, got[username], want)
		}
	}

	// Every blank one is now an undeliverable placeholder naming its account.
	placeholders := map[string]string{}
	for _, username := range []string{"bob", "Jan Novák", "jan-novak", "☺", "spacey"} {
		email := got[username]
		if !strings.HasSuffix(email, "@kukatko.invalid") {
			t.Errorf("%s placeholder = %q, want an @kukatko.invalid address", username, email)
		}
		if len(strings.SplitN(email, "@", 2)[0]) > 64 {
			t.Errorf("%s placeholder %q has an over-long local part", username, email)
		}
		if other, dup := placeholders[email]; dup {
			t.Errorf("%s and %s were given the same placeholder %q", username, other, email)
		}
		placeholders[email] = username
	}
	if want := "bob-us0000000000000000000000b@kukatko.invalid"; got["bob"] != want {
		t.Errorf("bob placeholder = %q, want %q", got["bob"], want)
	}
	if want := "user-us0000000000000000000000e@kukatko.invalid"; got["☺"] != want {
		t.Errorf("unusable username placeholder = %q, want %q", got["☺"], want)
	}

	// The empty-string default is gone and blank is refused from now on.
	if _, err := tx.Exec(ctx,
		`INSERT INTO users (uid, username) VALUES ('us0000000000000000000000z', 'zoe')`); err == nil {
		t.Error("insert without an email succeeded, want a NOT NULL violation")
	}
}

// TestHTTP_createUserRequiresEmail asserts an account cannot be created through
// the admin API without a usable address: an omitted, blank and malformed one
// are all refused with a 400 that names the field, while a valid address is
// accepted and stored normalized.
func TestHTTP_createUserRequiresEmail(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "mailadmin", auth.RoleAdmin)
	admin := env.loginClient(t, "mailadmin")

	rejected := []struct {
		name string
		body string
	}{
		{"omitted", `{"username":"noaddr","password":"correct horse battery","role":"viewer"}`},
		{"empty", `{"username":"noaddr","password":"correct horse battery","role":"viewer","email":""}`},
		{"whitespace", `{"username":"noaddr","password":"correct horse battery","role":"viewer","email":"   "}`},
		{"malformed", `{"username":"noaddr","password":"correct horse battery","role":"viewer","email":"nope"}`},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			status, body := env.do(t, admin, "POST", "/api/v1/admin/users", tt.body)
			if status != 400 {
				t.Fatalf("create with an %s address = %d, want 400 (body %s)", tt.name, status, body)
			}
			if !strings.Contains(strings.ToLower(string(body)), "email") {
				t.Errorf("the 400 does not name the email field: %s", body)
			}
		})
	}

	// The address is normalized on the way in: trimmed, domain lower-cased.
	status, body := env.do(t, admin, "POST", "/api/v1/admin/users",
		`{"username":"mailed","password":"correct horse battery","role":"viewer",`+
			`"email":"  Jan.Novak@Example.COM  "}`)
	if status != 201 {
		t.Fatalf("create with a valid address = %d, want 201 (body %s)", status, body)
	}
	created := decodeUser(t, body)
	if created["email"] != "Jan.Novak@example.com" {
		t.Errorf("stored email = %v, want Jan.Novak@example.com", created["email"])
	}
}

// TestHTTP_updateUserRejectsInvalidEmail asserts an existing account cannot be
// updated into having no usable address — neither by clearing it nor by
// mistyping it — and that a refused update leaves the stored address alone.
func TestHTTP_updateUserRejectsInvalidEmail(t *testing.T) {
	env := newHTTPEnv(t, 50)
	env.mustCreate(t, "updadmin", auth.RoleAdmin)
	admin := env.loginClient(t, "updadmin")
	target := env.mustCreate(t, "updtarget", auth.RoleViewer)
	path := "/api/v1/admin/users/" + target.UID

	for _, tt := range []struct {
		name string
		body string
	}{
		{"omitted", `{"display_name":"","role":"viewer","disabled":false}`},
		{"empty", `{"display_name":"","email":"","role":"viewer","disabled":false}`},
		{"whitespace", `{"display_name":"","email":" \t ","role":"viewer","disabled":false}`},
		{"malformed", `{"display_name":"","email":"jan@localhost","role":"viewer","disabled":false}`},
		{"display-name form", `{"display_name":"","email":"Jan <jan@example.com>","role":"viewer","disabled":false}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, body := env.do(t, admin, "PATCH", path, tt.body)
			if status != 400 {
				t.Fatalf("update to an %s address = %d, want 400 (body %s)", tt.name, status, body)
			}
		})
	}

	// The refusals changed nothing.
	after, err := env.svc.GetUser(t.Context(), target.UID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if after.Email != target.Email {
		t.Errorf("email after refused updates = %q, want %q", after.Email, target.Email)
	}
}

// TestCreateUser_emailRules asserts, through the real service and database, the
// two halves of the rule the column now depends on: an account cannot be created
// without a valid address, and two accounts may deliberately share one — a
// household mailbox is a real arrangement, so there is no unique index.
func TestCreateUser_emailRules(t *testing.T) {
	env := newTestEnv(t)

	_, err := env.svc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: "addressless", Password: testPassword, Role: auth.RoleViewer,
	})
	if !errors.Is(err, auth.ErrInvalidEmail) {
		t.Fatalf("CreateUser without an address err = %v, want ErrInvalidEmail", err)
	}

	const shared = "rodina@example.com"
	for _, username := range []string{"matka", "otec"} {
		user, err := env.svc.CreateUser(t.Context(), auth.CreateUserInput{
			Username: username, Email: shared, Password: testPassword, Role: auth.RoleViewer,
		})
		if err != nil {
			t.Fatalf("CreateUser(%q) with a shared address: %v", username, err)
		}
		if user.Email != shared {
			t.Errorf("%s stored email = %q, want %q", username, user.Email, shared)
		}
	}
}

// TestBootstrap_getsPlaceholderEmail asserts a first start needs no mailbox: the
// bootstrap maintainer created on an empty database lands with an undeliverable
// placeholder address rather than none at all.
func TestBootstrap_getsPlaceholderEmail(t *testing.T) {
	env := newTestEnv(t)

	outcome, err := env.svc.Bootstrap(t.Context(), "Root Admin", testPassword)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if outcome != auth.BootstrapCreated {
		t.Fatalf("outcome = %v, want BootstrapCreated", outcome)
	}
	user, err := env.store.GetUserByUsername(t.Context(), "root admin")
	if err != nil {
		t.Fatalf("GetUserByUsername: %v", err)
	}
	if want := "root-admin@kukatko.invalid"; user.Email != want {
		t.Errorf("bootstrap email = %q, want %q", user.Email, want)
	}
}
