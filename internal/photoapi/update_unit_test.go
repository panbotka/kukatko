package photoapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// decodeBody is a test helper that runs decodeUpdate over a JSON string.
func decodeBody(t *testing.T, json string) (map[string]struct{}, updateBody, error) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/photos/x", strings.NewReader(json))
	return decodeUpdate(req)
}

// TestDecodeUpdate_presence verifies that decodeUpdate reports exactly the keys
// the caller sent, distinguishing an omitted field from one explicitly null.
func TestDecodeUpdate_presence(t *testing.T) {
	t.Parallel()

	present, body, err := decodeBody(t, `{"title":"Sunset","taken_at":null}`)
	if err != nil {
		t.Fatalf("decodeUpdate: %v", err)
	}
	if _, ok := present["title"]; !ok {
		t.Error("title not reported present")
	}
	if _, ok := present["taken_at"]; !ok {
		t.Error("taken_at (explicit null) not reported present")
	}
	if _, ok := present["description"]; ok {
		t.Error("description reported present though omitted")
	}
	if body.Title == nil || *body.Title != "Sunset" {
		t.Errorf("title value = %v, want Sunset", body.Title)
	}
	if body.TakenAt != nil {
		t.Errorf("taken_at value = %v, want nil", body.TakenAt)
	}
}

// TestDecodeUpdate_errors verifies malformed and hostile bodies are rejected.
func TestDecodeUpdate_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"title":`},
		{name: "unknown field", body: `{"colour":"red"}`},
		{name: "not an object", body: `["title"]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := decodeBody(t, tt.body); err == nil {
				t.Errorf("decodeUpdate(%q) = nil error, want rejection", tt.body)
			}
		})
	}
}

// TestMergeUpdate verifies the overlay of present fields onto the current photo's
// metadata, including clearing via null and coordinate validation.
func TestMergeUpdate(t *testing.T) {
	t.Parallel()

	taken := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC)
	base := photos.Photo{
		Title:         "Old",
		Description:   "desc",
		Notes:         "notes",
		AiNote:        "old ai note",
		TakenAt:       &taken,
		TakenAtSource: "exif",
		Lat:           new(10.0),
		Lng:           new(20.0),
		// Only an importer writes this column now; the merge must carry it over.
		Private: true,
	}

	t.Run("omitted fields are unchanged", func(t *testing.T) {
		t.Parallel()
		got, err := mergeUpdate(base, map[string]struct{}{}, updateBody{})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.Title != "Old" || got.TakenAtSource != "exif" || got.Lat == nil || *got.Lat != 10 {
			t.Errorf("unchanged merge altered values: %+v", got)
		}
	})

	t.Run("title overwritten, the imported private flag carried over", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"title": {}}
		got, err := mergeUpdate(base, present, updateBody{Title: new("New")})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.Title != "New" {
			t.Errorf("title not applied: %+v", got)
		}
		// The update overwrites the whole row, so an edit must not clear the flag
		// the importer set.
		if !got.Private {
			t.Errorf("private cleared by an unrelated edit: %+v", got)
		}
	})

	t.Run("ai_note overwritten while notes unchanged", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"ai_note": {}}
		got, err := mergeUpdate(base, present, updateBody{AiNote: new("fresh ai note")})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.AiNote != "fresh ai note" {
			t.Errorf("ai_note = %q, want %q", got.AiNote, "fresh ai note")
		}
		if got.Notes != "notes" {
			t.Errorf("notes = %q, want unchanged %q", got.Notes, "notes")
		}
	})

	t.Run("omitted ai_note is unchanged", func(t *testing.T) {
		t.Parallel()
		got, err := mergeUpdate(base, map[string]struct{}{}, updateBody{})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.AiNote != "old ai note" {
			t.Errorf("ai_note = %q, want unchanged %q", got.AiNote, "old ai note")
		}
	})

	t.Run("taken_at set marks manual source", func(t *testing.T) {
		t.Parallel()
		newTaken := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
		present := map[string]struct{}{"taken_at": {}}
		got, err := mergeUpdate(base, present, updateBody{TakenAt: &newTaken})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.TakenAt == nil || !got.TakenAt.Equal(newTaken) || got.TakenAtSource != "manual" {
			t.Errorf("taken_at not applied with manual source: %+v", got)
		}
	})

	t.Run("taken_at null clears and resets source", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"taken_at": {}}
		got, err := mergeUpdate(base, present, updateBody{TakenAt: nil})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.TakenAt != nil || got.TakenAtSource != "unknown" {
			t.Errorf("taken_at not cleared: %+v", got)
		}
	})

	t.Run("gps null clears coordinates", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"lat": {}, "lng": {}}
		got, err := mergeUpdate(base, present, updateBody{Lat: nil, Lng: nil})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.Lat != nil || got.Lng != nil {
			t.Errorf("coordinates not cleared: lat=%v lng=%v", got.Lat, got.Lng)
		}
	})

	t.Run("out-of-range latitude rejected", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"lat": {}}
		if _, err := mergeUpdate(base, present, updateBody{Lat: new(91.0)}); err == nil {
			t.Error("mergeUpdate accepted latitude 91")
		}
	})

	t.Run("out-of-range longitude rejected", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"lng": {}}
		if _, err := mergeUpdate(base, present, updateBody{Lng: new(-181.0)}); err == nil {
			t.Error("mergeUpdate accepted longitude -181")
		}
	})
}

// TestMergeUpdate_credits verifies the IPTC/XMP credit fields: the present ones
// are trimmed and applied, the absent ones carried over untouched, an over-long
// value is rejected (the handler turns that into a 400), and the machine-derived
// file metadata is never part of the update at all.
func TestMergeUpdate_credits(t *testing.T) {
	t.Parallel()

	base := photos.Photo{
		Subject:   "old subject",
		Keywords:  "old,keywords",
		Artist:    "Old Artist",
		Copyright: "© old",
		License:   "old licence",
		Scan:      true,
	}

	t.Run("present fields are trimmed and applied", func(t *testing.T) {
		t.Parallel()
		present := map[string]struct{}{"subject": {}, "artist": {}, "scan": {}}
		got, err := mergeUpdate(base, present, updateBody{
			Subject: new("  Sunset over the lagoon  "), Artist: new(" Jan Novák "), Scan: new(false),
		})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.Subject != "Sunset over the lagoon" || got.Artist != "Jan Novák" || got.Scan {
			t.Errorf("credit fields not applied: %+v", got)
		}
		// The fields the caller did not send keep the photo's current values.
		if got.Keywords != "old,keywords" || got.Copyright != "© old" || got.License != "old licence" {
			t.Errorf("an absent credit field was cleared: %+v", got)
		}
	})

	t.Run("over-long values are rejected", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name    string
			present string
			body    updateBody
		}{
			{"subject", "subject", updateBody{Subject: new(strings.Repeat("a", 1001))}},
			{"keywords", "keywords", updateBody{Keywords: new(strings.Repeat("a", 2001))}},
			{"artist", "artist", updateBody{Artist: new(strings.Repeat("a", 256))}},
			{"copyright", "copyright", updateBody{Copyright: new(strings.Repeat("a", 1001))}},
			{"license", "license", updateBody{License: new(strings.Repeat("a", 1001))}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				present := map[string]struct{}{tc.present: {}}
				if _, err := mergeUpdate(base, present, tc.body); err == nil {
					t.Errorf("mergeUpdate(%s over the cap) = nil error, want rejection", tc.name)
				}
			})
		}
	})

	t.Run("a value at the cap is accepted", func(t *testing.T) {
		t.Parallel()
		artist := strings.Repeat("a", 255)
		got, err := mergeUpdate(base, map[string]struct{}{"artist": {}}, updateBody{Artist: &artist})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.Artist != artist {
			t.Errorf("a value exactly at the cap was not applied")
		}
	})

	t.Run("an explicit null leaves the NOT NULL column unchanged", func(t *testing.T) {
		t.Parallel()
		got, err := mergeUpdate(base, map[string]struct{}{"subject": {}}, updateBody{Subject: nil})
		if err != nil {
			t.Fatalf("mergeUpdate: %v", err)
		}
		if got.Subject != "old subject" {
			t.Errorf("subject = %q, want unchanged on an explicit null", got.Subject)
		}
	})
}
