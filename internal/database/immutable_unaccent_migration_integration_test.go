//go:build integration

package database_test

import (
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// TestMigrate_immutableUnaccentSurvivesEmptySearchPath verifies migration 0104:
// with the search_path emptied exactly as pg_restore empties it, the full-text
// wrapper still strips diacritics, and a table with a generated column over it
// can be created — the statement a restore of photos used to fail on, because
// the planner inlines the wrapper's body while it checks the column.
func TestMigrate_immutableUnaccentSurvivesEmptySearchPath(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // the probe table must not outlive the test

	var schema string
	if err := tx.QueryRow(ctx, `
		SELECT n.nspname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE p.proname = 'immutable_unaccent'`).Scan(&schema); err != nil {
		t.Fatalf("locating immutable_unaccent: %v", err)
	}
	fn := pgx.Identifier{schema, "immutable_unaccent"}.Sanitize()
	if _, err := tx.Exec(ctx, "SET LOCAL search_path = ''"); err != nil {
		t.Fatalf("emptying search_path: %v", err)
	}

	var got string
	if err := tx.QueryRow(ctx, "SELECT "+fn+"('Žluťoučký kůň')").Scan(&got); err != nil {
		t.Fatalf("calling immutable_unaccent under an empty search_path: %v", err)
	}
	if want := "Zlutoucky kun"; got != want {
		t.Errorf("immutable_unaccent() = %q, want %q", got, want)
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE unaccent_probe (
		title text,
		fts pg_catalog.tsvector GENERATED ALWAYS AS (
			pg_catalog.to_tsvector('simple', `+fn+`(title))) STORED)`); err != nil {
		t.Fatalf("creating a generated column over immutable_unaccent like pg_restore does: %v", err)
	}
}
