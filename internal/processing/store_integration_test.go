//go:build integration

package processing_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
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
	} {
		if at {
			t.Errorf("%s evidence present on a fresh photo", name)
		}
	}
}
