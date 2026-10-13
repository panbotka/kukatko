//go:build integration

package database_test

import (
	"os"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/project"
)

// membersMigration is the file under test.
const membersMigration = "migrations/0101_project_members.sql"

// backfillUser is one account seeded before migration 0101 runs, and the
// membership it must come out of the backfill with.
type backfillUser struct {
	uid, role, wantRole string
	subject             *string
}

// TestMigrate_projectMembersBackfill verifies migration 0101 against a database
// in the state just before it: one account per instance role, with every
// per-library column filled. Each becomes a member of the bootstrap project with
// its role (maintainer → admin), the five columns copied 1:1 and its last
// project set — and running the migration body again changes nothing.
func TestMigrate_projectMembersBackfill(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	undoUsersColumnDrop(t, db)
	// Roll the schema back to just before 0101, so the seeded accounts are what
	// the migration finds, exactly as in production — including the role CHECK
	// 0103 later narrowed to user/maintainer.
	if _, err := db.Pool().Exec(ctx, `
		DROP TABLE project_members;
		ALTER TABLE users DROP COLUMN last_project_uid;
		ALTER TABLE users DROP CONSTRAINT users_role_check;
		ALTER TABLE users ADD CONSTRAINT users_role_check
			CHECK (role IN ('viewer', 'curator', 'editor', 'admin', 'maintainer'))`); err != nil {
		t.Fatalf("undoing 0101: %v", err)
	}

	subject := "sbj_backfill"
	if _, err := db.Pool().Exec(ctx,
		`INSERT INTO subjects (uid, slug, name, type, project_id) VALUES ($1, $2, 'Backfill', 'person', $3)`,
		subject, subject, project.BootstrapUID); err != nil {
		t.Fatalf("seeding the subject: %v", err)
	}
	approved := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	seen := approved.Add(time.Hour)
	reference := approved.Add(2 * time.Hour)
	digest := approved.Add(3 * time.Hour)
	users := []backfillUser{
		{uid: "us_bf_viewer", role: "viewer", wantRole: "viewer", subject: &subject},
		{uid: "us_bf_curator", role: "curator", wantRole: "curator"},
		{uid: "us_bf_editor", role: "editor", wantRole: "editor"},
		{uid: "us_bf_admin", role: "admin", wantRole: "admin"},
		{uid: "us_bf_maint", role: "maintainer", wantRole: "admin"},
	}
	for _, u := range users {
		if _, err := db.Pool().Exec(ctx, `
			INSERT INTO users (uid, username, email, password_hash, role, approved_at, subject_uid,
			                   last_seen_at, visit_reference_at, task_digest_at)
			VALUES ($1, $2, $3, 'x', $4, $5, $6, $7, $8, $9)`,
			u.uid, u.uid, u.uid+"@example.test", u.role, approved, u.subject, seen, reference, digest); err != nil {
			t.Fatalf("seeding %s: %v", u.uid, err)
		}
	}
	// One account that was never approved and has no clocks: NULLs copy as NULLs.
	if _, err := db.Pool().Exec(ctx, `
		INSERT INTO users (uid, username, email, password_hash, role, approved_at)
		VALUES ('us_bf_pending', 'us_bf_pending', 'pending@example.test', 'x', 'viewer', NULL)`); err != nil {
		t.Fatalf("seeding the pending account: %v", err)
	}

	runMembersMigration(t, db)
	assertBackfill(t, db, users, approved, seen, reference, digest)
	assertPendingBackfill(t, db)

	// Idempotent: the body again, then the runner again, change nothing.
	runMembersMigration(t, db)
	if again, err := db.Migrate(ctx); err != nil || len(again) != 0 {
		t.Fatalf("second Migrate = %v, %v; want nothing applied", again, err)
	}
	assertBackfill(t, db, users, approved, seen, reference, digest)
	assertPendingBackfill(t, db)
}

// runMembersMigration executes migration 0101's body against db.
func runMembersMigration(t *testing.T, db *database.DB) {
	t.Helper()
	body, err := os.ReadFile(membersMigration)
	if err != nil {
		t.Fatalf("reading %s: %v", membersMigration, err)
	}
	if _, err := db.Pool().Exec(t.Context(), string(body)); err != nil {
		t.Fatalf("running %s: %v", membersMigration, err)
	}
}

// assertBackfill fails the test unless every seeded account is exactly one
// bootstrap membership with the mapped role and the copied columns.
func assertBackfill(t *testing.T, db *database.DB, users []backfillUser, approved, seen, reference,
	digest time.Time,
) {
	t.Helper()
	var total int
	if err := db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM project_members`).Scan(&total); err != nil {
		t.Fatalf("counting memberships: %v", err)
	}
	if total != len(users)+1 {
		t.Errorf("project_members holds %d rows, want %d", total, len(users)+1)
	}
	for _, u := range users {
		var (
			projectUID, role, lastProject   string
			approvedAt, seenAt, refAt, dgAt *time.Time
			subject                         *string
		)
		if err := db.Pool().QueryRow(t.Context(), `
			SELECT m.project_uid, m.role, m.approved_at, m.subject_uid, m.last_seen_at,
			       m.visit_reference_at, m.task_digest_at, u.last_project_uid
			FROM project_members m JOIN users u ON u.uid = m.user_uid
			WHERE m.user_uid = $1`, u.uid).
			Scan(&projectUID, &role, &approvedAt, &subject, &seenAt, &refAt, &dgAt, &lastProject); err != nil {
			t.Fatalf("reading the membership of %s: %v", u.uid, err)
		}
		if projectUID != project.BootstrapUID || role != u.wantRole || lastProject != project.BootstrapUID {
			t.Errorf("%s: project %s role %s last project %s; want %s/%s/%s", u.uid, projectUID, role,
				lastProject, project.BootstrapUID, u.wantRole, project.BootstrapUID)
		}
		if !sameTime(approvedAt, approved) || !sameTime(seenAt, seen) || !sameTime(refAt, reference) ||
			!sameTime(dgAt, digest) {
			t.Errorf("%s: clocks %v/%v/%v/%v not copied", u.uid, approvedAt, seenAt, refAt, dgAt)
		}
		if (subject == nil) != (u.subject == nil) || (subject != nil && *subject != *u.subject) {
			t.Errorf("%s: subject %v, want %v", u.uid, subject, u.subject)
		}
	}
}

// assertPendingBackfill fails the test unless the never-approved account became
// an unapproved bootstrap member with no clocks.
func assertPendingBackfill(t *testing.T, db *database.DB) {
	t.Helper()
	var (
		role                            string
		approvedAt, seenAt, refAt, dgAt *time.Time
	)
	if err := db.Pool().QueryRow(t.Context(), `
		SELECT role, approved_at, last_seen_at, visit_reference_at, task_digest_at
		FROM project_members WHERE user_uid = 'us_bf_pending' AND project_uid = $1`, project.BootstrapUID).
		Scan(&role, &approvedAt, &seenAt, &refAt, &dgAt); err != nil {
		t.Fatalf("reading the pending membership: %v", err)
	}
	if role != "viewer" || approvedAt != nil || seenAt != nil || refAt != nil || dgAt != nil {
		t.Errorf("pending membership = %s/%v/%v/%v/%v, want viewer and all NULL", role, approvedAt, seenAt,
			refAt, dgAt)
	}
}

// sameTime reports whether got is set and the same instant as want.
func sameTime(got *time.Time, want time.Time) bool {
	return got != nil && got.Equal(want)
}

// TestMigrate_freshDatabaseHasNoMembers verifies a freshly migrated database —
// no accounts yet — has an empty project_members, and that migrating again
// changes nothing.
func TestMigrate_freshDatabaseHasNoMembers(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()
	for range 2 {
		if _, err := db.Migrate(ctx); err != nil {
			t.Fatalf("migrating: %v", err)
		}
		var members int
		if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM project_members`).Scan(&members); err != nil {
			t.Fatalf("counting memberships: %v", err)
		}
		if members != 0 {
			t.Errorf("a fresh database holds %d memberships, want 0", members)
		}
	}
}
