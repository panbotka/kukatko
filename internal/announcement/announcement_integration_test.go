//go:build integration

package announcement_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/announcement"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStore returns an announcement.Store plus the auth store used to seed authors
// and the database handle, over a freshly truncated integration database.
func newStore(t *testing.T) (*announcement.Store, *auth.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return announcement.NewStore(db.Pool()), auth.NewStore(db.Pool()), db
}

// makeUser inserts a maintainer account with the given uid/username and returns it.
func makeUser(t *testing.T, store *auth.Store, uid, username string) string {
	t.Helper()
	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		PasswordHash: "x",
		Role:         auth.RoleMaintainer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// setEntry builds an announcement.set audit entry stamped with actorUID.
func setEntry(actorUID string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: audit.ActionAnnouncementSet, TargetType: "announcement"}
}

// clearEntry builds an announcement.clear audit entry stamped with actorUID.
func clearEntry(actorUID string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: audit.ActionAnnouncementClear, TargetType: "announcement"}
}

// countAudit returns how many audit_log rows exist for the given action.
func countAudit(t *testing.T, db *database.DB, action string) int {
	t.Helper()
	n, err := audit.NewStore(db.Pool()).Count(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("counting audit %q: %v", action, err)
	}
	return n
}

// TestGetEmpty returns ErrNotFound when nothing is published.
func TestGetEmpty(t *testing.T) {
	store, _, _ := newStore(t)
	if _, err := store.Get(context.Background()); !errors.Is(err, announcement.ErrNotFound) {
		t.Fatalf("Get on empty error = %v, want ErrNotFound", err)
	}
}

// TestSetGetClear exercises publish → read → clear, and that publishing is an
// upsert (a second Set replaces the single row rather than adding another).
func TestSetGetClear(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	author := makeUser(t, users, "an_author", "author")

	published, err := store.Set(ctx, "Downtime tonight", announcement.LevelWarning, author, setEntry(author))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if published.Message != "Downtime tonight" || published.Level != announcement.LevelWarning ||
		published.AuthorUID != author || published.UpdatedAt.IsZero() {
		t.Fatalf("unexpected published: %+v", published)
	}

	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Message != "Downtime tonight" || got.Level != announcement.LevelWarning {
		t.Fatalf("Get mismatch: %+v", got)
	}

	// A second Set replaces the message and level; the table still holds one row.
	replaced, err := store.Set(ctx, "All clear", announcement.LevelInfo, author, setEntry(author))
	if err != nil {
		t.Fatalf("Set replace: %v", err)
	}
	if replaced.Message != "All clear" || replaced.Level != announcement.LevelInfo {
		t.Fatalf("unexpected replaced: %+v", replaced)
	}
	if n := countRows(t, db); n != 1 {
		t.Fatalf("announcements row count = %d, want 1 (upsert)", n)
	}
	if got := countAudit(t, db, audit.ActionAnnouncementSet); got != 2 {
		t.Fatalf("announcement.set audit rows = %d, want 2", got)
	}

	if err := store.Clear(ctx, clearEntry(author)); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := store.Get(ctx); !errors.Is(err, announcement.ErrNotFound) {
		t.Fatalf("Get after clear error = %v, want ErrNotFound", err)
	}
	if got := countAudit(t, db, audit.ActionAnnouncementClear); got != 1 {
		t.Fatalf("announcement.clear audit rows = %d, want 1", got)
	}
}

// TestSetValidation rejects a blank message and an unrecognised level, and
// defaults an empty level to info.
func TestSetValidation(t *testing.T) {
	store, users, _ := newStore(t)
	ctx := context.Background()
	author := makeUser(t, users, "an_val", "val")

	if _, err := store.Set(ctx, "   ", announcement.LevelInfo, author, setEntry(author)); !errors.Is(err, announcement.ErrEmptyMessage) {
		t.Fatalf("Set blank message error = %v, want ErrEmptyMessage", err)
	}
	if _, err := store.Set(ctx, "hi", "danger", author, setEntry(author)); !errors.Is(err, announcement.ErrInvalidLevel) {
		t.Fatalf("Set bad level error = %v, want ErrInvalidLevel", err)
	}
	defaulted, err := store.Set(ctx, "hi", "", author, setEntry(author))
	if err != nil {
		t.Fatalf("Set empty level: %v", err)
	}
	if defaulted.Level != announcement.LevelInfo {
		t.Fatalf("empty level = %q, want info", defaulted.Level)
	}
}

// TestAuthorCascadesToNull checks that deleting the author leaves the announcement
// standing with a NULL (empty) author, per ON DELETE SET NULL.
func TestAuthorCascadesToNull(t *testing.T) {
	store, users, db := newStore(t)
	ctx := context.Background()
	author := makeUser(t, users, "an_cascade", "cascade")

	if _, err := store.Set(ctx, "Stays up", announcement.LevelInfo, author, setEntry(author)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := db.Pool().Exec(ctx, "DELETE FROM users WHERE uid = $1", author); err != nil {
		t.Fatalf("deleting author: %v", err)
	}
	got, err := store.Get(ctx)
	if err != nil {
		t.Fatalf("Get after author delete: %v", err)
	}
	if got.Message != "Stays up" || got.AuthorUID != "" {
		t.Fatalf("after author delete = %+v, want message kept and empty author", got)
	}
}

// countRows returns the number of rows in the announcements table.
func countRows(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM announcements").Scan(&n); err != nil {
		t.Fatalf("counting announcements: %v", err)
	}
	return n
}
