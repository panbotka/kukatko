//go:build integration

package photos_test

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
)

// mustCreateArchived creates a photo with the given hash and stamps its
// archived_at to a controlled time so the retention cutoff can be exercised
// deterministically. It returns the new photo's UID.
func mustCreateArchived(
	t *testing.T, store *photos.Store, db *database.DB, ctx context.Context, hash string, at time.Time,
) string {
	t.Helper()
	created, err := store.Create(ctx, samplePhoto(hash))
	if err != nil {
		t.Fatalf("create %s: %v", hash, err)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE photos SET archived_at = $2 WHERE uid = $1", created.UID, at); err != nil {
		t.Fatalf("stamping archived_at for %s: %v", hash, err)
	}
	return created.UID
}

// TestListArchivedUIDs covers the cutoff, ordering, the all-archived (nil before)
// case and pagination.
func TestListArchivedUIDs(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	now := time.Now()
	old1 := mustCreateArchived(t, store, db, ctx, "old1", now.Add(-72*time.Hour))
	old2 := mustCreateArchived(t, store, db, ctx, "old2", now.Add(-48*time.Hour))
	mustCreateArchived(t, store, db, ctx, "recent", now.Add(-time.Hour))

	// A live (non-archived) photo must never appear.
	if _, err := store.Create(ctx, samplePhoto("live")); err != nil {
		t.Fatalf("create live: %v", err)
	}

	cutoff := now.Add(-24 * time.Hour)

	expired, err := store.ListArchivedUIDs(ctx, &cutoff, 100, 0)
	if err != nil {
		t.Fatalf("ListArchivedUIDs(before cutoff): %v", err)
	}
	if want := []string{old1, old2}; !equalStrings(expired, want) {
		t.Fatalf("expired = %v, want oldest-first %v", expired, want)
	}

	all, err := store.ListArchivedUIDs(ctx, nil, 100, 0)
	if err != nil {
		t.Fatalf("ListArchivedUIDs(all): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all archived = %v, want 3 entries", all)
	}

	// Pagination: limit 1, offset 1 returns the second-oldest only.
	page, err := store.ListArchivedUIDs(ctx, &cutoff, 1, 1)
	if err != nil {
		t.Fatalf("ListArchivedUIDs(paged): %v", err)
	}
	if len(page) != 1 || page[0] != old2 {
		t.Fatalf("paged = %v, want [%s]", page, old2)
	}
}

// equalStrings reports whether two string slices are identical in order.
func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
