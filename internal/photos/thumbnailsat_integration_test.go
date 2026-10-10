//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/photos"
)

// thumbnailsAt reads photos.thumbnails_at for uid straight from the table: the
// column is evidence for the processing report, not a field of photos.Photo.
func thumbnailsAt(t *testing.T, db *database.DB, uid string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := db.Pool().QueryRow(t.Context(),
		`SELECT thumbnails_at FROM photos WHERE uid = $1`, uid).Scan(&at); err != nil {
		t.Fatalf("reading thumbnails_at of %s: %v", uid, err)
	}
	return at
}

// TestMarkThumbnails_stamps proves the two stamps' contract: a fresh photo has
// none, MarkThumbnailsPresent sets one only when it is missing, and
// MarkThumbnailsBuilt always moves it — the rebuild a waiting viewer watches for.
func TestMarkThumbnails_stamps(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("ta01"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if at := thumbnailsAt(t, db, created.UID); at != nil {
		t.Fatalf("a fresh photo has thumbnails_at %v, want none", at)
	}
	// A pHash alone is not a thumbnail: the stamp stays empty.
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: created.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	if at := thumbnailsAt(t, db, created.UID); at != nil {
		t.Fatalf("SetPhash stamped thumbnails_at %v, want none", at)
	}

	if err := store.MarkThumbnailsPresent(ctx, created.UID); err != nil {
		t.Fatalf("MarkThumbnailsPresent: %v", err)
	}
	first := thumbnailsAt(t, db, created.UID)
	if first == nil {
		t.Fatal("MarkThumbnailsPresent left no stamp on an unstamped photo")
	}

	// Each statement runs in its own transaction, so now() moves between them.
	if err := store.MarkThumbnailsPresent(ctx, created.UID); err != nil {
		t.Fatalf("MarkThumbnailsPresent again: %v", err)
	}
	if again := thumbnailsAt(t, db, created.UID); again == nil || !again.Equal(*first) {
		t.Errorf("MarkThumbnailsPresent moved an existing stamp: %v → %v", first, again)
	}

	if err := store.MarkThumbnailsBuilt(ctx, created.UID); err != nil {
		t.Fatalf("MarkThumbnailsBuilt: %v", err)
	}
	if rebuilt := thumbnailsAt(t, db, created.UID); rebuilt == nil || !rebuilt.After(*first) {
		t.Errorf("MarkThumbnailsBuilt did not move the stamp: %v → %v", first, rebuilt)
	}

	// An unknown uid is nothing to stamp, not an error.
	if err := store.MarkThumbnailsBuilt(ctx, "phnosuchphoto"); err != nil {
		t.Errorf("MarkThumbnailsBuilt(unknown) = %v, want nil", err)
	}
}
