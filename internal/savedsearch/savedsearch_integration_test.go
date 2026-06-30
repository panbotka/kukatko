//go:build integration

package savedsearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/savedsearch"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStore returns a savedsearch.Store plus the auth store used to seed owners and
// the database handle, over a freshly truncated integration database.
func newStore(t *testing.T) (*savedsearch.Store, *auth.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return savedsearch.NewStore(db.Pool()), auth.NewStore(db.Pool()), db
}

// jsonEqual reports whether got and the want literal are the same JSON value,
// ignoring whitespace and object key order. JSONB normalises both on round-trip,
// so a byte-exact comparison of params would be wrong.
func jsonEqual(t *testing.T, got json.RawMessage, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("unmarshal got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatalf("unmarshal want %s: %v", want, err)
	}
	return reflect.DeepEqual(a, b)
}

// makeUser inserts a viewer account with the given uid/username and returns the uid.
func makeUser(t *testing.T, store *auth.Store, uid, username string) string {
	t.Helper()
	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// TestSavedSearchCRUD exercises create, get, update and delete of one record.
func TestSavedSearchCRUD(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "ss_owner", "owner")

	created, err := store.Create(ctx, owner, "Recent", json.RawMessage(`{"sort":"newest"}`))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.UID == "" || created.OwnerUID != owner || created.Name != "Recent" {
		t.Fatalf("unexpected created record: %+v", created)
	}
	if !jsonEqual(t, created.Params, `{"sort":"newest"}`) {
		t.Fatalf("params = %s, want {\"sort\":\"newest\"}", created.Params)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("timestamps not stamped: %+v", created)
	}

	got, err := store.Get(ctx, created.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UID != created.UID || got.Name != "Recent" {
		t.Fatalf("Get mismatch: %+v", got)
	}

	updated, err := store.Update(ctx, created.UID, "Older", json.RawMessage(`{"sort":"oldest"}`))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Older" || !jsonEqual(t, updated.Params, `{"sort":"oldest"}`) {
		t.Fatalf("unexpected updated record: %+v", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) && updated.UpdatedAt.Equal(created.UpdatedAt) {
		t.Logf("updated_at not advanced (clock granularity): %v", updated.UpdatedAt)
	}

	if err := store.Delete(ctx, created.UID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, created.UID); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Fatalf("Get after delete error = %v, want ErrNotFound", err)
	}
}

// TestSavedSearchCreateDefaultsParams checks that an empty params input is stored
// as the empty JSON object so the NOT NULL column is satisfied.
func TestSavedSearchCreateDefaultsParams(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "ss_def", "def")

	created, err := store.Create(ctx, owner, "Empty", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(created.Params) != "{}" {
		t.Fatalf("params = %s, want {}", created.Params)
	}
}

// TestSavedSearchListNewestFirst checks that List returns only the owner's
// searches, newest first.
func TestSavedSearchListNewestFirst(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	alice := makeUser(t, users, "ss_alice", "alice")
	bob := makeUser(t, users, "ss_bob", "bob")

	first, err := store.Create(ctx, alice, "First", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Create first: %v", err)
	}
	second, err := store.Create(ctx, alice, "Second", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Create second: %v", err)
	}
	if _, err := store.Create(ctx, bob, "Bob", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	list, err := store.List(ctx, alice)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List returned %d records, want 2", len(list))
	}
	if list[0].UID != second.UID || list[1].UID != first.UID {
		t.Fatalf("List order = [%s,%s], want [%s,%s]", list[0].UID, list[1].UID, second.UID, first.UID)
	}

	empty, err := store.List(ctx, "ss_nobody")
	if err != nil {
		t.Fatalf("List for unknown owner: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("List for unknown owner returned %d records, want 0", len(empty))
	}
}

// TestSavedSearchMissing checks that Get, Update and Delete return ErrNotFound for
// an unknown uid.
func TestSavedSearchMissing(t *testing.T) {
	store, _, _ := newStore(t)
	ctx := context.Background()

	if _, err := store.Get(ctx, "ss_missing"); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Get error = %v, want ErrNotFound", err)
	}
	if _, err := store.Update(ctx, "ss_missing", "x", json.RawMessage(`{}`)); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Update error = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "ss_missing"); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Delete error = %v, want ErrNotFound", err)
	}
}

// TestSavedSearchCascadesOnUserDelete checks that deleting the owner removes their
// saved searches via the ON DELETE CASCADE foreign key.
func TestSavedSearchCascadesOnUserDelete(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	owner := makeUser(t, users, "ss_cascade", "cascade")

	created, err := store.Create(ctx, owner, "Doomed", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "DELETE FROM users WHERE uid = $1", owner); err != nil {
		t.Fatalf("deleting owner: %v", err)
	}
	if _, err := store.Get(ctx, created.UID); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Fatalf("Get after owner delete error = %v, want ErrNotFound", err)
	}
}
