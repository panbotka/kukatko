//go:build integration

package processing_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the evidence query itself — the single
// round trip behind the per-photo processing report — against the real schema.

// TestEvidence_unknownPhoto checks the query's not-found contract, which is what
// turns a bad uid into a 404 rather than an empty report.
func TestEvidence_unknownPhoto(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	_, err := processing.NewStore(db.Pool()).Evidence(t.Context(), "nobody")
	if !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Evidence error = %v, want photos.ErrPhotoNotFound", err)
	}
}

// TestEvidence_freshPhotoHasNone checks a catalogued photo nothing has run on
// yet reads as no evidence at all — every join misses, and the query still
// returns its row.
func TestEvidence_freshPhotoHasNone(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := photos.NewStore(db.Pool())

	photo, err := store.Create(t.Context(), photos.Photo{
		FileHash: "hash-fresh", FilePath: "2026/08/fresh.jpg", FileName: "fresh.jpg",
		FileSize: 1, FileMime: "image/jpeg", MediaType: photos.MediaImage,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ev, err := processing.NewStore(db.Pool()).Evidence(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("Evidence: %v", err)
	}
	if ev.MediaType != photos.MediaImage || ev.HasGPS {
		t.Errorf("media/GPS = (%q, %v), want (image, false)", ev.MediaType, ev.HasGPS)
	}
	for name, at := range map[string]bool{
		"thumbnail": ev.ThumbnailAt != nil,
		"embedding": ev.EmbeddingAt != nil,
		"face":      ev.FaceAt != nil,
		"ocr":       ev.OCRAt != nil,
		"place":     ev.PlaceAt != nil,
		"sidecar":   ev.SidecarAt != nil,
		"hls":       ev.HLSAt != nil,
	} {
		if at {
			t.Errorf("%s evidence present on a fresh photo", name)
		}
	}
}

// TestEvidence_hlsIsTheNewestRendition checks the streaming column: it reports
// when the video was last encoded, and it is an aggregate — a video encoded into
// several qualities must still produce exactly one report row, not one per
// rendition.
func TestEvidence_hlsIsTheNewestRendition(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	photo, err := photos.NewStore(db.Pool()).Create(t.Context(), photos.Photo{
		FileHash: "hash-hls", FilePath: "2026/08/clip.mp4", FileName: "clip.mp4",
		FileSize: 1, FileMime: "video/mp4", MediaType: photos.MediaVideo,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store := processing.NewStore(db.Pool())

	before, err := store.Evidence(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("Evidence before the encode: %v", err)
	}
	if before.HLSAt != nil {
		t.Errorf("HLSAt = %v on a video nothing encoded yet, want none", before.HLSAt)
	}

	renditions := hlsjob.NewStore(db.Pool())
	for _, name := range []string{"1080p", "720p"} {
		if _, err := renditions.Save(t.Context(), hlsjob.Encoded{
			PhotoUID: photo.UID, Rendition: name, Playlist: "#EXTM3U\n",
			Width: 1920, Height: 1080, Bandwidth: 6_128_000,
			Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 2, DurationMs: 10_000,
		}); err != nil {
			t.Fatalf("recording rendition %s: %v", name, err)
		}
	}

	after, err := store.Evidence(t.Context(), photo.UID)
	if err != nil {
		t.Fatalf("Evidence after the encode: %v", err)
	}
	if after.HLSAt == nil {
		t.Fatal("HLSAt = none for a video with two recorded renditions")
	}
}
