package ppimport

import (
	"testing"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photoprism"
	"github.com/panbotka/kukatko/internal/storage"
)

// TestBuildPhotoStoresRawGeometry pins the invariant the face overlay depends on:
// file_width/file_height describe the bytes on disk and file_orientation is the
// tag still to be applied to them. PhotoPrism reports its dimensions with the tag
// ALREADY applied, so taking them verbatim used to store a self-contradicting
// pair for a quarter-turned photo.
//
// The fixture is the production photo from the bug report: a 5472 × 3648 original
// with orientation 8, which PhotoPrism reports as its displayed 3648 × 5472.
func TestBuildPhotoStoresRawGeometry(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		orientation int
		ppWidth     int
		ppHeight    int
		wantW       int
		wantH       int
	}{
		{name: "upright", orientation: 1, ppWidth: 5472, ppHeight: 3648, wantW: 5472, wantH: 3648},
		{name: "180 degrees", orientation: 3, ppWidth: 5472, ppHeight: 3648, wantW: 5472, wantH: 3648},
		{name: "90 CW", orientation: 6, ppWidth: 3648, ppHeight: 5472, wantW: 5472, wantH: 3648},
		{name: "270 CW", orientation: 8, ppWidth: 3648, ppHeight: 5472, wantW: 5472, wantH: 3648},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := exif.Metadata{Width: 5472, Height: 3648, Orientation: tc.orientation}
			got := buildPhoto(
				photoprism.Photo{UID: "pp1", Width: tc.ppWidth, Height: tc.ppHeight},
				photoprism.File{Hash: "sha1"},
				storage.StoredFile{Hash: "sha256", RelPath: "2024/01/x.jpg"},
				meta,
			)
			if got.FileWidth != tc.wantW || got.FileHeight != tc.wantH {
				t.Errorf("dimensions = %d×%d, want %d×%d",
					got.FileWidth, got.FileHeight, tc.wantW, tc.wantH)
			}
			if got.FileOrientation != tc.orientation {
				t.Errorf("orientation = %d, want %d", got.FileOrientation, tc.orientation)
			}
		})
	}
}

// TestRawGeometryFallsBackToDeorientedPhotoprism covers the branch where the
// original's own EXIF carries no geometry: PhotoPrism's pair is then the only
// source, and it has to have its quarter turn undone before it is stored.
func TestRawGeometryFallsBackToDeorientedPhotoprism(t *testing.T) {
	t.Parallel()
	pp := photoprism.Photo{Width: 3648, Height: 5472}
	if w, h := rawGeometry(pp, exif.Metadata{}, 8); w != 5472 || h != 3648 {
		t.Errorf("quarter turn: got %d×%d, want 5472×3648", w, h)
	}
	if w, h := rawGeometry(pp, exif.Metadata{}, 1); w != 3648 || h != 5472 {
		t.Errorf("upright: got %d×%d, want 3648×5472", w, h)
	}
}

// TestRawGeometryPrefersTheFile keeps PhotoPrism from overriding the geometry of
// the file the orientation was read from — the two must describe the same bytes.
func TestRawGeometryPrefersTheFile(t *testing.T) {
	t.Parallel()
	pp := photoprism.Photo{Width: 3648, Height: 5472}
	meta := exif.Metadata{Width: 5472, Height: 3648, Orientation: 8}
	if w, h := rawGeometry(pp, meta, 8); w != 5472 || h != 3648 {
		t.Errorf("got %d×%d, want the file's 5472×3648", w, h)
	}
}
