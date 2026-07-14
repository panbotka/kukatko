//go:build integration

package dirimport_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/dirimport"
	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photos"
)

// takenAt is the capture time the Takeout sidecars in this file record — the one
// the exported JPEG itself no longer carries.
var takenAt = time.Unix(1465236142, 0).UTC() // 2016-06-06 18:02:22 UTC

// takeoutTree lays out a folder that mimics a Google Photos export: media files
// whose EXIF was stripped in re-encoding, the JSON sidecars that hold everything
// that was stripped, the album metadata.json that must never become an album, and
// a sidecar whose photo was not exported at all.
func takeoutTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := "Takeout/Google Photos/Photos from 2016"

	write(t, root, dir+"/lake.jpg", jpegBytes(t, 30, 220, 30))
	write(t, root, dir+"/lake.jpg.supplemental-metadata.json", []byte(`{
		"title": "lake.jpg",
		"description": "Sunset over Lipno",
		"photoTakenTime": {"timestamp": "1465236142"},
		"geoData": {"latitude": 48.6417, "longitude": 14.0453, "altitude": 726.0},
		"people": [{"name": "Jan Novák"}],
		"favorited": true
	}`))

	// A name Google's file-name cap cut short, and a sidecar with no location at
	// all: 0/0 is the placeholder for "unknown", not a point in the Gulf of Guinea.
	write(t, root, dir+"/hills.jpg", jpegBytes(t, 220, 30, 30))
	write(t, root, dir+"/hills.jp.json", []byte(`{
		"title": "hills.jpg",
		"photoTakenTime": {"timestamp": "1465236142"},
		"geoData": {"latitude": 0.0, "longitude": 0.0, "altitude": 0.0},
		"favorited": false
	}`))

	// Takeout's own album file, and a sidecar whose media was never exported.
	write(t, root, dir+"/metadata.json", []byte(`{"title":"Photos from 2016","access":"protected"}`))
	write(t, root, dir+"/vanished.jpg.json", []byte(`{"photoTakenTime":{"timestamp":"1465236142"}}`))
	return root
}

// TestImport_takeoutExport is the contract of a Google Photos import: the dates,
// the captions and the GPS that live *beside* the media land on the photos; no
// album is created from the export; the sidecar that matched nothing is reported;
// and a second run changes nothing at all.
func TestImport_takeoutExport(t *testing.T) {
	env := newEnv(t)
	root := takeoutTree(t)
	opts := dirimport.Options{Root: root, Recursive: true, UploadedBy: env.uploader}

	result, err := env.svc.Import(t.Context(), opts)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Imported != 2 || result.Counts.Failed != 0 {
		t.Fatalf("Counts = %+v, want 2 imported and no failures", result.Counts)
	}

	assertSidecarMetadata(t, env)
	assertNoAlbumsFromExport(t, env)

	if result.Sidecars.Matched != 2 || result.Sidecars.Applied != 2 {
		t.Errorf("Sidecars = %+v, want two matched and applied", result.Sidecars)
	}
	if len(result.Sidecars.Orphans) != 1 {
		t.Errorf("Orphans = %v, want the sidecar whose photo was not exported", result.Sidecars.Orphans)
	}
	if len(result.Sidecars.Unreadable) != 0 {
		t.Errorf("Unreadable = %v, want none", result.Sidecars.Unreadable)
	}

	assertSecondRunChangesNothing(t, env, opts)
}

// assertSidecarMetadata checks what the export's JSON put on the photos: the
// capture time (the field the whole feature exists for), the caption, the GPS —
// and, for the photo whose export knew no location, no GPS at all.
func assertSidecarMetadata(t *testing.T, env *testEnv) {
	t.Helper()

	lake := photoByName(t, env, "lake.jpg")
	if lake.TakenAt == nil || !lake.TakenAt.UTC().Equal(takenAt) {
		t.Errorf("lake.jpg taken_at = %v, want %v", lake.TakenAt, takenAt)
	}
	if lake.TakenAtSource != string(exif.SourceSidecar) {
		t.Errorf("lake.jpg taken_at_source = %q, want %q", lake.TakenAtSource, exif.SourceSidecar)
	}
	if lake.Description != "Sunset over Lipno" {
		t.Errorf("lake.jpg description = %q", lake.Description)
	}
	if lake.Lat == nil || *lake.Lat != 48.6417 || lake.Lng == nil || *lake.Lng != 14.0453 {
		t.Errorf("lake.jpg GPS = %v/%v, want 48.6417/14.0453", lake.Lat, lake.Lng)
	}
	// The original is filed under the month it was *taken*, which only the sidecar
	// knew: a Takeout photo must not land under the month it was imported.
	if got := lake.FilePath[:7]; got != "2016/06" {
		t.Errorf("lake.jpg stored under %q, want the capture month 2016/06", got)
	}
	// Google's "favorited" is the importing user's favourite: favourites are
	// per-user in Kukátko.
	fav, err := env.organize.IsFavorite(t.Context(), env.uploader, lake.UID)
	if err != nil {
		t.Fatalf("IsFavorite: %v", err)
	}
	if !fav {
		t.Error("lake.jpg is not a favourite of the importing user, want it favourited")
	}

	hills := photoByName(t, env, "hills.jpg")
	if hills.TakenAt == nil || !hills.TakenAt.UTC().Equal(takenAt) {
		t.Errorf("hills taken_at = %v, want %v (its truncated sidecar still found it)", hills.TakenAt, takenAt)
	}
	if hills.Lat != nil || hills.Lng != nil {
		t.Errorf("hills GPS = %v/%v, want none: an exact 0/0 fix means unknown", hills.Lat, hills.Lng)
	}
}

// assertNoAlbumsFromExport: Takeout's folder structure and its metadata.json
// album files are auto-generated junk from the phone. The photos come in; the
// albums do not.
func assertNoAlbumsFromExport(t *testing.T, env *testEnv) {
	t.Helper()

	albums, err := env.organize.ListAlbums(t.Context())
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(albums) != 0 {
		t.Errorf("the export created %d albums, want none (album membership comes from --album)", len(albums))
	}
}

// assertSecondRunChangesNothing re-runs the identical import: every file is a
// content duplicate, so nothing is created — and because the sidecars have
// already filled every gap, nothing is written either, down to updated_at.
func assertSecondRunChangesNothing(t *testing.T, env *testEnv, opts dirimport.Options) {
	t.Helper()

	before := photoByName(t, env, "lake.jpg")

	result, err := env.svc.Import(t.Context(), opts)
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if result.Counts.Imported != 0 || result.Counts.Duplicates != 2 {
		t.Errorf("second run Counts = %+v, want nothing imported and 2 duplicates", result.Counts)
	}
	if got := countPhotos(t, env.db); got != 2 {
		t.Errorf("photos after the second run = %d, want 2", got)
	}

	after := photoByName(t, env, "lake.jpg")
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("the second run rewrote lake.jpg (updated_at %v → %v), want no write at all",
			before.UpdatedAt, after.UpdatedAt)
	}
}

// TestImport_sidecarFillsAnEarlierImportsGaps covers the folder somebody already
// imported *before* its sidecars were read: the files come back as duplicates, so
// nothing is created — and the dates and captions they never got are written
// anyway. Without this, a library imported the naive way could never be repaired
// except by deleting and starting over.
func TestImport_sidecarFillsAnEarlierImportsGaps(t *testing.T) {
	env := newEnv(t)
	root := takeoutTree(t)

	// The first import ignores the sidecars, exactly as an import before this
	// feature existed would have.
	if _, err := env.svc.Import(t.Context(), dirimport.Options{
		Root: root, Recursive: true, NoSidecars: true, UploadedBy: env.uploader,
	}); err != nil {
		t.Fatalf("first Import: %v", err)
	}
	naive := photoByName(t, env, "lake.jpg")
	if naive.TakenAt != nil {
		t.Fatalf("lake.jpg has taken_at %v after an import that ignored the sidecars, want none", naive.TakenAt)
	}

	// Now import it again, reading the sidecars this time.
	result, err := env.svc.Import(t.Context(), dirimport.Options{
		Root: root, Recursive: true, UploadedBy: env.uploader,
	})
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if result.Counts.Imported != 0 || result.Counts.Duplicates != 2 {
		t.Errorf("Counts = %+v, want nothing new and 2 duplicates", result.Counts)
	}

	filled := photoByName(t, env, "lake.jpg")
	if filled.TakenAt == nil || !filled.TakenAt.UTC().Equal(takenAt) {
		t.Errorf("lake.jpg taken_at = %v, want the sidecar's %v", filled.TakenAt, takenAt)
	}
	if filled.TakenAtSource != string(exif.SourceSidecar) {
		t.Errorf("lake.jpg taken_at_source = %q, want %q", filled.TakenAtSource, exif.SourceSidecar)
	}
	if filled.Description != "Sunset over Lipno" {
		t.Errorf("lake.jpg description = %q, want the sidecar's caption", filled.Description)
	}
	if filled.Lat == nil || *filled.Lat != 48.6417 {
		t.Errorf("lake.jpg lat = %v, want the sidecar's fix", filled.Lat)
	}
	if got := countPhotos(t, env.db); got != 2 {
		t.Errorf("photos = %d, want 2: filling gaps must not create anything", got)
	}
}

// TestImport_sidecarNeverOverwritesAUserEdit: an import fills gaps, it does not
// rewrite history. A caption or a date the user has already put on a photo
// survives a re-import of the export it came from.
func TestImport_sidecarNeverOverwritesAUserEdit(t *testing.T) {
	env := newEnv(t)
	root := takeoutTree(t)

	if _, err := env.svc.Import(t.Context(), dirimport.Options{
		Root: root, Recursive: true, NoSidecars: true, UploadedBy: env.uploader,
	}); err != nil {
		t.Fatalf("first Import: %v", err)
	}

	// The user dates and captions the photo by hand, as they would in the UI.
	edited := time.Date(2016, 6, 7, 9, 0, 0, 0, time.UTC)
	lake := photoByName(t, env, "lake.jpg")
	if _, err := env.photos.UpdateMetadata(t.Context(), lake.UID, photos.MetadataUpdate{
		Description:   "My own caption",
		TakenAt:       &edited,
		TakenAtSource: "manual",
	}); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	if _, err := env.svc.Import(t.Context(), dirimport.Options{
		Root: root, Recursive: true, UploadedBy: env.uploader,
	}); err != nil {
		t.Fatalf("second Import: %v", err)
	}

	got := photoByName(t, env, "lake.jpg")
	if got.Description != "My own caption" {
		t.Errorf("description = %q, want the user's own caption kept", got.Description)
	}
	if got.TakenAt == nil || !got.TakenAt.UTC().Equal(edited) {
		t.Errorf("taken_at = %v, want the user's own date %v kept", got.TakenAt, edited)
	}
	if got.TakenAtSource != "manual" {
		t.Errorf("taken_at_source = %q, want it left at manual", got.TakenAtSource)
	}
	// The gaps the user left are still filled: the GPS fix nobody typed in.
	if got.Lat == nil || *got.Lat != 48.6417 {
		t.Errorf("lat = %v, want the sidecar's fix filling the gap", got.Lat)
	}
}
