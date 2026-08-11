//go:build integration

package database

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// These tests live inside the package (rather than in database_test) because
// they exercise the unexported extension bootstrap that has to run before the
// pool — and therefore before anything the exported API can reach. They connect
// to the database named by KUKATKO_TEST_DATABASE_URL directly instead of through
// internal/database/dbtest, which would be an import cycle.
//
// The env var name is duplicated from dbtest.EnvTestDatabaseURL for that reason.
const envTestDatabaseURL = "KUKATKO_TEST_DATABASE_URL"

// probeExtension is an extension the create branch can be tested with: it ships
// with a stock PostgreSQL (contrib), carries no types the catalogue depends on,
// and is not one Kukátko installs — so creating and dropping it around a test
// cannot disturb the test database. Tests skip when the server lacks it.
const probeExtension = "hstore"

// testConn opens a plain connection to the integration-test database, skipping
// the test when KUKATKO_TEST_DATABASE_URL is unset and failing it when the
// connection cannot be opened. The connection is closed via t.Cleanup.
func testConn(t *testing.T) *pgx.Conn {
	t.Helper()

	dsn := os.Getenv(envTestDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", envTestDatabaseURL)
	}

	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("connecting to the test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

// extensionInstalled reports whether the connected database currently lists the
// named extension, failing the test when the catalog cannot be read.
func extensionInstalled(t *testing.T, conn *pgx.Conn, name string) bool {
	t.Helper()

	var installed bool
	err := conn.QueryRow(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)", name).Scan(&installed)
	if err != nil {
		t.Fatalf("querying extension %q: %v", name, err)
	}
	return installed
}

// TestEnsureExtension_installsWhenMissing covers the branch a fresh database
// takes: the extension is absent, so it gets created. Without it `kukatko serve`
// cannot start at all against a database that has never been migrated — the pool
// registers the pgvector types on connect and there are none.
func TestEnsureExtension_installsWhenMissing(t *testing.T) {
	conn := testConn(t)
	ctx := t.Context()

	var available bool
	err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = $1)", probeExtension).Scan(&available)
	if err != nil {
		t.Fatalf("querying available extensions: %v", err)
	}
	if !available {
		t.Skipf("this server does not ship the %s extension; skipping", probeExtension)
	}

	// Start from "not installed" whatever the database's history, and leave it
	// that way: the probe extension is not part of Kukátko's schema.
	dropProbe := func() {
		_, _ = conn.Exec(context.Background(), "DROP EXTENSION IF EXISTS "+pgx.Identifier{probeExtension}.Sanitize())
	}
	dropProbe()
	t.Cleanup(dropProbe)

	if extensionInstalled(t, conn, probeExtension) {
		t.Fatalf("extension %q is still installed after the drop; the test cannot prove anything", probeExtension)
	}

	if err := ensureExtension(ctx, conn, probeExtension); err != nil {
		t.Fatalf("ensureExtension(%q): %v", probeExtension, err)
	}
	if !extensionInstalled(t, conn, probeExtension) {
		t.Errorf("extension %q is not installed after ensureExtension", probeExtension)
	}
}

// TestEnsureExtension_noopWhenInstalled covers the branch every existing
// deployment takes. It must issue no CREATE EXTENSION — a managed or shared
// Postgres may not let the application role create one, and a startup that tried
// anyway would fail on a database that was already perfectly usable.
func TestEnsureExtension_noopWhenInstalled(t *testing.T) {
	conn := testConn(t)

	if !extensionInstalled(t, conn, vectorExtension) {
		t.Fatalf("the test database has no %s extension; it is not a Kukátko database", vectorExtension)
	}

	if err := ensureExtension(t.Context(), conn, vectorExtension); err != nil {
		t.Fatalf("ensureExtension(%q) on an installed extension: %v", vectorExtension, err)
	}
	if !extensionInstalled(t, conn, vectorExtension) {
		t.Errorf("extension %q disappeared", vectorExtension)
	}
}

// TestEnsureExtension_reportsAnUncreatableExtension verifies the error path is
// wrapped and names the extension, so an operator reading a failed startup knows
// which one the server is missing rather than only that some SQL failed.
func TestEnsureExtension_reportsAnUncreatableExtension(t *testing.T) {
	conn := testConn(t)

	const missing = "kukatko_no_such_extension"
	err := ensureExtension(t.Context(), conn, missing)
	if err == nil {
		t.Fatalf("ensureExtension(%q) = nil, want an error", missing)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error %q does not name the extension %q", err, missing)
	}
}

// TestEnsureVectorExtension_overAConnConfig verifies the wrapper the pool
// bootstrap actually calls: it opens its own connection from a pgx config and
// succeeds against a database that already has the extension.
func TestEnsureVectorExtension_overAConnConfig(t *testing.T) {
	dsn := os.Getenv(envTestDatabaseURL)
	if dsn == "" {
		t.Skipf("%s not set; skipping integration test", envTestDatabaseURL)
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parsing the test DSN: %v", err)
	}
	if err := ensureVectorExtension(t.Context(), cfg); err != nil {
		t.Fatalf("ensureVectorExtension: %v", err)
	}
}
