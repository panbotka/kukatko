package sidecar

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/exif"
)

// TestReadXMPFixture reads an Apple-shaped XMP sidecar end to end, through the
// same exiftool the EXIF path already depends on. It is skipped where exiftool is
// absent — that is exactly the case readXMP reports as an error rather than
// silently importing an export with no dates (see TestReadXMPWithoutTags).
func TestReadXMPFixture(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool is not installed")
	}

	meta, err := Read(t.Context(), filepath.Join("testdata", "IMG_9999.jpg.xmp"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if meta.Source != SourceXMP {
		t.Errorf("Source = %q, want %q", meta.Source, SourceXMP)
	}
	want := time.Date(2018, 7, 14, 11, 30, 0, 0, time.UTC)
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
	if meta.Title != "Na Petříně" || meta.Description != "Výhled z rozhledny" {
		t.Errorf("Title = %q, Description = %q", meta.Title, meta.Description)
	}
	if meta.Creator != "Pan Botka" {
		t.Errorf("Creator = %q", meta.Creator)
	}
	if !slices.Equal(meta.Keywords, []string{"Praha", "výlet"}) {
		t.Errorf("Keywords = %v", meta.Keywords)
	}
	if meta.Rating != 4 {
		t.Errorf("Rating = %d, want 4", meta.Rating)
	}
	if meta.Lat == nil || meta.Lng == nil || *meta.Lat < 50 || *meta.Lat > 50.1 {
		t.Errorf("GPS = %v/%v, want roughly 50.08/14.42", meta.Lat, meta.Lng)
	}
}

// TestXMPMetadata maps the tag document exiftool produces onto Metadata, without
// needing exiftool to produce it: the shapes a tag can arrive in (a bare string,
// a one-entry list, a bag of keywords) are the interesting part.
func TestXMPMetadata(t *testing.T) {
	t.Parallel()

	when := time.Date(2018, 7, 14, 11, 30, 0, 0, time.UTC)
	meta := exif.Metadata{
		TakenAt:       &when,
		TakenAtSource: exif.SourceExif,
		Lat:           new(50.08),
		Lng:           new(14.42),
		Exif: map[string]any{
			"Description": "Výhled",
			"Title":       []any{"Petřín"},
			"Subject":     []any{"Praha", "výlet"},
			"Creator":     "Pan Botka",
			"Rating":      float64(5),
		},
	}

	got := xmpMetadata(meta, "IMG_1.jpg.xmp")

	if got.TakenAt == nil || !got.TakenAt.Equal(when) {
		t.Errorf("TakenAt = %v, want %v", got.TakenAt, when)
	}
	if got.Title != "Petřín" || got.Description != "Výhled" {
		t.Errorf("Title = %q, Description = %q", got.Title, got.Description)
	}
	if !slices.Equal(got.Keywords, []string{"Praha", "výlet"}) {
		t.Errorf("Keywords = %v", got.Keywords)
	}
	if got.Rating != 5 {
		t.Errorf("Rating = %d, want 5", got.Rating)
	}
}

// TestXMPMetadataIgnoresFilenameDate: exif.Extract falls back to parsing a date
// out of the file name, and the file name here belongs to the *sidecar*. It says
// nothing about when the photo was taken and must not become its capture time.
func TestXMPMetadataIgnoresFilenameDate(t *testing.T) {
	t.Parallel()

	guessed := time.Date(2019, 8, 1, 0, 0, 0, 0, time.UTC)
	meta := exif.Metadata{
		TakenAt:       &guessed,
		TakenAtSource: exif.SourceFilename,
		Exif:          map[string]any{"Rating": float64(3)},
	}

	if got := xmpMetadata(meta, "IMG_20190801.jpg.xmp"); got.TakenAt != nil {
		t.Errorf("TakenAt = %v, want none: a date guessed from the sidecar's name is not a capture time",
			got.TakenAt)
	}
}

// TestXMPRating normalises the star scale: XMP writes -1 for "rejected", which
// Kukátko has no field for and reads as unrated.
func TestXMPRating(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		raw  any
		want int
	}{
		"number":     {float64(4), 4},
		"string":     {"2", 2},
		"rejected":   {float64(-1), 0},
		"out of top": {float64(9), 0},
		"absent":     {nil, 0},
		"nonsense":   {"stars", 0},
	}
	for name, tc := range tests {
		if got := xmpRating(tc.raw); got != tc.want {
			t.Errorf("xmpRating(%v) [%s] = %d, want %d", tc.raw, name, got, tc.want)
		}
	}
}

// TestReadXMPWithoutTags reports a sidecar it could read nothing from, rather
// than quietly importing the photo as if the sidecar had said nothing. That is
// what an XMP looks like on a box with no exiftool.
func TestReadXMPWithoutTags(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "broken.xmp")
	if err := os.WriteFile(path, []byte("this is not XMP"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := Read(t.Context(), path); err == nil {
		t.Fatal("Read(unreadable xmp) = nil error, want an error naming the unreadable sidecar")
	}
}
