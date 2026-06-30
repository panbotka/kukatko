//go:build integration

package photoapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// editResp mirrors the photo_edits JSON returned by the edit endpoints.
type editResp struct {
	PhotoUID   string   `json:"photo_uid"`
	CropX      *float64 `json:"crop_x"`
	CropW      *float64 `json:"crop_w"`
	Rotation   int      `json:"rotation"`
	Brightness float64  `json:"brightness"`
	Contrast   float64  `json:"contrast"`
}

// getEdit fetches and decodes the stored edit for a photo.
func getEdit(t *testing.T, client *http.Client, base, uid string) editResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid+"/edit", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET edit status = %d, want 200", resp.StatusCode)
	}
	var out editResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode edit: %v", err)
	}
	return out
}

// TestEdit_getDefaultsToNeutral verifies an unedited photo reports a neutral edit
// rather than 404, so the editor UI always has a value to seed its controls.
func TestEdit_getDefaultsToNeutral(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 10, 20, 30)

	got := getEdit(t, client, env.server.URL, photo.UID)
	if got.PhotoUID != photo.UID || got.Rotation != 0 || got.Brightness != 0 || got.CropX != nil {
		t.Errorf("default edit = %+v, want neutral edit for %s", got, photo.UID)
	}
}

// TestEdit_putThenGet stores an edit and reads it back.
func TestEdit_putThenGet(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 11, 21, 31)

	// Use values exactly representable as float32 (the REAL column type) so the
	// round-trip is exact: 0.5 and -0.25 have no binary rounding.
	body, _ := json.Marshal(map[string]any{"rotation": 90, "brightness": 0.5, "contrast": -0.25})
	resp := mustDo(t, client, http.MethodPut, env.server.URL+"/api/v1/photos/"+photo.UID+"/edit", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT edit status = %d, want 200", resp.StatusCode)
	}

	got := getEdit(t, client, env.server.URL, photo.UID)
	if got.Rotation != 90 || got.Brightness != 0.5 || got.Contrast != -0.25 {
		t.Errorf("stored edit = %+v, want rotation 90 / brightness 0.5 / contrast -0.25", got)
	}
}

// TestEdit_putValidation rejects an out-of-range rotation with 400.
func TestEdit_putValidation(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 12, 22, 32)

	body, _ := json.Marshal(map[string]any{"rotation": 45})
	resp := mustDo(t, client, http.MethodPut, env.server.URL+"/api/v1/photos/"+photo.UID+"/edit", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT invalid rotation status = %d, want 400", resp.StatusCode)
	}
}

// TestEdit_putForbiddenForViewer confirms a viewer cannot save edits.
func TestEdit_putForbiddenForViewer(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 13, 23, 33)

	body, _ := json.Marshal(map[string]any{"rotation": 90})
	resp := mustDo(t, client, http.MethodPut, env.server.URL+"/api/v1/photos/"+photo.UID+"/edit", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer PUT edit status = %d, want 403", resp.StatusCode)
	}
}

// TestDownload_honorsRotationEdit checks that, once a 90° rotation is saved, the
// download endpoint serves a rotated JPEG (the seeded original is 64×48, so the
// rotated image is 48×64) while ?original=true still serves the unrotated bytes.
func TestDownload_honorsRotationEdit(t *testing.T) {
	env := newEnv(t)
	client, token := env.login(t, "editor", auth.RoleEditor)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 14, 24, 34)

	body, _ := json.Marshal(map[string]any{"rotation": 90})
	resp := mustDo(t, client, http.MethodPut, env.server.URL+"/api/v1/photos/"+photo.UID+"/edit", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT edit status = %d, want 200", resp.StatusCode)
	}

	editedW, editedH := downloadImageSize(t, client, env.server.URL, photo.UID, "")
	if editedW != 48 || editedH != 64 {
		t.Errorf("edited download size = %dx%d, want 48x64 (rotated)", editedW, editedH)
	}

	origW, origH := downloadImageSize(t, client, env.server.URL, photo.UID, "?original=true&t="+token)
	if origW != 64 || origH != 48 {
		t.Errorf("original download size = %dx%d, want 64x48 (unrotated)", origW, origH)
	}
}

// downloadImageSize downloads a photo (with the given query suffix) and returns
// the decoded image's dimensions.
func downloadImageSize(t *testing.T, client *http.Client, base, uid, query string) (int, int) {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, base+"/api/v1/photos/"+uid+"/download"+query, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// Not a JPEG (the original might not be) — fall back to a generic decode.
		img, _, derr := image.Decode(bytes.NewReader(data))
		if derr != nil {
			t.Fatalf("decode download: %v / %v", err, derr)
		}
		return img.Bounds().Dx(), img.Bounds().Dy()
	}
	return cfg.Width, cfg.Height
}
