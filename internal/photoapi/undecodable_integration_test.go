//go:build integration

package photoapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
)

// seedStill stores data, whatever it is, as an original named name and
// catalogues an image photo over it with a primary file — the way a corrupt
// upload sits in a real library.
func (e *env) seedStill(t *testing.T, name string, data []byte) photos.Photo {
	t.Helper()
	stored, err := e.fs.Store(t.Context(), bytes.NewReader(data), time.Time{}, name)
	if err != nil {
		t.Fatalf("storage.Store(%s): %v", name, err)
	}
	created, err := e.store.Create(t.Context(), photos.Photo{
		ProjectID: project.BootstrapUID, FileHash: stored.Hash, FilePath: stored.RelPath,
		FileName: name, FileSize: stored.Size, FileMime: "image/jpeg", TakenAtSource: "unknown",
		FileWidth: 64, FileHeight: 48, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("store.Create(%s): %v", name, err)
	}
	if _, err := e.store.CreateFile(t.Context(), project.BootstrapUID, photos.PhotoFile{
		PhotoUID: created.UID, FilePath: created.FilePath, FileHash: created.FileHash,
		FileSize: created.FileSize, FileMime: created.FileMime, IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("store.CreateFile(%s): %v", name, err)
	}
	return created
}

// getStatusAndError issues a GET and returns the status and the JSON error
// string of the answer (empty for a body that is not one).
func getStatusAndError(t *testing.T, client *http.Client, url string) (int, string) {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	var body struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body.Error
}

// TestThumbRoute_undecodableOriginalIs404 proves a photo whose original can never
// become a picture — a JPEG cut off half-way, bytes that are no image at all — is
// answered 404 with its own error string on every thumbnail size and on the face
// crop cut from one, never the 500 that would log at ERROR on every view.
func TestThumbRoute_undecodableOriginalIs404(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.AccountViewer)

	good := jpegBytes(t, 70, 80, 90)
	originals := map[string][]byte{
		"truncated.jpg": good[:len(good)/2],
		"garbage.jpg":   []byte("this is not an image, only text pretending to be one"),
	}
	for name, data := range originals {
		t.Run(name, func(t *testing.T) {
			seeded := env.seedStill(t, name, data)
			base := env.server.URL + apiV + "/photos/" + seeded.UID
			for _, path := range []string{"/thumb/tile_100", "/thumb/fit_720", "/face" + faceBoxQuery} {
				status, msg := getStatusAndError(t, client, base+path)
				if status != http.StatusNotFound {
					t.Errorf("GET %s status = %d (%q), want 404", path, status, msg)
				}
				if msg != "original cannot be thumbnailed" {
					t.Errorf("GET %s error = %q, want the undecodable-original string", path, msg)
				}
			}
		})
	}
}

// TestThumbRoute_storageFailureStays500 proves the 404 is kept for the photo's
// own state: an original gone from storage is a fault of the server, and the
// route still answers it 500.
func TestThumbRoute_storageFailureStays500(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.AccountViewer)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Gone", TakenAtSource: "unknown"}, "gone.jpg", 91, 92, 93)
	if err := env.fs.Delete(t.Context(), seeded.FilePath); err != nil {
		t.Fatalf("deleting the original: %v", err)
	}

	status, msg := getStatusAndError(t, client, env.server.URL+apiV+"/photos/"+seeded.UID+"/thumb/tile_100")
	if status != http.StatusInternalServerError {
		t.Errorf("thumb status = %d (%q), want 500", status, msg)
	}
	if msg != "thumbnail unavailable" {
		t.Errorf("thumb error = %q, want the generic failure string", msg)
	}
}
