//go:build integration

package hlsjob_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
)

// storeHarness prepares a clean database with one catalogued video and the
// rendition store over it. Unlike the encode tests it needs no ffmpeg: nothing
// here is encoded, only recorded.
func storeHarness(t *testing.T) (*hlsjob.Store, photos.Photo) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	photo, err := photos.NewStore(db.Pool()).Create(t.Context(), photos.Photo{
		FileHash:        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		FilePath:        "2026/01/clip.mp4",
		FileName:        "clip.mp4",
		FileSize:        1024,
		FileMime:        "video/mp4",
		FileOrientation: 1,
		MediaType:       photos.MediaVideo,
	})
	if err != nil {
		t.Fatalf("creating the catalogued video: %v", err)
	}
	return hlsjob.NewStore(db.Pool()), photo
}

// row returns a valid rendition row for the given photo and rendition name.
func row(photoUID, name string, width, height int) hlsjob.Encoded {
	return hlsjob.Encoded{
		PhotoUID:     photoUID,
		Rendition:    name,
		Playlist:     "#EXTM3U\n",
		Width:        width,
		Height:       height,
		Bandwidth:    6_128_000,
		Codecs:       "avc1.640028,mp4a.40.2",
		SegmentCount: 3,
		DurationMs:   18_000,
	}
}

// TestStore_saveIsAnUpsert verifies the primary key does its job: a second write
// of the same rendition replaces the first rather than adding a row, and moves
// encoded_at forward so "when was this last encoded" stays answerable.
func TestStore_saveIsAnUpsert(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	first, err := store.Save(ctx, row(photo.UID, "1080p", 1920, 1080))
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if first.EncodedAt.IsZero() {
		t.Error("Save did not stamp encoded_at")
	}

	replacement := row(photo.UID, "1080p", 1280, 720)
	replacement.SegmentCount = 5
	second, err := store.Save(ctx, replacement)
	if err != nil {
		t.Fatalf("Save (upsert): %v", err)
	}
	if second.Width != 1280 || second.SegmentCount != 5 {
		t.Errorf("upsert stored %+v, want the replacement's values", second)
	}
	if second.EncodedAt.Before(first.EncodedAt) {
		t.Errorf("encoded_at went backwards: %v then %v", first.EncodedAt, second.EncodedAt)
	}
	rows, err := store.ListForPhoto(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListForPhoto: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("photo has %d rows after an upsert, want 1", len(rows))
	}
}

// TestStore_listIsWidestFirst verifies the listing order a master playlist wants:
// a player takes the first variant it can play, so the best one must come first.
func TestStore_listIsWidestFirst(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	if _, err := store.Save(ctx, row(photo.UID, "720p", 1280, 720)); err != nil {
		t.Fatalf("Save 720p: %v", err)
	}
	if _, err := store.Save(ctx, row(photo.UID, "1080p", 1920, 1080)); err != nil {
		t.Fatalf("Save 1080p: %v", err)
	}
	rows, err := store.ListForPhoto(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListForPhoto: %v", err)
	}
	if len(rows) != 2 || rows[0].Rendition != "1080p" || rows[1].Rendition != "720p" {
		t.Errorf("ListForPhoto = %+v, want 1080p before 720p", rows)
	}
}

// TestStore_notFound verifies the sentinel for a video that has not been encoded
// into the requested rendition, and that a photo with none lists empty rather
// than failing — "not encoded yet" is a state, not an error.
func TestStore_notFound(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	if _, err := store.Get(ctx, photo.UID, "1080p"); !errors.Is(err, hlsjob.ErrRenditionNotFound) {
		t.Errorf("Get = %v, want ErrRenditionNotFound", err)
	}
	rows, err := store.ListForPhoto(ctx, photo.UID)
	if err != nil || len(rows) != 0 {
		t.Errorf("ListForPhoto = %v, %v, want an empty list and no error", rows, err)
	}
}

// TestStore_rejectsUnusableRows verifies the table refuses what no master
// playlist may advertise, so a bug upstream fails loudly here instead of
// producing a variant a player chokes on.
func TestStore_rejectsUnusableRows(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	tests := map[string]func(*hlsjob.Encoded){
		"no playlist":   func(e *hlsjob.Encoded) { e.Playlist = "" },
		"no picture":    func(e *hlsjob.Encoded) { e.Width = 0 },
		"no segments":   func(e *hlsjob.Encoded) { e.SegmentCount = 0 },
		"no duration":   func(e *hlsjob.Encoded) { e.DurationMs = 0 },
		"no codecs":     func(e *hlsjob.Encoded) { e.Codecs = "" },
		"bad rendition": func(e *hlsjob.Encoded) { e.Rendition = "../etc" },
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			enc := row(photo.UID, "1080p", 1920, 1080)
			corrupt(&enc)
			if _, err := store.Save(ctx, enc); err == nil {
				t.Errorf("Save accepted a row with %s", name)
			}
		})
	}
}

// TestStore_cascadesWithThePhoto verifies the foreign key: purging a photo takes
// its renditions with it, so no row survives describing objects that were deleted
// along with the photo.
func TestStore_cascadesWithThePhoto(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	if _, err := store.Save(ctx, row(photo.UID, "1080p", 1920, 1080)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	db := dbtest.New(t)
	if _, err := db.Pool().Exec(ctx, "DELETE FROM photos WHERE uid = $1", photo.UID); err != nil {
		t.Fatalf("deleting the photo: %v", err)
	}
	rows, err := store.ListForPhoto(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListForPhoto: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("photo deletion left %d renditions behind", len(rows))
	}
}

// TestStore_hasAndHasAny verifies the two existence checks the serving routes
// run on the request path: whether one rendition exists (the segment route, on
// every fragment a player fetches) and whether any does at all (the flag on the
// photo payload).
func TestStore_hasAndHasAny(t *testing.T) {
	store, photo := storeHarness(t)
	ctx := t.Context()

	any, err := store.HasAny(ctx, photo.UID)
	if err != nil {
		t.Fatalf("HasAny before the encode: %v", err)
	}
	if any {
		t.Error("HasAny = true before anything was encoded")
	}
	if _, err := store.Save(ctx, row(photo.UID, "1080p", 1920, 1080)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tests := []struct {
		rendition string
		want      bool
	}{
		{rendition: "1080p", want: true},
		{rendition: "720p", want: false},
	}
	for _, tt := range tests {
		got, hasErr := store.Has(ctx, photo.UID, tt.rendition)
		if hasErr != nil {
			t.Fatalf("Has(%q): %v", tt.rendition, hasErr)
		}
		if got != tt.want {
			t.Errorf("Has(%q) = %v, want %v", tt.rendition, got, tt.want)
		}
	}

	any, err = store.HasAny(ctx, photo.UID)
	if err != nil {
		t.Fatalf("HasAny after the encode: %v", err)
	}
	if !any {
		t.Error("HasAny = false for a video with a recorded rendition")
	}
	if unknown, unknownErr := store.HasAny(ctx, "ptnosuchphoto0000000000000000000"); unknownErr != nil ||
		unknown {
		t.Errorf("HasAny(unknown photo) = %v, %v, want false, nil", unknown, unknownErr)
	}
}
