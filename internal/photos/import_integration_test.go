//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They cover Store.ApplyImportMetadata: the precedence an
// import has over the catalogue (the source owns the fields it fills), the limits of
// it (it may never erase), and its idempotence (a re-import writes nothing at all).

// fullImport is the metadata an import offers for a photo whose source knows
// everything about it.
var fullImport = photos.ImportMetadata{
	Subject:      "Masopustní průvod",
	Keywords:     "masopust,maska",
	Artist:       "Jan Novák",
	Copyright:    "© 2016 Jan Novák",
	License:      "CC BY-NC 4.0",
	Notes:        "Nalezeno v krabici po babičce.",
	Software:     "Adobe Photoshop Lightroom",
	Scan:         true,
	CameraSerial: "BX-40023199",
	ColorProfile: "Display P3",
	ImageCodec:   "jpeg",
	Projection:   "equirectangular",
	OriginalName: "IMG_4821.JPG",
}

// blankPhoto inserts a catalogued photo with no metadata at all — the state every
// row an importer meets for the first time is in.
func blankPhoto(t *testing.T, store *photos.Store, hash string) photos.Photo {
	t.Helper()
	created, err := store.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2016/02/" + hash + ".jpg",
		FileName: hash + ".jpg", FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return created
}

// TestApplyImportMetadata_fillsEveryColumn verifies each mapped column is written and
// read back. A column missing from the statement would otherwise stay empty and look
// like a source that simply had nothing to say.
func TestApplyImportMetadata_fillsEveryColumn(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	photo := blankPhoto(t, store, "imp-1")

	changed, err := store.ApplyImportMetadata(ctx, photo.UID, fullImport)
	if err != nil {
		t.Fatalf("ApplyImportMetadata: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true: every column was empty")
	}

	got, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	fields := []struct{ name, got, want string }{
		{"subject", got.Subject, fullImport.Subject},
		{"keywords", got.Keywords, fullImport.Keywords},
		{"artist", got.Artist, fullImport.Artist},
		{"copyright", got.Copyright, fullImport.Copyright},
		{"license", got.License, fullImport.License},
		{"notes", got.Notes, fullImport.Notes},
		{"software", got.Software, fullImport.Software},
		{"camera_serial", got.CameraSerial, fullImport.CameraSerial},
		{"color_profile", got.ColorProfile, fullImport.ColorProfile},
		{"image_codec", got.ImageCodec, fullImport.ImageCodec},
		{"projection", got.Projection, fullImport.Projection},
		{"original_name", got.OriginalName, fullImport.OriginalName},
	}
	for _, f := range fields {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q", f.name, f.got, f.want)
		}
	}
	if !got.Scan {
		t.Error("scan = false, want true")
	}
}

// TestApplyImportMetadata_rerunWritesNothing verifies applying the same metadata twice
// is a genuine no-op — the second run reports no change and does not even move
// updated_at, so an idle re-import cannot reshuffle a library sorted by it.
func TestApplyImportMetadata_rerunWritesNothing(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	photo := blankPhoto(t, store, "imp-2")

	if _, err := store.ApplyImportMetadata(ctx, photo.UID, fullImport); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	before, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}

	changed, err := store.ApplyImportMetadata(ctx, photo.UID, fullImport)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if changed {
		t.Error("changed = true on an unchanged re-apply, want false")
	}
	after, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at moved on a no-op re-apply: %s -> %s", before.UpdatedAt, after.UpdatedAt)
	}
}

// TestApplyImportMetadata_emptyNeverClobbers pins the rule that keeps an import safe
// to re-run over a curated library: the source owns the fields it fills, but an empty
// value it does not carry must never erase what is there. A source that has forgotten
// the artist (or never knew one) must not blank the artist the user typed.
func TestApplyImportMetadata_emptyNeverClobbers(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	photo := blankPhoto(t, store, "imp-3")

	if _, err := store.ApplyImportMetadata(ctx, photo.UID, fullImport); err != nil {
		t.Fatalf("first apply: %v", err)
	}

	// A source that now knows only the subject: everything else it has nothing for.
	changed, err := store.ApplyImportMetadata(ctx, photo.UID,
		photos.ImportMetadata{Subject: "Masopust v Ostrovačicích"})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if !changed {
		t.Error("changed = false, want true: the subject is new")
	}

	got, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Subject != "Masopust v Ostrovačicích" {
		t.Errorf("subject = %q, want the source's new value", got.Subject)
	}
	kept := []struct{ name, got, want string }{
		{"artist", got.Artist, fullImport.Artist},
		{"copyright", got.Copyright, fullImport.Copyright},
		{"license", got.License, fullImport.License},
		{"keywords", got.Keywords, fullImport.Keywords},
		{"software", got.Software, fullImport.Software},
		{"camera_serial", got.CameraSerial, fullImport.CameraSerial},
		{"color_profile", got.ColorProfile, fullImport.ColorProfile},
		{"image_codec", got.ImageCodec, fullImport.ImageCodec},
		{"projection", got.Projection, fullImport.Projection},
		{"original_name", got.OriginalName, fullImport.OriginalName},
	}
	for _, f := range kept {
		if f.got != f.want {
			t.Errorf("%s = %q, want %q kept: an empty source value erased it", f.name, f.got, f.want)
		}
	}
	if !got.Scan {
		t.Error("scan = false: the source can set the flag, never clear it")
	}
}

// TestApplyImportMetadata_notesAreGapFilledOnly verifies notes — Kukátko's own field,
// which the source has no business rewriting — is only ever written into an empty
// column. A note the user typed survives an import that carries a different one.
func TestApplyImportMetadata_notesAreGapFilledOnly(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()
	photo := blankPhoto(t, store, "imp-4")

	if _, err := store.UpdateMetadata(ctx, photo.UID,
		photos.MetadataUpdate{Notes: "moje poznámka"}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	if _, err := store.ApplyImportMetadata(ctx, photo.UID,
		photos.ImportMetadata{Notes: "poznámka ze zdroje", Artist: "Jan Novák"}); err != nil {
		t.Fatalf("ApplyImportMetadata: %v", err)
	}

	got, err := store.GetByUID(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Notes != "moje poznámka" {
		t.Errorf("notes = %q, want the user's note kept", got.Notes)
	}
	if got.Artist != "Jan Novák" {
		t.Errorf("artist = %q, want the source's: it owns that one", got.Artist)
	}
}

// TestApplyImportMetadata_unknownPhoto verifies an unknown photo is a typed error
// rather than a silent no-op, so a caller cannot mistake a missing row for a photo
// the source had nothing new for.
func TestApplyImportMetadata_unknownPhoto(t *testing.T) {
	store, _ := newStore(t)

	_, err := store.ApplyImportMetadata(t.Context(), "nope", fullImport)
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("error = %v, want ErrPhotoNotFound", err)
	}
}
