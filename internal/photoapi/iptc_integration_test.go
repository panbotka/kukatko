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

// TestUpdateMetadata_iptcFields verifies PATCH /photos/{uid} writes the six
// editable IPTC/XMP credit fields, trims their whitespace, serves them back in the
// detail body, and refuses to touch the machine-derived file metadata.
func TestUpdateMetadata_iptcFields(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)

	seeded := env.seedPhoto(t, photos.Photo{
		Title: "Vinobraní", TakenAtSource: "exif",
	}, "iptc.jpg", 10, 20, 30)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	body := []byte(`{
		"subject":"  Sklizeň hroznů na jižní Moravě  ",
		"artist":"Jan Novák",
		"copyright":"© 2024 Jan Novák",
		"license":"CC BY-NC 4.0",
		"keywords":"vinobraní,hrozny,sklizeň",
		"scan":true
	}`)
	resp := mustDo(t, editor, http.MethodPatch, url, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp.StatusCode)
	}
	var patched photos.Photo
	if err := json.NewDecoder(resp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch: %v", err)
	}

	// The subject arrived padded; it is stored trimmed.
	if patched.Subject != "Sklizeň hroznů na jižní Moravě" {
		t.Errorf("subject = %q, want the trimmed value", patched.Subject)
	}
	if patched.Artist != "Jan Novák" || patched.Copyright != "© 2024 Jan Novák" ||
		patched.License != "CC BY-NC 4.0" || patched.Keywords != "vinobraní,hrozny,sklizeň" || !patched.Scan {
		t.Errorf("credit fields not applied: %+v", patched)
	}
	// An untouched field survives the overwrite the store does.
	if patched.Title != "Vinobraní" {
		t.Errorf("title = %q, want the unchanged 'Vinobraní'", patched.Title)
	}

	// The persisted photo serves the same values from the detail route.
	detailResp := mustDo(t, editor, http.MethodGet, url, nil)
	defer func() { _ = detailResp.Body.Close() }()
	var detail photos.Photo
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Subject != "Sklizeň hroznů na jižní Moravě" || detail.Keywords != "vinobraní,hrozny,sklizeň" ||
		detail.Artist != "Jan Novák" || !detail.Scan {
		t.Errorf("detail does not carry the credit fields: %+v", detail)
	}
}

// TestUpdateMetadata_iptcValidation verifies the length caps answer 400, that a
// machine-derived column cannot be written through the editable route, and that a
// viewer is still refused.
func TestUpdateMetadata_iptcValidation(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	viewer, _ := env.login(t, "viewer", auth.RoleViewer)

	seeded := env.seedPhoto(t, photos.Photo{TakenAtSource: "exif"}, "iptc-limits.jpg", 40, 50, 60)
	url := env.server.URL + "/api/v1/photos/" + seeded.UID

	// Each field's cap, one rune over. The values are plain ASCII so the rune count
	// is the byte count and the intent stays readable.
	tooLong := []struct {
		name string
		body string
	}{
		{"subject", `{"subject":"` + strings.Repeat("a", 1001) + `"}`},
		{"copyright", `{"copyright":"` + strings.Repeat("a", 1001) + `"}`},
		{"license", `{"license":"` + strings.Repeat("a", 1001) + `"}`},
		{"keywords", `{"keywords":"` + strings.Repeat("a", 2001) + `"}`},
		{"artist", `{"artist":"` + strings.Repeat("a", 256) + `"}`},
	}
	for _, tc := range tooLong {
		t.Run("over-long "+tc.name+" is 400", func(t *testing.T) {
			resp := mustDo(t, editor, http.MethodPatch, url, []byte(tc.body))
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", resp.StatusCode)
			}
		})
	}

	t.Run("value at the cap is accepted", func(t *testing.T) {
		body := []byte(`{"artist":"` + strings.Repeat("a", 255) + `"}`)
		resp := mustDo(t, editor, http.MethodPatch, url, body)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200 for a value exactly at the cap", resp.StatusCode)
		}
	})

	t.Run("machine-derived field is rejected", func(t *testing.T) {
		// software/color_profile/image_codec/camera_serial/original_name/projection
		// are not on the update body, so the decoder rejects them as unknown fields:
		// an edit may not rewrite what the file itself said.
		resp := mustDo(t, editor, http.MethodPatch, url, []byte(`{"software":"Photoshop"}`))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})

	t.Run("viewer is forbidden", func(t *testing.T) {
		resp := mustDo(t, viewer, http.MethodPatch, url, []byte(`{"subject":"Nope"}`))
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("viewer patch status = %d, want 403", resp.StatusCode)
		}
	})
}
