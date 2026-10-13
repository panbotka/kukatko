//go:build integration

package database_test

import (
	"os"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
)

// instanceRoleMigration is the file under test.
const instanceRoleMigration = "migrations/0103_users_role_instance.sql"

// TestMigrate_usersRoleCollapsesToInstanceRole verifies migration 0103 against
// a database in the state just before it: accounts on the old five-rung roles,
// one of them created after 0101 and therefore without a membership, and a live
// session caching a role. Every non-maintainer becomes 'user', the account
// without a membership gets one carrying its old role before that role is
// erased, an existing membership is left exactly as it was, sessions.role is
// gone while the session survives, the new CHECK refuses an old role — and the
// body runs a second time without changing anything.
func TestMigrate_usersRoleCollapsesToInstanceRole(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	undoUsersColumnDrop(t, db)
	if _, err := db.Pool().Exec(ctx, `
		ALTER TABLE users DROP CONSTRAINT users_role_check;
		ALTER TABLE users ADD CONSTRAINT users_role_check
			CHECK (role IN ('viewer', 'curator', 'editor', 'admin', 'maintainer'));
		ALTER TABLE sessions ADD COLUMN role TEXT NOT NULL DEFAULT 'viewer'`); err != nil {
		t.Fatalf("undoing 0103: %v", err)
	}
	for _, u := range []struct{ uid, role string }{
		{"us_ir_member", "editor"}, {"us_ir_late", "curator"}, {"us_ir_maint", "maintainer"},
	} {
		if _, err := db.Pool().Exec(ctx, `
			INSERT INTO users (uid, username, email, password_hash, role, approved_at)
			VALUES ($1, $2, $3, 'x', $4, now())`, u.uid, u.uid, u.uid+"@example.test", u.role); err != nil {
			t.Fatalf("seeding %s: %v", u.uid, err)
		}
	}
	// The 0101 backfill already ran for this one, and an admin has since made
	// it a viewer of the project; 0103 must not overwrite that.
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO project_members (project_uid, user_uid, role, approved_at)
		VALUES ('`+project.BootstrapUID+`', 'us_ir_member', 'viewer', now());
		INSERT INTO sessions (id, token, download_token, user_uid, role, expires_at)
		VALUES ('ses_ir', 'tok_ir', 'dl_ir', 'us_ir_member', 'editor', now() + interval '1 hour')`); err != nil {
		t.Fatalf("seeding the membership and session: %v", err)
	}

	runInstanceRoleMigration(t, db)
	assertInstanceRoles(t, db)
	runInstanceRoleMigration(t, db)
	assertInstanceRoles(t, db)

	if _, err := db.Pool().Exec(ctx, `UPDATE users SET role = 'admin' WHERE uid = 'us_ir_late'`); err == nil {
		t.Error("the new CHECK accepted the retired role 'admin'")
	}
	var sessions int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM sessions WHERE id = 'ses_ir'`).Scan(&sessions); err != nil ||
		sessions != 1 {
		t.Errorf("sessions after the migration = %d (%v), want the live one kept", sessions, err)
	}
	var hasRole bool
	if err := db.Pool().QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		               WHERE table_schema = current_schema() AND table_name = 'sessions' AND column_name = 'role')`).
		Scan(&hasRole); err != nil || hasRole {
		t.Errorf("sessions.role still exists = %v (%v), want dropped", hasRole, err)
	}
}

// runInstanceRoleMigration executes migration 0103's body against db.
func runInstanceRoleMigration(t *testing.T, db *database.DB) {
	t.Helper()
	body, err := os.ReadFile(instanceRoleMigration)
	if err != nil {
		t.Fatalf("reading %s: %v", instanceRoleMigration, err)
	}
	if _, err := db.Pool().Exec(t.Context(), string(body)); err != nil {
		t.Fatalf("running %s: %v", instanceRoleMigration, err)
	}
}

// assertInstanceRoles fails the test unless the three seeded accounts carry
// the instance role and the bootstrap membership the migration must leave.
func assertInstanceRoles(t *testing.T, db *database.DB) {
	t.Helper()
	want := map[string][2]string{
		"us_ir_member": {"user", "viewer"},
		"us_ir_late":   {"user", "curator"},
		"us_ir_maint":  {"maintainer", "admin"},
	}
	for uid, w := range want {
		var role, memberRole string
		if err := db.Pool().QueryRow(t.Context(), `
			SELECT u.role, m.role FROM users u
			JOIN project_members m ON m.user_uid = u.uid AND m.project_uid = $2
			WHERE u.uid = $1`, uid, project.BootstrapUID).Scan(&role, &memberRole); err != nil {
			t.Fatalf("reading %s: %v", uid, err)
		}
		if role != w[0] || memberRole != w[1] {
			t.Errorf("%s: users.role %s, membership %s; want %s/%s", uid, role, memberRole, w[0], w[1])
		}
	}
}
