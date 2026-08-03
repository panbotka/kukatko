//go:build integration

package photos_test

import (
	"encoding/json"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// transposedPhoto builds a photo whose columns hold the DISPLAYED frame of a
// storedWidth × storedHeight original — the pair a PhotoPrism-derived import
// wrote — with the file's own EXIF geometry in the exif document under the given
// tag names. Passing no tags leaves the document without any geometry.
func transposedPhoto(hash string, storedWidth, storedHeight, orientation int, tags map[string]any) photos.Photo {
	p := samplePhoto(hash)
	p.FileOrientation = orientation
	p.FileWidth, p.FileHeight = storedWidth, storedHeight
	if orientation >= 5 && orientation <= 8 {
		p.FileWidth, p.FileHeight = storedHeight, storedWidth
	}
	if tags != nil {
		raw, err := json.Marshal(tags)
		if err != nil {
			panic(err)
		}
		p.Exif = raw
	}
	return p
}

// exifTags is the geometry an exiftool document carries for a 5472 × 3648
// original: the file's own, pre-rotation dimensions.
func exifTags(width, height int) map[string]any {
	return map[string]any{"ImageWidth": width, "ImageHeight": height, "Make": "Canon"}
}

// TestListDimensionMismatchesFindsQuarterTurns verifies the scan reports exactly
// the quarter-turned photos whose columns are the file's own dimensions
// transposed, across every orientation the library holds.
func TestListDimensionMismatchesFindsQuarterTurns(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	// Orientations 1 and 3 do not swap, so their columns already agree with the
	// file and nothing is reported; 6 and 8 do.
	for hash, orientation := range map[string]int{"h1": 1, "h3": 3, "h6": 6, "h8": 8} {
		if _, err := store.Create(ctx,
			transposedPhoto(hash, 5472, 3648, orientation, exifTags(5472, 3648))); err != nil {
			t.Fatalf("Create(%s): %v", hash, err)
		}
	}

	got, err := store.ListDimensionMismatches(ctx)
	if err != nil {
		t.Fatalf("ListDimensionMismatches: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d mismatches, want 2 (orientations 6 and 8): %+v", len(got), got)
	}
	for _, m := range got {
		if m.Orientation != 6 && m.Orientation != 8 {
			t.Errorf("reported orientation %d, want only the quarter turns", m.Orientation)
		}
		if m.StoredWidth != 3648 || m.StoredHeight != 5472 {
			t.Errorf("stored = %d×%d, want the transposed 3648×5472", m.StoredWidth, m.StoredHeight)
		}
		if m.RawWidth != 5472 || m.RawHeight != 3648 {
			t.Errorf("raw = %d×%d, want the file's 5472×3648", m.RawWidth, m.RawHeight)
		}
	}
}

// TestListDimensionMismatchesNeedsEvidence verifies the scan never guesses: a
// quarter-turned photo whose EXIF document says nothing about its geometry, or
// whose columns already agree with it, is left out. That is what keeps the repair
// from swapping a correctly-imported photo (an own upload stores the raw pair) on
// a provenance hunch.
func TestListDimensionMismatchesNeedsEvidence(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	cases := map[string]photos.Photo{
		// No geometry in the document at all.
		"no-exif": transposedPhoto("hA", 5472, 3648, 8, nil),
		// A document that carries other tags but no dimensions.
		"no-dims": transposedPhoto("hB", 5472, 3648, 8, map[string]any{"Make": "Canon"}),
		// A non-numeric value must degrade to "unknown", not abort the query.
		"text-dims": transposedPhoto("hC", 5472, 3648, 8,
			map[string]any{"ImageWidth": "5472 pixels", "ImageHeight": "3648 pixels"}),
		// A square frame: the swap is an identity, so there is nothing to report.
		"square": transposedPhoto("hD", 4000, 4000, 8, exifTags(4000, 4000)),
	}
	for name, p := range cases {
		if _, err := store.Create(ctx, p); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	// A photo already holding the file's own pair — the state after a repair, and
	// the state an own upload is created in.
	correct := samplePhoto("hE")
	correct.FileOrientation = 8
	correct.FileWidth, correct.FileHeight = 5472, 3648
	raw, err := json.Marshal(exifTags(5472, 3648))
	if err != nil {
		t.Fatalf("marshal exif: %v", err)
	}
	correct.Exif = raw
	if _, err := store.Create(ctx, correct); err != nil {
		t.Fatalf("Create(correct): %v", err)
	}

	got, err := store.ListDimensionMismatches(ctx)
	if err != nil {
		t.Fatalf("ListDimensionMismatches: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %+v, want no mismatches without evidence", got)
	}
}

// TestRepairDimensionsIsIdempotent verifies the repair writes the file's own pair
// once and then reports "unchanged", so a second run cannot swap a corrected row
// back — the property that makes the repair safe to re-run and to interrupt.
func TestRepairDimensionsIsIdempotent(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, transposedPhoto("h8", 5472, 3648, 8, exifTags(5472, 3648)))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	mismatches, err := store.ListDimensionMismatches(ctx)
	if err != nil || len(mismatches) != 1 {
		t.Fatalf("ListDimensionMismatches = (%+v, %v), want one row", mismatches, err)
	}

	changed, err := store.RepairDimensions(ctx, mismatches[0])
	if err != nil || !changed {
		t.Fatalf("RepairDimensions = (%v, %v), want (true, nil)", changed, err)
	}
	fixed, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if fixed.FileWidth != 5472 || fixed.FileHeight != 3648 {
		t.Errorf("after repair = %d×%d, want 5472×3648", fixed.FileWidth, fixed.FileHeight)
	}
	if fixed.FileOrientation != 8 {
		t.Errorf("orientation = %d, want the raw tag 8 untouched", fixed.FileOrientation)
	}

	// Re-running the same (now stale) mismatch must not swap it back.
	changed, err = store.RepairDimensions(ctx, mismatches[0])
	if err != nil {
		t.Fatalf("second RepairDimensions: %v", err)
	}
	if changed {
		t.Error("second RepairDimensions reported a change, want a no-op")
	}
	again, err := store.ListDimensionMismatches(ctx)
	if err != nil || len(again) != 0 {
		t.Errorf("ListDimensionMismatches after repair = (%+v, %v), want empty", again, err)
	}
}
