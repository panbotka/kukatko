//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/pgvector/pgvector-go"

	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database, so they intentionally
// do not run in parallel.

func TestNew_ping(t *testing.T) {
	db := dbtest.New(t)
	if err := db.Ping(t.Context()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestMigrate_extensionsPresent(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()

	for _, ext := range []string{"vector", "unaccent"} {
		var present bool
		err := db.Pool().QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)", ext).Scan(&present)
		if err != nil {
			t.Fatalf("querying extension %q: %v", ext, err)
		}
		if !present {
			t.Errorf("extension %q is not installed", ext)
		}
	}
}

func TestMigrate_schemaMigrationsRecorded(t *testing.T) {
	db := dbtest.New(t)

	var count int
	err := db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if count < 1 {
		t.Errorf("schema_migrations has %d rows, want at least 1", count)
	}
}

func TestMigrate_idempotent(t *testing.T) {
	db := dbtest.New(t) // first application happens here

	applied, err := db.Migrate(t.Context()) // second application must be a no-op
	if err != nil {
		t.Fatalf("re-running Migrate: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("second Migrate applied %v, want none", applied)
	}
}

func TestMigrate_halfvecUsable(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	pool := db.Pool()

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS dbtest_halfvec")
	})
	if _, err := pool.Exec(ctx,
		"CREATE TABLE dbtest_halfvec (id int PRIMARY KEY, emb halfvec(3))"); err != nil {
		t.Fatalf("creating halfvec table: %v", err)
	}

	want := pgvector.NewHalfVector([]float32{1, 2, 3})
	if _, err := pool.Exec(ctx,
		"INSERT INTO dbtest_halfvec (id, emb) VALUES (1, $1)", want); err != nil {
		t.Fatalf("inserting halfvec: %v", err)
	}

	var got pgvector.HalfVector
	if err := pool.QueryRow(ctx,
		"SELECT emb FROM dbtest_halfvec WHERE id = 1").Scan(&got); err != nil {
		t.Fatalf("scanning halfvec: %v", err)
	}

	gotSlice, wantSlice := got.Slice(), want.Slice()
	if len(gotSlice) != len(wantSlice) {
		t.Fatalf("halfvec length = %d, want %d", len(gotSlice), len(wantSlice))
	}
	for i := range wantSlice {
		if gotSlice[i] != wantSlice[i] {
			t.Errorf("halfvec[%d] = %v, want %v", i, gotSlice[i], wantSlice[i])
		}
	}
}

func TestTruncateAll_preservesMigrations(t *testing.T) {
	db := dbtest.New(t)
	ctx := t.Context()
	pool := db.Pool()

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS dbtest_trunc")
	})
	if _, err := pool.Exec(ctx, "CREATE TABLE dbtest_trunc (id int)"); err != nil {
		t.Fatalf("creating temp table: %v", err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO dbtest_trunc (id) VALUES (1), (2)"); err != nil {
		t.Fatalf("seeding temp table: %v", err)
	}

	dbtest.TruncateAll(t, db)

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM dbtest_trunc").Scan(&rows); err != nil {
		t.Fatalf("counting after truncate: %v", err)
	}
	if rows != 0 {
		t.Errorf("dbtest_trunc has %d rows after TruncateAll, want 0", rows)
	}

	var migrations int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrations); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if migrations == 0 {
		t.Error("TruncateAll removed schema_migrations rows; it must preserve them")
	}
}
