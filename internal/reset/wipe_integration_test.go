//go:build integration

package reset_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/reset"
	"github.com/panbotka/kukatko/internal/thumb"
)

// TestPreflight_countsWithoutDeleting verifies the default run is a measurement:
// it reports the rows and objects a wipe would remove and removes none of them.
func TestPreflight_countsWithoutDeleting(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)
	keysBefore := env.storedKeys(t)

	pre, err := env.svc.Preflight(t.Context(), reset.Options{})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if pre.Connection.Database != env.target.Database {
		t.Errorf("connected database = %q, want %q", pre.Connection.Database, env.target.Database)
	}
	if got := countOf(pre.Counts.Catalogue, "photos"); got != 2 {
		t.Errorf("preflight photos = %d, want 2", got)
	}
	if got := countOf(pre.Counts.Preserved, "users"); got != 1 {
		t.Errorf("preflight users = %d, want 1", got)
	}
	if pre.Storage.Referenced.Originals != 2 || pre.Storage.Referenced.Sidecars != 2 {
		t.Errorf("referenced = %+v, want 2 originals and 2 sidecars", pre.Storage.Referenced)
	}
	if want := 2 * len(thumb.SizeNames()); pre.Storage.Referenced.Thumbnails != want {
		t.Errorf("referenced thumbnails = %d, want %d", pre.Storage.Referenced.Thumbnails, want)
	}
	if pre.Storage.Sweep || pre.Storage.Stored.Total() != 0 {
		t.Errorf("storage plan = %+v, want no sweep measurement without --orphan-sweep", pre.Storage)
	}

	// Nothing moved: the dry run neither truncated a table nor removed an object.
	if got := env.count(t, "photos"); got != 2 {
		t.Errorf("photos after a dry run = %d, want 2", got)
	}
	if got := env.storedKeys(t); len(got) != len(keysBefore) {
		t.Errorf("store holds %d key(s) after a dry run, want %d", len(got), len(keysBefore))
	}

	// And the service refuses to delete even if asked directly, unless Execute is set.
	if _, err := env.svc.Execute(t.Context(), reset.Options{Confirm: env.target.Database},
		pre.Counts); !errors.Is(err, reset.ErrNotExecuting) {
		t.Errorf("Execute without Options.Execute = %v, want ErrNotExecuting", err)
	}
	if got := env.count(t, "photos"); got != 2 {
		t.Errorf("photos after a refused execute = %d, want 2", got)
	}
}

// TestExecute_wrongTypedNameAborts verifies a confirmation that is not the target
// database's name — including the empty one — deletes nothing.
func TestExecute_wrongTypedNameAborts(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)
	pre, err := env.svc.Preflight(t.Context(), reset.Options{})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}

	for _, typed := range []string{"", "kukatko", env.target.Database + " ", "KUKATKO_TEST"} {
		if typed == env.target.Database {
			continue
		}
		opts := reset.Options{Execute: true, Confirm: typed}
		if _, err := env.svc.Execute(t.Context(), opts, pre.Counts); !errors.Is(err, reset.ErrConfirmationMismatch) {
			t.Errorf("Execute with typed %q = %v, want ErrConfirmationMismatch", typed, err)
		}
	}
	if got := env.count(t, "photos"); got != 2 {
		t.Errorf("photos after mistyped confirmations = %d, want 2", got)
	}
	if len(env.storedKeys(t)) == 0 {
		t.Error("the store was emptied by a mistyped confirmation")
	}
}

// TestExecute_targetMismatchAborts verifies a service pointed at a database the
// connection does not serve refuses to do anything — the guard against a typo or
// a stale environment variable reaching the wrong database.
func TestExecute_targetMismatchAborts(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)

	wrong := reset.New(reset.Config{
		Pool:    env.db.Pool(),
		Target:  reset.Target{Host: env.target.Host, Port: env.target.Port, Database: "kukatko_production"},
		Storage: env.fs,
	})
	if _, err := wrong.Preflight(t.Context(), reset.Options{}); !errors.Is(err, reset.ErrTargetMismatch) {
		t.Errorf("Preflight against a mismatched target = %v, want ErrTargetMismatch", err)
	}
	opts := reset.Options{Execute: true, Confirm: "kukatko_production"}
	if _, err := wrong.Execute(t.Context(), opts, reset.Counts{}); !errors.Is(err, reset.ErrTargetMismatch) {
		t.Errorf("Execute against a mismatched target = %v, want ErrTargetMismatch", err)
	}
	if got := env.count(t, "photos"); got != 2 {
		t.Errorf("photos after a mismatched target = %d, want 2", got)
	}
}

// TestExecute_emptiesTheCatalogueAndPreservesAccounts is the main case: a
// confirmed run empties every catalogue table, leaves the accounts, the
// announcement, the audit trail and the migration history untouched, removes the
// objects the catalogue referenced, and records itself in the audit log.
func TestExecute_emptiesTheCatalogueAndPreservesAccounts(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)
	migrationsBefore := env.count(t, "schema_migrations")
	auditBefore := env.count(t, "audit_log")

	pre, err := env.svc.Preflight(t.Context(), reset.Options{})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	result, err := env.svc.Execute(t.Context(), env.executeOptions(), pre.Counts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if result.Before.Rows() == 0 {
		t.Error("before counts are empty; the summary would prove nothing")
	}
	if result.After.Rows() != 0 {
		t.Errorf("after counts = %d row(s), want 0: %+v", result.After.Rows(), result.After.NonEmpty())
	}
	for _, table := range reset.CatalogueTables() {
		if got := env.count(t, table); got != 0 {
			t.Errorf("catalogue table %s still holds %d row(s)", table, got)
		}
	}
	for table, want := range map[string]int64{
		"users": 1, "sessions": 1, "api_tokens": 1, "announcements": 1,
		"schema_migrations": migrationsBefore,
	} {
		if got := env.count(t, table); got != want {
			t.Errorf("preserved table %s = %d row(s), want %d", table, got, want)
		}
	}

	if keys := env.storedKeys(t); len(keys) != 0 {
		t.Errorf("store still holds %v", keys)
	}
	if result.Storage.Deleted == 0 || result.Storage.Failed != 0 {
		t.Errorf("storage result = %+v, want objects deleted and none failed", result.Storage)
	}
	if result.Storage.ThumbCacheCleared != 2 {
		t.Errorf("thumb cache cleared for %d hash(es), want 2", result.Storage.ThumbCacheCleared)
	}
	assertAuditedReset(t, env, auditBefore)
}

// assertAuditedReset verifies the wipe appended exactly one library.reset entry
// to the trail it preserved, carrying the operator and the counts.
func assertAuditedReset(t *testing.T, env *resetEnv, auditBefore int64) {
	t.Helper()

	if got := env.count(t, "audit_log"); got != auditBefore+1 {
		t.Fatalf("audit_log = %d row(s), want %d (the seeded entries plus the reset)", got, auditBefore+1)
	}
	records, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionLibraryReset, Limit: 10})
	if err != nil {
		t.Fatalf("listing audit records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("found %d library.reset entries, want 1", len(records))
	}
	details := records[0].Details
	if details["operator"] != "test@integration" {
		t.Errorf("audit operator = %v, want test@integration", details["operator"])
	}
	if details["database"] != env.target.Database {
		t.Errorf("audit database = %v, want %q", details["database"], env.target.Database)
	}
	if rows, ok := details["rows_deleted"].(float64); !ok || rows == 0 {
		t.Errorf("audit rows_deleted = %v, want a non-zero count", details["rows_deleted"])
	}
}

// TestPreflight_schemaDriftAborts verifies a table nobody classified stops the
// wipe instead of being silently left behind.
func TestPreflight_schemaDriftAborts(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)

	env.exec(t, `CREATE TABLE photo_books (id bigserial PRIMARY KEY)`)
	t.Cleanup(func() {
		if _, err := env.db.Pool().Exec(context.Background(), `DROP TABLE IF EXISTS photo_books`); err != nil {
			t.Errorf("dropping the stray table: %v", err)
		}
	})

	_, err := env.svc.Preflight(t.Context(), reset.Options{})
	if !errors.Is(err, reset.ErrSchemaDrift) {
		t.Fatalf("Preflight with an unclassified table = %v, want ErrSchemaDrift", err)
	}
	if got := env.count(t, "photos"); got != 2 {
		t.Errorf("photos after a refused preflight = %d, want 2", got)
	}
}

// TestExecute_orphanSweepIsConfinedToOwnedPrefixes verifies the sweep removes the
// leftovers the catalogue never referenced and leaves every object outside
// Kukátko's prefixes exactly where it was.
func TestExecute_orphanSweepIsConfinedToOwnedPrefixes(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)

	orphans := []string{
		"2019/01/leftover.jpg",
		"thumb/ff/ee/dd/ffeedd_tile_500.jpg",
		"sidecars/2019/01/leftover.jpg.yml",
	}
	foreign := []string{"backups/db/2026-07-31.dump", "README.md", "other-app/state.bin"}
	for _, key := range append(slices.Clone(orphans), foreign...) {
		env.writeObject(t, key, "leftover")
	}

	opts := env.executeOptions()
	opts.OrphanSweep = true
	pre, err := env.svc.Preflight(t.Context(), opts)
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if !pre.Storage.Sweep || pre.Storage.Foreign != len(foreign) {
		t.Errorf("storage plan = %+v, want a sweep reporting %d foreign key(s)", pre.Storage, len(foreign))
	}
	if want := pre.Storage.Referenced.Total() + len(orphans); pre.Storage.Stored.Total() != want {
		t.Errorf("stored total = %d, want %d", pre.Storage.Stored.Total(), want)
	}

	result, err := env.svc.Execute(t.Context(), opts, pre.Counts)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Storage.Foreign != len(foreign) {
		t.Errorf("foreign = %d, want %d", result.Storage.Foreign, len(foreign))
	}
	if result.Storage.Missing != 0 {
		t.Errorf("missing = %d, want 0: a sweep deletes what is there", result.Storage.Missing)
	}
	left := env.storedKeys(t)
	slices.Sort(left)
	want := slices.Clone(foreign)
	slices.Sort(want)
	if !slices.Equal(left, want) {
		t.Errorf("store holds %v, want exactly the foreign keys %v", left, want)
	}
	if !result.Storage.ThumbCacheSwept {
		t.Error("the local thumbnail cache was not swept")
	}
	if _, err := os.Stat(filepath.Join(env.cacheDir, thumb.CacheSubdir)); !os.IsNotExist(err) {
		t.Errorf("thumbnail cache directory still exists (stat error = %v)", err)
	}
}

// countOf returns the row count recorded for one table, or -1 when the table is
// not in the snapshot.
func countOf(counts []reset.TableCount, table string) int64 {
	for _, count := range counts {
		if count.Table == table {
			return count.Rows
		}
	}
	return -1
}

// TestExecute_keepsTheAccountButNotItsPerson pins the one thing a preserved
// account does lose. users.subject_uid points into the subjects table the wipe
// empties, so the reset nulls it in the same transaction — Postgres would
// otherwise refuse the CASCADE-free truncation outright. Everything that makes
// the account an account is untouched.
func TestExecute_keepsTheAccountButNotItsPerson(t *testing.T) {
	env := newResetEnv(t)
	env.seedLibrary(t)

	var linked string
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT subject_uid FROM users WHERE uid = 'usr000000001'`).Scan(&linked); err != nil {
		t.Fatalf("reading the seeded link: %v", err)
	}
	if linked != "sbj000000001" {
		t.Fatalf("seeded link = %q, want sbj000000001", linked)
	}

	pre, err := env.svc.Preflight(t.Context(), reset.Options{})
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if _, err = env.svc.Execute(t.Context(), env.executeOptions(), pre.Counts); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var (
		username string
		role     string
		hash     string
		subject  *string
	)
	if err := env.db.Pool().QueryRow(t.Context(),
		`SELECT username, role, password_hash, subject_uid FROM users WHERE uid = 'usr000000001'`).
		Scan(&username, &role, &hash, &subject); err != nil {
		t.Fatalf("reading the account after the wipe: %v", err)
	}
	if username != "operator" || role != "maintainer" || hash != "hash" {
		t.Errorf("account after the wipe = %s/%s/%s, want operator/maintainer/hash", username, role, hash)
	}
	if subject != nil {
		t.Errorf("linked person after the wipe = %q, want none — that person no longer exists", *subject)
	}

	// The constraint the wipe lifted is back: a link into a library that no
	// longer holds that person is still refused.
	if _, err := env.db.Pool().Exec(t.Context(),
		`UPDATE users SET subject_uid = 'sbj000000001' WHERE uid = 'usr000000001'`); err == nil {
		t.Error("linking to a deleted person succeeded; the foreign key was not restored")
	}
}
