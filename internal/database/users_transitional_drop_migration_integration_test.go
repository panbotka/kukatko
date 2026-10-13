//go:build integration

package database_test

import (
	"os"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
)

// usersDropMigration is the file under test.
const usersDropMigration = "migrations/0110_users_drop_transitional_columns.sql"

// transitionalUserColumns are the per-library columns 0101 copied onto the
// membership and 0110 drops from users.
var transitionalUserColumns = []string{
	"approved_at", "subject_uid", "last_seen_at", "visit_reference_at", "task_digest_at",
}

// TestMigrate_usersTransitionalColumnsDropped verifies migration 0110: after a
// full migration none of the five transitional columns is left on users, every
// one of them is still on project_members, and running the body a second time
// changes nothing.
func TestMigrate_usersTransitionalColumnsDropped(t *testing.T) {
	db := freshSchemaDB(t)
	if _, err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	assertColumns(t, db)

	body, err := os.ReadFile(usersDropMigration)
	if err != nil {
		t.Fatalf("reading %s: %v", usersDropMigration, err)
	}
	if _, err := db.Pool().Exec(t.Context(), string(body)); err != nil {
		t.Fatalf("running %s again: %v", usersDropMigration, err)
	}
	assertColumns(t, db)
}

// assertColumns fails the test unless users lacks and project_members carries
// every transitional column.
func assertColumns(t *testing.T, db *database.DB) {
	t.Helper()
	users := tableColumns(t, db, "users")
	members := tableColumns(t, db, "project_members")
	for _, col := range transitionalUserColumns {
		if slices.Contains(users, col) {
			t.Errorf("users.%s still exists after 0110", col)
		}
		if !slices.Contains(members, col) {
			t.Errorf("project_members.%s is missing", col)
		}
	}
}

// tableColumns lists the columns of table in the connection's current schema.
func tableColumns(t *testing.T, db *database.DB, table string) []string {
	t.Helper()
	rows, err := db.Pool().Query(t.Context(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		t.Fatalf("listing the columns of %s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning a column of %s: %v", table, err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating the columns of %s: %v", table, err)
	}
	if len(out) == 0 {
		t.Fatalf("table %s has no columns in the current schema", table)
	}
	return out
}

// undoUsersColumnDrop puts the five columns 0110 dropped back onto users, for
// a test that rolls the schema back to before an older migration and seeds
// accounts the way they looked then.
func undoUsersColumnDrop(t *testing.T, db *database.DB) {
	t.Helper()
	if _, err := db.Pool().Exec(t.Context(), `
		ALTER TABLE users
			ADD COLUMN last_seen_at       TIMESTAMPTZ,
			ADD COLUMN visit_reference_at TIMESTAMPTZ,
			ADD COLUMN subject_uid        VARCHAR(32) REFERENCES subjects (uid) ON DELETE SET NULL,
			ADD COLUMN approved_at        TIMESTAMPTZ,
			ADD COLUMN task_digest_at     TIMESTAMPTZ`); err != nil {
		t.Fatalf("undoing 0110: %v", err)
	}
}
