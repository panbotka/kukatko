//go:build integration

package database_test

import (
	"os"
	"sync"
	"testing"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// TestMigrate_concurrentRunsSerialise starts two migrators at once against one
// empty schema — the shape of a `serve` start racing a CLI subcommand — and
// verifies the advisory lock makes them take turns: both succeed, exactly one
// applies the whole chain, the other finds the schema up to date, and nothing is
// recorded twice. Without the lock the pair races CREATE TABLE IF NOT EXISTS and
// the first files, and one of them fails.
func TestMigrate_concurrentRunsSerialise(t *testing.T) {
	first := freshSchemaDB(t)
	second := sameSchemaDB(t, first)

	pending, err := first.Pending(t.Context())
	if err != nil {
		t.Fatalf("Pending on an empty schema: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("Pending on an empty schema reported nothing")
	}
	var created bool
	if err := first.Pool().QueryRow(t.Context(),
		"SELECT to_regclass(current_schema() || '.schema_migrations') IS NOT NULL").Scan(&created); err != nil {
		t.Fatalf("looking for schema_migrations: %v", err)
	}
	if created {
		t.Error("Pending created schema_migrations; it must only read")
	}

	type result struct {
		applied []string
		err     error
	}
	results := make([]result, 2)
	var start, done sync.WaitGroup
	start.Add(1)
	for i, db := range []*database.DB{first, second} {
		done.Go(func() {
			start.Wait()
			applied, err := db.Migrate(t.Context())
			results[i] = result{applied: applied, err: err}
		})
	}
	start.Done()
	done.Wait()

	total := 0
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("migrator %d: %v", i, r.err)
		}
		total += len(r.applied)
	}
	if total != len(pending) || (len(results[0].applied) != 0 && len(results[1].applied) != 0) {
		t.Errorf("applied %d + %d files, want one migrator to apply all %d and the other none",
			len(results[0].applied), len(results[1].applied), len(pending))
	}

	var rows, distinct int
	if err := first.Pool().QueryRow(t.Context(),
		"SELECT count(*), count(DISTINCT version) FROM schema_migrations").Scan(&rows, &distinct); err != nil {
		t.Fatalf("counting schema_migrations: %v", err)
	}
	if rows != len(pending) || distinct != rows {
		t.Errorf("schema_migrations has %d rows (%d distinct), want %d", rows, distinct, len(pending))
	}

	// Both migrators gave the lock back: a third run on either pool is a no-op
	// rather than a wait on a lock a pooled connection still holds.
	for i, db := range []*database.DB{second, first} {
		applied, err := db.Migrate(t.Context())
		if err != nil || len(applied) != 0 {
			t.Errorf("rerun %d = %v, %v; want nothing applied", i, applied, err)
		}
		left, err := db.Pending(t.Context())
		if err != nil || len(left) != 0 {
			t.Errorf("Pending after migrating = %v, %v; want none", left, err)
		}
	}
}

// sameSchemaDB opens a second pool on the throwaway schema of a freshSchemaDB
// handle, so two migrators can work the same empty schema from separate
// sessions. It is closed before the schema is dropped (cleanups run in reverse).
func sameSchemaDB(t *testing.T, db *database.DB) *database.DB {
	t.Helper()
	var schema string
	if err := db.Pool().QueryRow(t.Context(), "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatalf("reading the fresh schema's name: %v", err)
	}
	other, err := database.New(t.Context(), config.DatabaseConfig{
		URL:          dsnWithParam(os.Getenv(dbtest.EnvTestDatabaseURL), "search_path", schema+",public"),
		MaxOpenConns: 2, MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("connecting a second pool to %s: %v", schema, err)
	}
	t.Cleanup(other.Close)
	return other
}
