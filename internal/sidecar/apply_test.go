package sidecar

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/exif"
)

// captured is the true capture time used across the precedence tests, and
// exported is what a Takeout re-encode stamps into DateTimeOriginal years later.
var (
	captured = time.Date(2016, 6, 6, 18, 2, 22, 0, time.UTC)
	exported = time.Date(2021, 3, 2, 9, 0, 0, 0, time.UTC)
)

// TestApplyTakenAt is the heart of the feature. EXIF is the primary source, but
// a Takeout export routinely carries a bogus DateTimeOriginal equal to the day it
// was exported — believe it and the photo lands five years from where it belongs.
func TestApplyTakenAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		meta       exif.Metadata
		sidecar    Metadata
		wantTaken  time.Time
		wantSource exif.Source
	}{
		{
			name:       "no EXIF date at all: the sidecar is all there is",
			meta:       exif.Metadata{TakenAtSource: exif.SourceUnknown},
			sidecar:    Metadata{TakenAt: &captured},
			wantTaken:  captured,
			wantSource: exif.SourceSidecar,
		},
		{
			name: "a date guessed from the file name loses to the sidecar",
			meta: exif.Metadata{
				TakenAt:       new(time.Date(2016, 6, 6, 0, 0, 0, 0, time.UTC)),
				TakenAtSource: exif.SourceFilename,
			},
			sidecar:    Metadata{TakenAt: &captured},
			wantTaken:  captured,
			wantSource: exif.SourceSidecar,
		},
		{
			name:       "a bogus EXIF date from the export loses to the sidecar",
			meta:       exif.Metadata{TakenAt: &exported, TakenAtSource: exif.SourceExif},
			sidecar:    Metadata{TakenAt: &captured},
			wantTaken:  captured,
			wantSource: exif.SourceSidecar,
		},
		{
			name: "a plausible EXIF date wins: the zone offset is not a re-encode",
			meta: exif.Metadata{
				TakenAt:       new(captured.Add(10 * time.Hour)),
				TakenAtSource: exif.SourceExif,
			},
			sidecar:    Metadata{TakenAt: &captured},
			wantTaken:  captured.Add(10 * time.Hour),
			wantSource: exif.SourceExif,
		},
		{
			name: "an EXIF date before the sidecar's wins: only a later one is suspect",
			meta: exif.Metadata{
				TakenAt:       new(captured.Add(-72 * time.Hour)),
				TakenAtSource: exif.SourceExif,
			},
			sidecar:    Metadata{TakenAt: &captured},
			wantTaken:  captured.Add(-72 * time.Hour),
			wantSource: exif.SourceExif,
		},
		{
			name:       "a sidecar with no date never clears the EXIF one",
			meta:       exif.Metadata{TakenAt: &captured, TakenAtSource: exif.SourceExif},
			sidecar:    Metadata{Description: "no date here"},
			wantTaken:  captured,
			wantSource: exif.SourceExif,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			meta := tc.meta
			Apply(&meta, tc.sidecar)

			if meta.TakenAt == nil || !meta.TakenAt.Equal(tc.wantTaken) {
				t.Errorf("TakenAt = %v, want %v", meta.TakenAt, tc.wantTaken)
			}
			if meta.TakenAtSource != tc.wantSource {
				t.Errorf("TakenAtSource = %q, want %q", meta.TakenAtSource, tc.wantSource)
			}
		})
	}
}

// TestApplyGPSFillsOnlyGaps: an export fills what the file does not carry and
// overwrites nothing it does. Half a fix is not a location, so latitude and
// longitude move together.
func TestApplyGPSFillsOnlyGaps(t *testing.T) {
	t.Parallel()

	t.Run("fills a missing fix", func(t *testing.T) {
		t.Parallel()
		meta := exif.Metadata{}
		Apply(&meta, Metadata{Lat: new(48.6), Lng: new(14.0), Altitude: new(726.0)})

		if meta.Lat == nil || *meta.Lat != 48.6 || meta.Lng == nil || *meta.Lng != 14.0 {
			t.Errorf("GPS = %v/%v, want the sidecar's", meta.Lat, meta.Lng)
		}
		if meta.Altitude == nil || *meta.Altitude != 726 {
			t.Errorf("Altitude = %v, want 726", meta.Altitude)
		}
	})

	t.Run("keeps the file's own fix", func(t *testing.T) {
		t.Parallel()
		meta := exif.Metadata{Lat: new(50.1), Lng: new(14.4)}
		Apply(&meta, Metadata{Lat: new(48.6), Lng: new(14.0)})

		if *meta.Lat != 50.1 || *meta.Lng != 14.4 {
			t.Errorf("GPS = %v/%v, want the file's own EXIF fix", *meta.Lat, *meta.Lng)
		}
	})
}

// TestApplyStampsTheSidecarDocument checks where the export's people and keywords
// end up: in the photo's metadata document, and nowhere else. Google's people
// carry no face box, so nothing can honestly become a subject or a marker.
func TestApplyStampsTheSidecarDocument(t *testing.T) {
	t.Parallel()

	meta := exif.Metadata{}
	Apply(&meta, Metadata{
		Source:   SourceGoogle,
		Path:     "/tmp/Takeout/IMG_1234.jpg.json",
		TakenAt:  &captured,
		People:   []string{"Jan Novák"},
		Keywords: []string{"Praha"},
		Favorite: true,
		Rating:   4,
	})

	doc, ok := meta.Exif[exifKey].(map[string]any)
	if !ok {
		t.Fatalf("Exif[%q] = %v, want a document", exifKey, meta.Exif[exifKey])
	}
	if doc["Source"] != string(SourceGoogle) {
		t.Errorf("Source = %v, want %q", doc["Source"], SourceGoogle)
	}
	if doc["File"] != "IMG_1234.jpg.json" {
		t.Errorf("File = %v, want the sidecar's base name", doc["File"])
	}
	people, ok := doc["People"].([]string)
	if !ok || len(people) != 1 || people[0] != "Jan Novák" {
		t.Errorf("People = %v, want the export's names kept as metadata", doc["People"])
	}
	if doc["Favorited"] != true || doc["Rating"] != 4 {
		t.Errorf("Favorited = %v, Rating = %v", doc["Favorited"], doc["Rating"])
	}
}

// TestApplyEmptySidecarChangesNothing: a sidecar that says nothing must not
// stamp an empty document onto a photo that has proper EXIF.
func TestApplyEmptySidecarChangesNothing(t *testing.T) {
	t.Parallel()

	meta := exif.Metadata{TakenAt: &captured, TakenAtSource: exif.SourceExif}
	Apply(&meta, Metadata{Source: SourceGoogle, Path: "IMG_1.jpg.json"})

	if meta.Exif != nil {
		t.Errorf("Exif = %v, want nothing stamped for an empty sidecar", meta.Exif)
	}
	if meta.TakenAtSource != exif.SourceExif {
		t.Errorf("TakenAtSource = %q, want it untouched", meta.TakenAtSource)
	}
}
