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
