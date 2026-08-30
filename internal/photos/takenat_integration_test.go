//go:build integration

package photos_test

import (
	"context"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// scannedIn is the wrong date the fixture photo carries: the day a scan went
// through the scanner rather than the day the picture was taken.
var scannedIn = time.Date(2011, 3, 8, 10, 15, 0, 0, time.UTC)

// setTakenAt writes a capture time (or clears it with nil) through the ordinary
// metadata update path, which is where the preserve-the-cleared-date rule lives.
// It returns the refreshed photo.
func setTakenAt(t *testing.T, store *photos.Store, ctx context.Context, uid string, at *time.Time) photos.Photo {
	t.Helper()
	source := photos.TakenAtSourceUnknown
	if at != nil {
		source = photos.TakenAtSourceManual
	}
	updated, err := store.UpdateMetadata(ctx, uid, photos.MetadataUpdate{
		TakenAt:          at,
		TakenAtSource:    source,
		TakenAtPrecision: photos.TakenAtPrecisionDay,
		TakenAtEstimated: true,
		TakenAtNote:      "podle babičky svatba",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata(taken_at=%v): %v", at, err)
	}
	return updated
}

// mustBeforeUnknown fails unless the photo's preserved date is want (nil for
// "nothing was ever put away").
func mustBeforeUnknown(t *testing.T, got photos.Photo, want *time.Time, when string) {
	t.Helper()
	switch {
	case want == nil && got.TakenAtBeforeUnknown != nil:
		t.Fatalf("%s: taken_at_before_unknown = %s, want none", when, got.TakenAtBeforeUnknown)
	case want != nil && got.TakenAtBeforeUnknown == nil:
		t.Fatalf("%s: taken_at_before_unknown = none, want %s", when, want)
	case want != nil && !got.TakenAtBeforeUnknown.Equal(*want):
		t.Fatalf("%s: taken_at_before_unknown = %s, want %s", when, got.TakenAtBeforeUnknown, want)
	}
}

// TestTakenAtCleared_preservesTheOutgoingDate walks the four transitions of the
// preserved capture date through the store: a dated photo cleared, a clear that
// follows a clear, a real date stated afterwards, and a photo that never had a
// date being cleared.
func TestTakenAtCleared_preservesTheOutgoingDate(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("clr1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	mustBeforeUnknown(t, created, nil, "a fresh photo")

	dated := setTakenAt(t, store, ctx, created.UID, &scannedIn)
	mustBeforeUnknown(t, dated, nil, "a photo that carries a date")

	// value → cleared: the outgoing date is put away, not lost.
	cleared := setTakenAt(t, store, ctx, created.UID, nil)
	if cleared.TakenAt != nil {
		t.Fatalf("clearing left taken_at = %s", cleared.TakenAt)
	}
	if cleared.TakenAtSource != photos.TakenAtSourceUnknown {
		t.Errorf("taken_at_source = %q, want %q", cleared.TakenAtSource, photos.TakenAtSourceUnknown)
	}
	mustBeforeUnknown(t, cleared, &scannedIn, "after clearing a dated photo")

	// The orthogonal pair survives untouched: "date unknown, but grandma says it
	// was a wedding" is a legitimate state.
	if !cleared.TakenAtEstimated || cleared.TakenAtNote != "podle babičky svatba" {
		t.Errorf("clearing disturbed the estimate pair: estimated=%v note=%q",
			cleared.TakenAtEstimated, cleared.TakenAtNote)
	}

	// cleared → cleared: a second clear must not overwrite what is preserved.
	again := setTakenAt(t, store, ctx, created.UID, nil)
	mustBeforeUnknown(t, again, &scannedIn, "after clearing an already-cleared photo")

	// cleared → a real date: the set-aside value is no longer the date we put
	// away, and goes in the same statement.
	actual := time.Date(1974, 6, 14, 0, 0, 0, 0, time.UTC)
	redated := setTakenAt(t, store, ctx, created.UID, &actual)
	if redated.TakenAt == nil || !redated.TakenAt.Equal(actual) {
		t.Fatalf("taken_at = %v, want %s", redated.TakenAt, actual)
	}
	mustBeforeUnknown(t, redated, nil, "after stating a real date")
}

// TestTakenAtCleared_neverDatedStaysEmpty verifies that clearing the date of a
// photo that never had one preserves nothing: there is no wrong date to keep,
// and writing NULL over the column would be indistinguishable from losing one.
func TestTakenAtCleared_neverDatedStaysEmpty(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	undated := samplePhoto("clr2")
	undated.TakenAt = nil
	undated.TakenAtSource = photos.TakenAtSourceUnknown
	created, err := store.Create(ctx, undated)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	cleared := setTakenAt(t, store, ctx, created.UID, nil)
	if cleared.TakenAt != nil {
		t.Fatalf("taken_at = %s, want none", cleared.TakenAt)
	}
	mustBeforeUnknown(t, cleared, nil, "after clearing a never-dated photo")
}

// TestTakenAtCleared_survivesAnUnrelatedEdit verifies that an edit which does not
// touch the date at all — a title change on a photo whose date was declared
// unknown — leaves the preserved date exactly where it was. The metadata update
// writes the whole row, so this is the case a naive assignment would wipe.
func TestTakenAtCleared_survivesAnUnrelatedEdit(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("clr3"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	setTakenAt(t, store, ctx, created.UID, &scannedIn)
	cleared := setTakenAt(t, store, ctx, created.UID, nil)
	mustBeforeUnknown(t, cleared, &scannedIn, "after clearing")

	retitled, err := store.UpdateMetadata(ctx, created.UID, photos.MetadataUpdate{
		Title:            "Svatba",
		TakenAt:          cleared.TakenAt,
		TakenAtSource:    cleared.TakenAtSource,
		TakenAtPrecision: cleared.TakenAtPrecision,
	})
	if err != nil {
		t.Fatalf("UpdateMetadata(title): %v", err)
	}
	mustBeforeUnknown(t, retitled, &scannedIn, "after an edit that leaves the date alone")

	// And it is on the row, not only in the RETURNING of the statement that wrote it.
	reread, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	mustBeforeUnknown(t, reread, &scannedIn, "on re-read")
}
