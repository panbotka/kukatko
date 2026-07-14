package sidecar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReadGoogleFixture reads a Takeout sidecar of the documented shape and
// checks every field the import carries over, above all the capture time: it is
// the field the exported JPEG no longer has.
func TestReadGoogleFixture(t *testing.T) {
	t.Parallel()

	meta, err := Read(t.Context(), filepath.Join("testdata", "IMG_1234.jpg.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if meta.Source != SourceGoogle {
		t.Errorf("Source = %q, want %q", meta.Source, SourceGoogle)
	}
	want := time.Unix(1465236142, 0).UTC()
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
	if meta.Description != "Sunset over Lipno" {
		t.Errorf("Description = %q", meta.Description)
	}
	if meta.Lat == nil || *meta.Lat != 48.6417 || meta.Lng == nil || *meta.Lng != 14.0453 {
		t.Errorf("GPS = %v/%v, want 48.6417/14.0453", meta.Lat, meta.Lng)
	}
	if meta.Altitude == nil || *meta.Altitude != 726 {
		t.Errorf("Altitude = %v, want 726", meta.Altitude)
	}
	if !meta.Favorite {
		t.Error("Favorite = false, want true")
	}
	if len(meta.People) != 2 || meta.People[0] != "Jan Novák" {
		t.Errorf("People = %v", meta.People)
	}
	// The export's title is the file name, not a caption: it must not become one.
	if meta.Title != "" {
		t.Errorf("Title = %q, want empty (Takeout's title is the file name)", meta.Title)
	}
}

// TestReadGoogleZeroGeoAndNoDescription covers the export Google writes for a
// photo it knows nothing about: an exact 0/0 fix, which means "no location" and
// not a point in the Gulf of Guinea, and no description at all.
func TestReadGoogleZeroGeoAndNoDescription(t *testing.T) {
	t.Parallel()

	meta, err := Read(t.Context(), filepath.Join("testdata", "IMG_5678.jpg.supplemental-metadata.json"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if meta.Lat != nil || meta.Lng != nil || meta.Altitude != nil {
		t.Errorf("GPS = %v/%v/%v, want all absent for an exact 0/0 fix", meta.Lat, meta.Lng, meta.Altitude)
	}
	if meta.Description != "" {
		t.Errorf("Description = %q, want empty", meta.Description)
	}
	if meta.Favorite {
		t.Error("Favorite = true, want false")
	}
	want := time.Unix(1262304000, 0).UTC()
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
	if meta.Empty() {
		t.Error("Empty() = true, want false: the sidecar still carries the capture time")
	}
}

// TestReadGoogleVariants covers the JSON shapes seen across Takeout versions: a
// numeric (unquoted) timestamp, a photo whose location was corrected in Google
// Photos (geoData wins over the camera's geoDataExif), a sidecar with only a
// creationTime, and one that carries nothing at all.
func TestReadGoogleVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		takenAt *time.Time
		lat     *float64
	}{
		{
			name:    "numeric timestamp",
			body:    `{"photoTakenTime":{"timestamp":1465236142}}`,
			takenAt: new(time.Unix(1465236142, 0).UTC()),
		},
		{
			name: "corrected location wins over the camera's",
			body: `{"photoTakenTime":{"timestamp":"1465236142"},
				"geoData":{"latitude":50.1,"longitude":14.4},
				"geoDataExif":{"latitude":48.6,"longitude":14.0}}`,
			takenAt: new(time.Unix(1465236142, 0).UTC()),
			lat:     new(50.1),
		},
		{
			name: "camera location used when the corrected one is unknown",
			body: `{"geoData":{"latitude":0,"longitude":0},
				"geoDataExif":{"latitude":48.6,"longitude":14.0}}`,
			lat: new(48.6),
		},
		{
			name:    "creationTime is the last resort",
			body:    `{"creationTime":{"timestamp":"1601234567"}}`,
			takenAt: new(time.Unix(1601234567, 0).UTC()),
		},
		{
			name: "an unparseable timestamp is absent, not fatal",
			body: `{"photoTakenTime":{"timestamp":"nonsense"},"description":"kept"}`,
		},
		{
			name: "empty sidecar",
			body: `{}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "IMG_0001.jpg.json")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			meta, err := Read(t.Context(), path)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			switch {
			case tc.takenAt == nil && meta.TakenAt != nil:
				t.Errorf("TakenAt = %v, want none", meta.TakenAt)
			case tc.takenAt != nil && (meta.TakenAt == nil || !meta.TakenAt.Equal(*tc.takenAt)):
				t.Errorf("TakenAt = %v, want %v", meta.TakenAt, *tc.takenAt)
			}
			switch {
			case tc.lat == nil && meta.Lat != nil:
				t.Errorf("Lat = %v, want none", *meta.Lat)
			case tc.lat != nil && (meta.Lat == nil || *meta.Lat != *tc.lat):
				t.Errorf("Lat = %v, want %v", meta.Lat, *tc.lat)
			}
		})
	}
}

// TestReadRejectsUnknownFormat keeps `.aae` and friends out: an Apple AAE file
// describes an edit, not metadata, and must never be read as a sidecar.
func TestReadRejectsUnknownFormat(t *testing.T) {
	t.Parallel()

	if _, err := Read(t.Context(), filepath.Join("testdata", "IMG_1234.jpg.aae")); err == nil {
		t.Fatal("Read(.aae) = nil error, want an error")
	}
}

// TestReadGoogleMalformed reports a corrupt sidecar rather than silently
// importing the photo without its date.
func TestReadGoogleMalformed(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "IMG_0002.jpg.json")
	if err := os.WriteFile(path, []byte(`{"photoTakenTime":`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Read(t.Context(), path); err == nil {
		t.Fatal("Read(malformed) = nil error, want an error")
	}
}
