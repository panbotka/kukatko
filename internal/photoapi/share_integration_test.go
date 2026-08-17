//go:build integration

package photoapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// shareManifestFile mirrors one entry of the share-manifest response body.
type shareManifestFile struct {
	UID     string `json:"uid"`
	Name    string `json:"name"`
	Mime    string `json:"mime"`
	Size    int64  `json:"size"`
	Preview bool   `json:"preview"`
}

// shareURL is the share-manifest endpoint under the API base path.
func shareURL(base string) string { return base + "/api/v1/photos/share-manifest" }

// postShareManifest posts a selection to the share-manifest endpoint and returns
// the response; the caller closes the body.
func postShareManifest(t *testing.T, client *http.Client, base string, uids []string) *http.Response {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"photo_uids": uids})
	if err != nil {
		t.Fatalf("marshal share request: %v", err)
	}
	return mustDo(t, client, http.MethodPost, shareURL(base), raw)
}

// readShareManifest decodes a 200 share-manifest response into its files, failing
// the test on any other status.
func readShareManifest(t *testing.T, resp *http.Response) []shareManifestFile {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("share manifest status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Files []shareManifestFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode share manifest: %v", err)
	}
	return body.Files
}

// TestShareManifest_describesSelectionInOrder proves the manifest answers for the
// whole selection in the client's own order, states each file's name/type/size off
// the catalogue row, marks a RAW original for its JPEG preview, and silently skips
// a UID that resolves to nothing (as the ZIP and the single download do).
func TestShareManifest_describesSelectionInOrder(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	jpg := env.seedPhoto(t, photos.Photo{Title: "Beach", TakenAtSource: "unknown"}, "beach.jpg", 10, 20, 30)
	clip := env.seedPhoto(t, photos.Photo{
		Title: "Clip", TakenAtSource: "unknown", MediaType: photos.MediaVideo,
	}, "clip.mp4", 40, 50, 60)
	raw := env.seedPhoto(t, photos.Photo{Title: "Raw", TakenAtSource: "unknown"}, "IMG_0007.CR2", 70, 80, 90)

	resp := postShareManifest(t, client, env.server.URL, []string{clip.UID, "pt-missing", jpg.UID, raw.UID})
	defer func() { _ = resp.Body.Close() }()

	files := readShareManifest(t, resp)
	if len(files) != 3 {
		t.Fatalf("manifest holds %d files, want 3: %+v", len(files), files)
	}
	if files[0].UID != clip.UID || files[1].UID != jpg.UID || files[2].UID != raw.UID {
		t.Errorf("manifest order = %s/%s/%s, want the requested order minus the missing uid",
			files[0].UID, files[1].UID, files[2].UID)
	}
	if files[0].Name != "clip.mp4" || files[0].Mime != clip.FileMime || files[0].Preview {
		t.Errorf("video entry = %+v, want the original name/type and no preview", files[0])
	}
	if files[1].Name != "beach.jpg" || files[1].Size != jpg.FileSize {
		t.Errorf("photo entry = %+v, want beach.jpg with the stored size %d", files[1], jpg.FileSize)
	}
	// The RAW is shared as a JPEG preview: named .jpg, typed image/jpeg, and
	// budgeted with the original's (larger) size.
	if !files[2].Preview || files[2].Name != "IMG_0007.jpg" || files[2].Mime != "image/jpeg" {
		t.Errorf("raw entry = %+v, want a preview named IMG_0007.jpg typed image/jpeg", files[2])
	}
	if files[2].Size != raw.FileSize {
		t.Errorf("raw entry size = %d, want the original's %d as an upper bound", files[2].Size, raw.FileSize)
	}
}

// TestShareManifest_deduplicatesNames proves two photos carrying one file name
// arrive under two names, so a share sheet cannot silently collapse them into one
// picture in the phone's library.
func TestShareManifest_deduplicatesNames(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	p1 := env.seedPhoto(t, photos.Photo{Title: "A", TakenAtSource: "unknown"}, "IMG.jpg", 11, 21, 31)
	p2 := env.seedPhoto(t, photos.Photo{Title: "B", TakenAtSource: "unknown"}, "IMG.jpg", 41, 51, 61)

	resp := postShareManifest(t, client, env.server.URL, []string{p1.UID, p2.UID})
	defer func() { _ = resp.Body.Close() }()

	files := readShareManifest(t, resp)
	if len(files) != 2 {
		t.Fatalf("manifest holds %d files, want 2", len(files))
	}
	if files[0].Name != "IMG.jpg" || files[1].Name != "IMG (2).jpg" {
		t.Errorf("names = %q/%q, want IMG.jpg and IMG (2).jpg", files[0].Name, files[1].Name)
	}
}

// TestShareManifest_rejectsUnusableRequests proves the three refusals: an
// oversized selection is 413 (the ZIP cap is the precedent), a selection that
// resolves to nothing is 400, and an unauthenticated request never gets a file
// name at all.
func TestShareManifest_rejectsUnusableRequests(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = "p" + string(rune('a'+i%26))
	}
	capped := postShareManifest(t, client, env.server.URL, tooMany)
	defer func() { _ = capped.Body.Close() }()
	if capped.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("over-cap status = %d, want 413", capped.StatusCode)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(capped.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(errBody.Error, "too many") {
		t.Errorf("over-cap error = %q, want it to mention the cap", errBody.Error)
	}

	empty := postShareManifest(t, client, env.server.URL, []string{})
	defer func() { _ = empty.Body.Close() }()
	if empty.StatusCode != http.StatusBadRequest {
		t.Errorf("empty selection status = %d, want 400", empty.StatusCode)
	}

	unknown := postShareManifest(t, client, env.server.URL, []string{"pt-nope"})
	defer func() { _ = unknown.Body.Close() }()
	if unknown.StatusCode != http.StatusBadRequest {
		t.Errorf("unresolvable selection status = %d, want 400", unknown.StatusCode)
	}

	anon := postShareManifest(t, &http.Client{}, env.server.URL, []string{"whatever"})
	defer func() { _ = anon.Body.Close() }()
	if anon.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d, want 401", anon.StatusCode)
	}
}

// publishingFS is a storage backend that publishes an address for its objects —
// so the media routes take their redirecting branch — while still being able to
// serve the bytes itself. That is what the real R2 backend is (its objects are both
// signed-URL-addressable and readable through the application), and what the test
// double newSigningR2 cannot be: its bucket is a placeholder no Open can reach, so
// it can only ever prove a redirect.
type publishingFS struct {
	*storage.FS
}

// URL publishes the object at relPath on the media domain, mirroring an R2 backend
// with a Worker in front of it. It is unsigned: this double exists to make the
// routes *choose* the published branch, and signature verification is covered by
// the signing double in media_integration_test.go.
func (publishingFS) URL(relPath string) string {
	return testMediaBaseURL + "/" + relPath
}

// mirrorOriginal copies a seeded photo's original into dst under the very key the
// catalogue row names, the way `kukatko storage migrate-to-r2` puts an existing
// library into a bucket. It is what makes a photo seeded through env.fs readable
// through a second backend as well.
func mirrorOriginal(t *testing.T, env *env, dst storage.Storage, photo photos.Photo) {
	t.Helper()
	data := env.readOriginal(t, photo)
	if err := dst.Put(t.Context(), bytes.NewReader(data), storage.StoredFile{
		Hash:    photo.FileHash,
		RelPath: photo.FilePath,
		Size:    photo.FileSize,
		MIME:    photo.FileMime,
	}); err != nil {
		t.Fatalf("mirroring %s into the publishing backend: %v", photo.FilePath, err)
	}
}

// TestMediaRoutes_proxyStreamsBytesOnPublishedBackend is the test the share flow
// stands or falls on: with originals in the object store, both media routes must be
// able to answer with the bytes *through this application* when asked to, because a
// page cannot read a cross-origin redirect and would otherwise hand the share sheet
// nothing. Without ?proxy=true the same routes still redirect, so the cheap path
// stays the default.
func TestMediaRoutes_proxyStreamsBytesOnPublishedBackend(t *testing.T) {
	mediaFS, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	media := publishingFS{FS: mediaFS}
	env := newEnvWithMedia(t, media)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	client = noRedirectClient(client)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Remote", TakenAtSource: "unknown"}, "remote.jpg", 33, 44, 55)
	mirrorOriginal(t, env, media, seeded)

	base := env.server.URL + "/api/v1/photos/" + seeded.UID

	redirected := mustDo(t, client, http.MethodGet, base+"/download?original=true", nil)
	defer func() { _ = redirected.Body.Close() }()
	if redirected.StatusCode != http.StatusFound {
		t.Fatalf("plain download status = %d, want 302 (the default stays a redirect)", redirected.StatusCode)
	}
	if got, want := redirected.Header.Get("Location"), testMediaBaseURL+"/"+seeded.FilePath; got != want {
		t.Errorf("redirect Location = %q, want %q", got, want)
	}

	proxied := mustDo(t, client, http.MethodGet, base+"/download?original=true&proxy=true", nil)
	defer func() { _ = proxied.Body.Close() }()
	if proxied.StatusCode != http.StatusOK {
		t.Fatalf("proxied download status = %d, want 200", proxied.StatusCode)
	}
	body, err := io.ReadAll(proxied.Body)
	if err != nil {
		t.Fatalf("reading proxied original: %v", err)
	}
	if !bytes.Equal(body, env.readOriginal(t, seeded)) {
		t.Errorf("proxied download returned %d bytes, want the stored original", len(body))
	}
	if ct := proxied.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("proxied download Content-Type = %q, want image/jpeg", ct)
	}

	thumbResp := mustDo(t, client, http.MethodGet, base+"/thumb/fit_720?proxy=true", nil)
	defer func() { _ = thumbResp.Body.Close() }()
	if thumbResp.StatusCode != http.StatusOK {
		t.Fatalf("proxied thumb status = %d, want 200", thumbResp.StatusCode)
	}
	thumbBody, err := io.ReadAll(thumbResp.Body)
	if err != nil {
		t.Fatalf("reading proxied thumb: %v", err)
	}
	if len(thumbBody) < 2 || thumbBody[0] != 0xFF || thumbBody[1] != 0xD8 {
		t.Errorf("proxied thumb is not JPEG (len %d)", len(thumbBody))
	}
	if ct := thumbResp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("proxied thumb Content-Type = %q, want image/jpeg", ct)
	}
}

// TestMediaRoutes_proxyStillRequiresTheGuard proves ?proxy=true is only a choice of
// transport, never a way around authorization: unauthenticated it is a 401, exactly
// as the redirecting form is.
func TestMediaRoutes_proxyStillRequiresTheGuard(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)
	seeded := env.seedPhoto(t, photos.Photo{Title: "Local", TakenAtSource: "unknown"}, "local.jpg", 13, 23, 33)

	base := env.server.URL + "/api/v1/photos/" + seeded.UID
	for _, path := range []string{"/download?original=true&proxy=true", "/thumb/fit_720?proxy=true"} {
		resp := mustDo(t, &http.Client{}, http.MethodGet, base+path, nil)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated %s status = %d, want 401", path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	// The same requests do work for the logged-in caller, so the 401s above are the
	// guard talking and not a broken route.
	resp := mustDo(t, client, http.MethodGet, base+"/download?original=true&proxy=true", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("authenticated proxied download status = %d, want 200", resp.StatusCode)
	}
}
