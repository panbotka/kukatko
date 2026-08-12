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

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
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

// putEdit saves an edit for a photo and fails the test unless it is accepted.
func putEdit(t *testing.T, client *http.Client, base, uid string, edit map[string]any) {
	t.Helper()
	body, err := json.Marshal(edit)
	if err != nil {
		t.Fatalf("marshal edit: %v", err)
	}
	resp := mustDo(t, client, http.MethodPut, base+"/api/v1/photos/"+uid+"/edit", body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT edit status = %d, want 200", resp.StatusCode)
	}
}

// thumbnailJobPayloads returns the payload of every queued/running thumbnail job,
// so a test can assert both that the rebuild was scheduled and that it asks for
// the forced rebuild rather than the skip-what-is-cached repair.
func thumbnailJobPayloads(t *testing.T, e *env) []struct {
	PhotoUID string `json:"photo_uid"`
	Force    bool   `json:"force"`
} {
	t.Helper()
	all, err := e.jobs.List(t.Context(), jobs.ListOptions{Limit: 50})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	var out []struct {
		PhotoUID string `json:"photo_uid"`
		Force    bool   `json:"force"`
	}
	for _, job := range all {
		if job.Type != jobs.TypeThumbnail {
			continue
		}
		var decoded struct {
			PhotoUID string `json:"photo_uid"`
			Force    bool   `json:"force"`
		}
		if err := json.Unmarshal(job.Payload, &decoded); err != nil {
			t.Fatalf("decode thumbnail payload %q: %v", job.Payload, err)
		}
		out = append(out, decoded)
	}
	return out
}

// TestEdit_putAuditsAndRebuildsThumbnails verifies the two consequences a saved
// edit must have beyond the photo_edits row: an audit entry attributed to the
// acting user and carrying the rotation, and a forced thumbnail job so the library
// grid stops showing the previous rendering.
func TestEdit_putAuditsAndRebuildsThumbnails(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "edit_audit", auth.RoleEditor)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 15, 25, 35)

	putEdit(t, client, env.server.URL, photo.UID, map[string]any{"rotation": 270})

	rows, err := audit.NewStore(env.db.Pool()).List(t.Context(),
		audit.Filter{Action: audit.ActionPhotoEdit})
	if err != nil {
		t.Fatalf("listing audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("photo.edit audit rows = %d, want 1", len(rows))
	}
	entry := rows[0]
	if entry.TargetType != "photos" || entry.TargetUID == nil || *entry.TargetUID != photo.UID {
		t.Errorf("audit target = %s/%v, want photos/%s", entry.TargetType, entry.TargetUID, photo.UID)
	}
	if entry.ActorUID == nil || *entry.ActorUID == "" {
		t.Error("audit entry has no actor")
	}
	// JSONB numbers decode as float64.
	if got, ok := entry.Details["rotation"].(float64); !ok || got != 270 {
		t.Errorf("audit details rotation = %v, want 270", entry.Details["rotation"])
	}

	payloads := thumbnailJobPayloads(t, env)
	if len(payloads) != 1 {
		t.Fatalf("thumbnail jobs = %d, want 1", len(payloads))
	}
	if payloads[0].PhotoUID != photo.UID || !payloads[0].Force {
		t.Errorf("thumbnail job = %+v, want a forced rebuild of %s", payloads[0], photo.UID)
	}
}

// TestEdit_neutralSaveRebuildsThumbnails verifies resetting an edit is not a
// special case: the neutral edit is audited and schedules the same forced rebuild,
// because the cache is keyed by the original's hash and would otherwise keep
// serving the rotation the user just took back.
func TestEdit_neutralSaveRebuildsThumbnails(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "edit_reset", auth.RoleEditor)
	photo := env.seedPhoto(t, photos.Photo{Title: "p"}, "p.jpg", 16, 26, 36)

	putEdit(t, client, env.server.URL, photo.UID,
		map[string]any{"rotation": 0, "brightness": 0, "contrast": 0})

	if got := env.countAuditAction(t, audit.ActionPhotoEdit); got != 1 {
		t.Errorf("photo.edit audit rows = %d, want 1 (a reset is an edit too)", got)
	}
	payloads := thumbnailJobPayloads(t, env)
	if len(payloads) != 1 || !payloads[0].Force {
		t.Errorf("thumbnail jobs = %+v, want one forced rebuild", payloads)
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
