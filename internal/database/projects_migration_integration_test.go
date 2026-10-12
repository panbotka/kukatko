//go:build integration

package database_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/project"
)

// freshSchemaDB returns a DB whose connections resolve unqualified names to a
// brand-new, empty schema (the extensions stay reachable through public), so the
// whole migration chain runs from scratch without touching the shared test
// database's public schema — or racing another session's tests in it. The
// schema is dropped on cleanup.
func freshSchemaDB(t *testing.T) *database.DB {
	t.Helper()
	raw := os.Getenv(dbtest.EnvTestDatabaseURL)
	if raw == "" {
		t.Skipf("%s not set; skipping integration test", dbtest.EnvTestDatabaseURL)
	}
	// Make sure the extensions the migrations need already exist in public.
	_ = dbtest.New(t)

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random schema suffix: %v", err)
	}
	schema := "mt_fresh_" + hex.EncodeToString(suffix)

	db, err := database.New(t.Context(), config.DatabaseConfig{
		URL: dsnWithParam(raw, "search_path", schema+",public"), MaxOpenConns: 2, MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("connecting with a fresh search_path: %v", err)
	}
	ident := pgx.Identifier{schema}.Sanitize()
	t.Cleanup(func() {
		if _, err := db.Pool().Exec(context.Background(), "DROP SCHEMA IF EXISTS "+ident+" CASCADE"); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		db.Close()
	})
	if _, err := db.Pool().Exec(t.Context(), "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	return db
}

// dsnWithParam returns dsn with one runtime parameter added, for both DSN forms
// pgx accepts (a postgres:// URL or the keyword/value form).
func dsnWithParam(dsn, key, value string) string {
	if !strings.Contains(dsn, "://") {
		return dsn + " " + key + "='" + value + "'"
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + key + "=" + url.QueryEscape(value)
}

// TestMigrate_freshDatabaseHasOnlyTheBootstrapProject verifies that migrating an
// empty schema leaves exactly one project — the bootstrap row, with the values
// project.BootstrapUID/BootstrapSlug promise — and that migrating again (and
// re-running migration 0100's statements themselves) changes nothing.
func TestMigrate_freshDatabaseHasOnlyTheBootstrapProject(t *testing.T) {
	db := freshSchemaDB(t)
	ctx := t.Context()

	applied, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("migrating a fresh schema: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("a fresh schema applied no migrations — the search_path did not isolate it")
	}
	assertOnlyBootstrap(t, db)

	again, err := db.Migrate(ctx)
	if err != nil {
		t.Fatalf("migrating a second time: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second Migrate applied %v, want none", again)
	}
	assertOnlyBootstrap(t, db)

	// The migration body itself is idempotent, not only its bookkeeping.
	body, err := os.ReadFile("migrations/0100_projects.sql")
	if err != nil {
		t.Fatalf("reading migration 0100: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, string(body)); err != nil {
		t.Fatalf("re-running migration 0100's statements: %v", err)
	}
	assertOnlyBootstrap(t, db)
}

// assertOnlyBootstrap fails the test unless projects holds exactly the bootstrap
// row with its fixed values.
func assertOnlyBootstrap(t *testing.T, db *database.DB) {
	t.Helper()
	var (
		count                         int
		uid, slug, name, desc, prefix string
		active                        bool
		maxFile, maxTotal             *int64
	)
	if err := db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM projects`).Scan(&count); err != nil {
		t.Fatalf("counting projects: %v", err)
	}
	if count != 1 {
		t.Fatalf("projects holds %d rows, want exactly the bootstrap one", count)
	}
	if err := db.Pool().QueryRow(t.Context(), `
		SELECT uid, slug, name, description, active, storage_prefix, max_file_bytes, max_total_bytes
		FROM projects`).Scan(&uid, &slug, &name, &desc, &active, &prefix, &maxFile, &maxTotal); err != nil {
		t.Fatalf("reading the bootstrap project: %v", err)
	}
	if uid != project.BootstrapUID || slug != project.BootstrapSlug || name != "Veselice" || desc != "" ||
		!active || prefix != "" || maxFile != nil || maxTotal != nil {
		t.Errorf("bootstrap row = %s/%s/%s/%q/%v/%q/%v/%v", uid, slug, name, desc, active, prefix, maxFile, maxTotal)
	}
}

// TestProjectsTable_checks verifies the table's CHECK constraints refuse what
// the store's validation refuses, so a write that bypasses the store cannot
// store a malformed slug, prefix or limit either.
func TestProjectsTable_checks(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	tests := []struct {
		name   string
		column string
		value  any
	}{
		{name: "uppercase slug", column: "slug", value: "Bad"},
		{name: "leading hyphen slug", column: "slug", value: "-bad"},
		{name: "too long slug", column: "slug", value: strings.Repeat("a", 41)},
		{name: "empty name", column: "name", value: ""},
		{name: "absolute prefix", column: "storage_prefix", value: "/projects/x/"},
		{name: "prefix without slash", column: "storage_prefix", value: "projects/x"},
		{name: "zero file limit", column: "max_file_bytes", value: int64(0)},
		{name: "negative total limit", column: "max_total_bytes", value: int64(-5)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := `INSERT INTO projects (uid, slug, name) VALUES ('pjcheck', 'check', 'Check')`
			if _, err := db.Pool().Exec(t.Context(), query); err != nil {
				t.Fatalf("inserting the row under test: %v", err)
			}
			t.Cleanup(func() {
				_, _ = db.Pool().Exec(context.Background(), `DELETE FROM projects WHERE uid = 'pjcheck'`)
			})
			update := "UPDATE projects SET " + pgx.Identifier{tt.column}.Sanitize() + " = $1 WHERE uid = 'pjcheck'"
			if _, err := db.Pool().Exec(t.Context(), update, tt.value); err == nil {
				t.Errorf("setting %s = %v was accepted, want a CHECK violation", tt.column, tt.value)
			}
		})
	}
}
