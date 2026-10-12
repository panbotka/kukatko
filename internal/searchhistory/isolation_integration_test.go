//go:build integration

package searchhistory_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/searchhistory"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so
// they do not run in parallel. One account (A's member) searches in both
// projects, which is exactly the case the per-(user, project) key exists for.

// newFixture seeds the two-project fixture over a freshly truncated database
// and returns it with a store.
func newFixture(t *testing.T) (*projecttest.Fixture, *searchhistory.Store) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return projecttest.Seed(t, db.Pool()), searchhistory.NewStore(db.Pool())
}

// queries lists the stored queries of user in projectUID, newest first.
func queries(t *testing.T, store *searchhistory.Store, projectUID, user string) []string {
	t.Helper()
	entries, err := store.List(t.Context(), projectUID, user)
	if err != nil {
		t.Fatalf("List(%s): %v", projectUID, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Query)
	}
	return out
}

// record remembers query for user in projectUID, failing t on error.
func record(t *testing.T, store *searchhistory.Store, projectUID, user, query string) {
	t.Helper()
	if err := store.Record(t.Context(), projectUID, user, query); err != nil {
		t.Fatalf("Record(%s, %q): %v", projectUID, query, err)
	}
}

// TestStore_historyIsPerProject asserts a query recorded in A is listed in A
// only, and that the same query recorded in both projects is two entries.
func TestStore_historyIsPerProject(t *testing.T) {
	f, store := newFixture(t)
	user := f.A.UserUID

	record(t, store, f.A.ProjectUID, user, "jen v A")
	record(t, store, f.B.ProjectUID, user, "jen v B")
	record(t, store, f.A.ProjectUID, user, "oba")
	record(t, store, f.B.ProjectUID, user, "oba")

	if got, want := queries(t, store, f.A.ProjectUID, user), []string{"oba", "jen v A"}; !slices.Equal(got, want) {
		t.Errorf("history in A = %q, want %q", got, want)
	}
	if got, want := queries(t, store, f.B.ProjectUID, user), []string{"oba", "jen v B"}; !slices.Equal(got, want) {
		t.Errorf("history in B = %q, want %q", got, want)
	}
}

// TestStore_capIsPerProject fills A's history past the cap and asserts the
// prune trimmed A back to MaxEntries without deleting anything in B.
func TestStore_capIsPerProject(t *testing.T) {
	f, store := newFixture(t)
	user := f.A.UserUID

	record(t, store, f.B.ProjectUID, user, "nejstarší v B")
	for i := range searchhistory.MaxEntries + 5 {
		record(t, store, f.A.ProjectUID, user, fmt.Sprintf("dotaz %02d", i))
	}

	if got := queries(t, store, f.A.ProjectUID, user); len(got) != searchhistory.MaxEntries {
		t.Errorf("history in A holds %d entries, want %d", len(got), searchhistory.MaxEntries)
	}
	if got, want := queries(t, store, f.B.ProjectUID, user), []string{"nejstarší v B"}; !slices.Equal(got, want) {
		t.Errorf("history in B = %q after A overflowed, want %q", got, want)
	}
}

// TestStore_clearIsPerProject asserts clearing A's history leaves B's.
func TestStore_clearIsPerProject(t *testing.T) {
	f, store := newFixture(t)
	user := f.A.UserUID

	record(t, store, f.A.ProjectUID, user, "v A")
	record(t, store, f.B.ProjectUID, user, "v B")
	if err := store.Clear(t.Context(), f.A.ProjectUID, user); err != nil {
		t.Fatalf("Clear(A): %v", err)
	}

	if got := queries(t, store, f.A.ProjectUID, user); len(got) != 0 {
		t.Errorf("history in A after Clear = %q, want empty", got)
	}
	if got, want := queries(t, store, f.B.ProjectUID, user), []string{"v B"}; !slices.Equal(got, want) {
		t.Errorf("history in B after clearing A = %q, want %q", got, want)
	}
}

// TestStore_noProject asserts every method refuses an empty project with
// ErrNoProject and writes nothing.
func TestStore_noProject(t *testing.T) {
	f, store := newFixture(t)
	ctx := t.Context()
	user := f.A.UserUID

	if err := store.Record(ctx, "", user, "x"); !errors.Is(err, searchhistory.ErrNoProject) {
		t.Errorf("Record = %v, want ErrNoProject", err)
	}
	if _, err := store.List(ctx, "", user); !errors.Is(err, searchhistory.ErrNoProject) {
		t.Errorf("List = %v, want ErrNoProject", err)
	}
	if err := store.Clear(ctx, "", user); !errors.Is(err, searchhistory.ErrNoProject) {
		t.Errorf("Clear = %v, want ErrNoProject", err)
	}
	if got := queries(t, store, f.A.ProjectUID, user); len(got) != 0 {
		t.Errorf("history in A = %q after the refused Record, want empty", got)
	}
}
