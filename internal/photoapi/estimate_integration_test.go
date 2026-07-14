//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestUpdateMetadata_takenAtEstimate verifies PATCH /photos/{uid} writes the
// approximate-date pair — the estimate flag and the free-text dating note — trims
// the note, serves both back in the detail body, and drops the note again as soon
// as the flag is cleared.
func TestUpdateMetadata_takenAtEstimate(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Babička na dvoře", TakenAtSource: "manual",
	}, "estimate.jpg", 11, 21, 31)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	// A fresh photo is not an estimate and carries no note.
	if seeded.TakenAtEstimated || seeded.TakenAtNote != "" {
		t.Fatalf("seeded photo already estimated: %+v", seeded)
	}

	body := []byte(`{"taken_at_estimated":true,"taken_at_note":"  kolem roku 1950, podle babičky  "}`)
	patched := patchPhoto(t, editor, url, body)
	if !patched.TakenAtEstimated {
		t.Errorf("taken_at_estimated = false, want true")
	}
	if patched.TakenAtNote != "kolem roku 1950, podle babičky" {
		t.Errorf("taken_at_note = %q, want the trimmed value", patched.TakenAtNote)
	}
	// An untouched field survives the whole-row overwrite the store does.
	if patched.Title != "Babička na dvoře" {
		t.Errorf("title = %q, want the unchanged 'Babička na dvoře'", patched.Title)
	}

	// The persisted photo serves the same values from the detail route.
	detailResp := mustDo(t, editor, http.MethodGet, url, nil)
	defer func() { _ = detailResp.Body.Close() }()
	var detail photos.Photo
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if !detail.TakenAtEstimated || detail.TakenAtNote != "kolem roku 1950, podle babičky" {
		t.Errorf("detail does not carry the estimate: %+v", detail)
	}

	// Clearing the flag must clear the note with it: a date presented as a fact may
	// not keep a stale dating remark hanging off it.
	cleared := patchPhoto(t, editor, url, []byte(`{"taken_at_estimated":false}`))
	if cleared.TakenAtEstimated {
		t.Errorf("taken_at_estimated = true after clearing")
	}
	if cleared.TakenAtNote != "" {
		t.Errorf("taken_at_note = %q, want it cleared with the flag", cleared.TakenAtNote)
	}
}

// TestUpdateMetadata_takenAtEstimateUndated verifies the edge case the feature
// exists for: a photo with no capture time at all may still be flagged as an
// estimate, with the note carrying the whole meaning.
func TestUpdateMetadata_takenAtEstimateUndated(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	seeded := env.seedPhoto(t, photos.Photo{TakenAtSource: "unknown"}, "undated.jpg", 12, 22, 32)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	patched := patchPhoto(t, editor, url,
		[]byte(`{"taken_at_estimated":true,"taken_at_note":"někdy ve 40. letech"}`))
	if patched.TakenAt != nil {
		t.Errorf("taken_at = %v, want it still unset", patched.TakenAt)
	}
	if !patched.TakenAtEstimated || patched.TakenAtNote != "někdy ve 40. letech" {
		t.Errorf("undated estimate not applied: %+v", patched)
	}
}

// TestUpdateMetadata_takenAtEstimateValidation verifies the note's length cap
// answers 400, a note exactly at the cap is accepted, and a viewer is still
// refused.
func TestUpdateMetadata_takenAtEstimateValidation(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)

	seeded := env.seedPhoto(t, photos.Photo{TakenAtSource: "exif"}, "estimate-limits.jpg", 13, 23, 33)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	t.Run("over-long note is 400", func(t *testing.T) {
		body := []byte(`{"taken_at_estimated":true,"taken_at_note":"` + strings.Repeat("a", 501) + `"}`)
		resp := mustDo(t, editor, http.MethodPatch, url, body)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("note at the cap is accepted", func(t *testing.T) {
		note := strings.Repeat("a", 500)
		body := []byte(`{"taken_at_estimated":true,"taken_at_note":"` + note + `"}`)
		patched := patchPhoto(t, editor, url, body)
		if patched.TakenAtNote != note {
			t.Errorf("a note exactly at the cap was not applied")
		}
	})

	t.Run("viewer is forbidden", func(t *testing.T) {
		resp := mustDo(t, viewer, http.MethodPatch, url, []byte(`{"taken_at_estimated":true}`))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer patch status = %d, want 403", resp.StatusCode)
		}
	})
}

// patchPhoto PATCHes the photo at url as the given client, asserts a 200 and
// returns the refreshed photo the handler answered with.
func patchPhoto(t *testing.T, client *http.Client, url string, body []byte) photos.Photo {
	t.Helper()
	resp := mustDo(t, client, http.MethodPatch, url, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp.StatusCode)
	}
	var patched photos.Photo
	if err := json.NewDecoder(resp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	return patched
}
