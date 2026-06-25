// Package dbtest provides helpers for integration tests that need a real
// PostgreSQL database. It connects to the database named by
// KUKATKO_TEST_DATABASE_URL, applies all migrations, and offers per-test
// isolation by truncating data tables. When the environment variable is unset
// the helpers skip the calling test, so the fast unit-test gate (make test)
// never requires a database; the DB-backed suite runs under make
// test-integration.
package dbtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
)

// EnvTestDatabaseURL is the environment variable holding the DSN of the
// integration-test database, which is separate from the production database and
// safe to truncate.
const EnvTestDatabaseURL = "KUKATKO_TEST_DATABASE_URL"

// New connects to the integration-test database, applies all migrations, and
// returns a ready-to-use DB. The calling test is skipped when
// KUKATKO_TEST_DATABASE_URL is unset. The returned DB is closed automatically
// via t.Cleanup; the test is failed on any connection or migration error.
func New(t *testing.T) *database.DB {
	t.Helper()

	url := os.Getenv(EnvTestDatabaseURL)
	if url == "" {
		t.Skipf("%s not set; skipping integration test", EnvTestDatabaseURL)
	}

	ctx := t.Context()
	db, err := database.New(ctx, config.DatabaseConfig{
		URL:          url,
		MaxOpenConns: 5,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(db.Close)

	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}
	return db
}

// TruncateAll empties every base table in the public schema except
// schema_migrations, giving a test a clean slate while preserving the applied
// migration history. Identity sequences are reset and foreign-key dependents are
// cascaded. The test is failed on any error.
func TruncateAll(t *testing.T, db *database.DB) {
	t.Helper()

	ctx := t.Context()
	tables, err := dataTables(ctx, db)
	if err != nil {
		t.Fatalf("listing tables to truncate: %v", err)
	}
	if len(tables) == 0 {
		return
	}

	stmt := "TRUNCATE TABLE " + strings.Join(tables, ", ") + " RESTART IDENTITY CASCADE"
	if _, err := db.Pool().Exec(ctx, stmt); err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}

// dataTables returns the sanitized, quoted names of every base table in the
// public schema except schema_migrations (which must survive truncation so
// migrations are not re-applied).
func dataTables(ctx context.Context, db *database.DB) ([]string, error) {
	const query = `SELECT tablename FROM pg_tables
		WHERE schemaname = 'public' AND tablename <> 'schema_migrations'`

	rows, err := db.Pool().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying public tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scanning table name: %w", err)
		}
		tables = append(tables, pgx.Identifier{name}.Sanitize())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating table names: %w", err)
	}
	return tables, nil
}
