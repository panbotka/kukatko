//go:build integration

package database_test

import (
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/database"
)

// projectIDDefaultsMigration is the file under test.
const projectIDDefaultsMigration = "migrations/0114_project_id_drop_defaults.sql"

// projectIDTables are the tables whose project_id carried the transitional
// default of the bootstrap project (migration 0102) until 0114 dropped it.
var projectIDTables = []string{
	"photos", "subjects", "albums", "labels", "face_clusters", "subject_families", "photo_tasks",
	"upload_links", "import_runs", "saved_searches", "notifications", "embeddings", "faces",
	"search_history", "instance_settings", "announcements", "photoprism_aliases",
}

// TestMigrate_projectIDDefaultsDropped verifies migration 0114: after a full
// migration no project_id column has a default any more while each is still
// NOT NULL with its foreign key to projects, an insert that names no project
// fails on NOT NULL instead of landing in the bootstrap project, and running
// the body a second time changes nothing.
func TestMigrate_projectIDDefaultsDropped(t *testing.T) {
	db := freshSchemaDB(t)
	if _, err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	assertProjectIDColumns(t, db)

	body, err := os.ReadFile(projectIDDefaultsMigration)
	if err != nil {
		t.Fatalf("reading %s: %v", projectIDDefaultsMigration, err)
	}
	if _, err := db.Pool().Exec(t.Context(), string(body)); err != nil {
		t.Fatalf("running %s again: %v", projectIDDefaultsMigration, err)
	}
	assertProjectIDColumns(t, db)

	_, err = db.Pool().Exec(t.Context(),
		`INSERT INTO albums (uid, slug, title) VALUES ('al_noproject', 'no-project', 'Bez projektu')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23502" || pgErr.ColumnName != "project_id" {
		t.Errorf("insert without a project = %v, want a NOT NULL violation on project_id", err)
	}
}

// assertProjectIDColumns fails the test unless every table of projectIDTables
// has a project_id that is NOT NULL, has no default and references projects.
func assertProjectIDColumns(t *testing.T, db *database.DB) {
	t.Helper()
	for _, table := range projectIDTables {
		var nullable string
		var def *string
		err := db.Pool().QueryRow(t.Context(), `
			SELECT is_nullable, column_default FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'project_id'`,
			table).Scan(&nullable, &def)
		if err != nil {
			t.Errorf("%s.project_id: %v", table, err)
			continue
		}
		if nullable != "NO" {
			t.Errorf("%s.project_id is nullable", table)
		}
		if def != nil {
			t.Errorf("%s.project_id still defaults to %s", table, *def)
		}
		var fks int
		if err := db.Pool().QueryRow(t.Context(), `
			SELECT count(*) FROM pg_constraint c
			JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
			WHERE c.contype = 'f' AND c.conrelid = to_regclass($1)
			  AND c.confrelid = to_regclass('projects') AND a.attname = 'project_id'`,
			table).Scan(&fks); err != nil || fks != 1 {
			t.Errorf("%s.project_id foreign keys to projects = %d (%v), want 1", table, fks, err)
		}
	}
}
