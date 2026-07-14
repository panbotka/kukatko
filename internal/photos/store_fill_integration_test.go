//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// sidecarTakenAt is the capture time an import offers from a Google Takeout JSON.
var sidecarTakenAt = time.Date(2016, 6, 6, 18, 2, 22, 0, time.UTC)

// TestFillMissingMetadata_fillsGapsAndIsIdempotent is the contract an import
// depends on: a photo imported before its sidecars were read gets the date,
// caption and GPS it never had — and running the import a second time writes
// nothing at all, not even updated_at.
func TestFillMissingMetadata_fillsGapsAndIsIdempotent(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo := mustCreate(t, store, photos.Photo{
		FileHash: "fill-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg",
		TakenAtSource: "unknown",
	})

	fill := photos.MetadataFill{
		TakenAt:       &sidecarTakenAt,
		TakenAtSource: "sidecar",
		Lat:           ptrFloat(48.6417),
		Lng:           ptrFloat(14.0453),
		Altitude:      ptrFloat(726),
		Title:         "Lipno",
		Description:   "Sunset over Lipno",
	}
	changed, err := store.FillMissingMetadata(ctx, photo.UID, fill)
	if err != nil {
		t.Fatalf("FillMissingMetadata: %v", err)
	}
	if !changed {
		t.Fatal("FillMissingMetadata reported no change, want the gaps filled")
	}

	filled, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if filled.TakenAt == nil || !filled.TakenAt.UTC().Equal(sidecarTakenAt) {
		t.Errorf("taken_at = %v, want %v", filled.TakenAt, sidecarTakenAt)
	}
	if filled.TakenAtSource != "sidecar" {
		t.Errorf("taken_at_source = %q, want sidecar", filled.TakenAtSource)
	}
	if filled.Lat == nil || *filled.Lat != 48.6417 || filled.Lng == nil || *filled.Lng != 14.0453 {
		t.Errorf("GPS = %v/%v, want the sidecar's fix", filled.Lat, filled.Lng)
	}
	if filled.Title != "Lipno" || filled.Description != "Sunset over Lipno" {
		t.Errorf("title = %q, description = %q", filled.Title, filled.Description)
	}

	// The second run of the same import must be a genuine no-op.
	changed, err = store.FillMissingMetadata(ctx, photo.UID, fill)
	if err != nil {
		t.Fatalf("second FillMissingMetadata: %v", err)
	}
	if changed {
		t.Error("the second fill reported a change, want none")
	}
	again, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if !again.UpdatedAt.Equal(filled.UpdatedAt) {
		t.Errorf("updated_at moved on a no-op fill: %v → %v", filled.UpdatedAt, again.UpdatedAt)
	}
}

// TestFillMissingMetadata_neverOverwrites: an import fills gaps, it does not
// rewrite history. Everything the photo already carries — its own EXIF fix, a
// date and a caption the user typed — survives the sidecar untouched.
func TestFillMissingMetadata_neverOverwrites(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	own := time.Date(2016, 6, 6, 12, 0, 0, 0, time.UTC)
	photo := mustCreate(t, store, photos.Photo{
		FileHash: "fill-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		TakenAt: &own, TakenAtSource: "manual", Title: "Mine", Description: "My own caption",
		Lat: ptrFloat(50.1), Lng: ptrFloat(14.4),
	})

	changed, err := store.FillMissingMetadata(ctx, photo.UID, photos.MetadataFill{
		TakenAt:       &sidecarTakenAt,
		TakenAtSource: "sidecar",
		Lat:           ptrFloat(48.6417),
		Lng:           ptrFloat(14.0453),
		Title:         "Theirs",
		Description:   "The export's caption",
		Altitude:      ptrFloat(726),
	})
	if err != nil {
		t.Fatalf("FillMissingMetadata: %v", err)
	}
	// Only the altitude was ever missing, so only the altitude is written.
	if !changed {
		t.Fatal("FillMissingMetadata reported no change, want the missing altitude filled")
	}

	got, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.TakenAt == nil || !got.TakenAt.UTC().Equal(own) || got.TakenAtSource != "manual" {
		t.Errorf("taken_at = %v (%s), want the user's own date kept", got.TakenAt, got.TakenAtSource)
	}
	if got.Title != "Mine" || got.Description != "My own caption" {
		t.Errorf("title = %q, description = %q, want the user's own text kept", got.Title, got.Description)
	}
	if got.Lat == nil || *got.Lat != 50.1 {
		t.Errorf("lat = %v, want the photo's own fix kept", got.Lat)
	}
	if got.Altitude == nil || *got.Altitude != 726 {
		t.Errorf("altitude = %v, want the one gap filled", got.Altitude)
	}
}

// TestFillMissingMetadata_overrulesAFilenameGuess: a capture time parsed out of a
// file name is a guess, and an export that recorded the real one outranks it.
func TestFillMissingMetadata_overrulesAFilenameGuess(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	guessed := time.Date(2016, 6, 6, 0, 0, 0, 0, time.UTC)
	photo := mustCreate(t, store, photos.Photo{
		FileHash: "fill-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg",
		TakenAt: &guessed, TakenAtSource: "filename",
	})

	if _, err := store.FillMissingMetadata(ctx, photo.UID, photos.MetadataFill{
		TakenAt: &sidecarTakenAt, TakenAtSource: "sidecar",
	}); err != nil {
		t.Fatalf("FillMissingMetadata: %v", err)
	}

	got, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.TakenAt == nil || !got.TakenAt.UTC().Equal(sidecarTakenAt) {
		t.Errorf("taken_at = %v, want the sidecar's %v to overrule the filename guess", got.TakenAt, sidecarTakenAt)
	}
	if got.TakenAtSource != "sidecar" {
		t.Errorf("taken_at_source = %q, want sidecar", got.TakenAtSource)
	}
}

// TestFillMissingMetadata_halfAFixIsNotALocation: a sidecar that offers only one
// coordinate offers no location, and neither half is written on its own.
func TestFillMissingMetadata_halfAFixIsNotALocation(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo := mustCreate(t, store, photos.Photo{
		FileHash: "fill-4", FilePath: "p/4.jpg", FileName: "4.jpg", FileMime: "image/jpeg",
		TakenAtSource: "unknown",
	})

	changed, err := store.FillMissingMetadata(ctx, photo.UID, photos.MetadataFill{Lat: ptrFloat(48.6)})
	if err != nil {
		t.Fatalf("FillMissingMetadata: %v", err)
	}
	if changed {
		t.Error("a lone latitude was written, want nothing")
	}

	got, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Lat != nil || got.Lng != nil {
		t.Errorf("GPS = %v/%v, want none", got.Lat, got.Lng)
	}
}

// ptrFloat returns a pointer to f.
func ptrFloat(f float64) *float64 { return &f }
