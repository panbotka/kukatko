//go:build integration

package photoapi_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// zipURL is the bulk ZIP download endpoint under the API base path.
func zipURL(base string) string { return base + "/api/v1/photos/download-zip" }

// postZip posts a zipDownloadRequest-shaped body to the ZIP endpoint and returns
// the response; the caller closes the body.
func postZip(t *testing.T, client *http.Client, base string, body map[string]any) *http.Response {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal zip request: %v", err)
	}
	return mustDo(t, client, http.MethodPost, zipURL(base), raw)
}

// readZipEntries reads resp's body as a ZIP archive and returns a map of entry
// name to its bytes, failing the test if the archive is not valid.
func readZipEntries(t *testing.T, resp *http.Response) map[string][]byte {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading zip body: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader (len %d): %v", len(body), err)
	}
	out := make(map[string][]byte, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening zip entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("reading zip entry %s: %v", f.Name, err)
		}
		out[f.Name] = data
	}
	return out
}

// TestDownloadZip_streamsSelectionWithDedupedNames proves the endpoint streams a
// valid ZIP holding one entry per requested photo, that colliding original file
// names are de-duplicated (" (2)" before the extension) and that each entry
// carries the photo's exact original bytes.
func TestDownloadZip_streamsSelectionWithDedupedNames(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	// Two photos share the file name "IMG.jpg" (distinct colours → distinct
	// content and storage paths) to exercise entry-name de-duplication.
	p1 := env.seedPhoto(t, photos.Photo{Title: "A", TakenAtSource: "unknown"}, "IMG.jpg", 10, 20, 30)
	p2 := env.seedPhoto(t, photos.Photo{Title: "B", TakenAtSource: "unknown"}, "IMG.jpg", 40, 50, 60)
	p3 := env.seedPhoto(t, photos.Photo{Title: "C", TakenAtSource: "unknown"}, "clip.mp4", 70, 80, 90)

	resp := postZip(t, client, env.server.URL, map[string]any{
		"photo_uids": []string{p1.UID, p2.UID, p3.UID},
		"date":       "2026-07-12",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("zip status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="kukatko-photos-2026-07-12.zip"` {
		t.Errorf("Content-Disposition = %q, want dated default", cd)
	}

	entries := readZipEntries(t, resp)
	if len(entries) != 3 {
		t.Fatalf("archive holds %d entries, want 3: %v", len(entries), entryNames(entries))
	}
	for _, want := range []string{"IMG.jpg", "IMG (2).jpg", "clip.mp4"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("archive missing entry %q; has %v", want, entryNames(entries))
		}
	}
	// The bytes of each de-duplicated entry must equal one of the two originals.
	orig1 := env.readOriginal(t, p1)
	orig2 := env.readOriginal(t, p2)
	if !bytes.Equal(entries["IMG.jpg"], orig1) {
		t.Error("entry IMG.jpg does not match the first original's bytes")
	}
	if !bytes.Equal(entries["IMG (2).jpg"], orig2) {
		t.Error("entry IMG (2).jpg does not match the second original's bytes")
	}
}

// TestDownloadZip_skipsMissingOriginal proves that an original gone from storage
// is skipped and reported in a MISSING.txt manifest rather than aborting the
// whole archive.
func TestDownloadZip_skipsMissingOriginal(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	present := env.seedPhoto(t, photos.Photo{Title: "Here", TakenAtSource: "unknown"}, "here.jpg", 11, 21, 31)
	gone := env.seedPhoto(t, photos.Photo{Title: "Gone", TakenAtSource: "unknown"}, "gone.jpg", 41, 51, 61)
	// Remove the original from storage so opening it fails.
	if err := env.fs.Delete(t.Context(), gone.FilePath); err != nil {
		t.Fatalf("deleting original: %v", err)
	}

	resp := postZip(t, client, env.server.URL, map[string]any{
		"photo_uids": []string{present.UID, gone.UID},
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("zip status = %d, want 200 (a missing file must not be fatal)", resp.StatusCode)
	}
	entries := readZipEntries(t, resp)
	if _, ok := entries["here.jpg"]; !ok {
		t.Errorf("present original missing from archive; has %v", entryNames(entries))
	}
	if _, ok := entries["gone.jpg"]; ok {
		t.Error("missing original was included, want skipped")
	}
	manifest, ok := entries["MISSING.txt"]
	if !ok {
		t.Fatalf("archive has no MISSING.txt; has %v", entryNames(entries))
	}
	if !bytes.Contains(manifest, []byte("gone.jpg")) {
		t.Errorf("MISSING.txt does not mention gone.jpg: %q", manifest)
	}
}

// TestDownloadZip_enforcesCap proves a request naming more than the per-request
// file cap is rejected with 413 before any archive is written.
func TestDownloadZip_enforcesCap(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	tooMany := make([]string, 1001)
	for i := range tooMany {
		tooMany[i] = "p" + string(rune('a'+i%26))
	}
	resp := postZip(t, client, env.server.URL, map[string]any{"photo_uids": tooMany})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("cap status = %d, want 413", resp.StatusCode)
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&errBody); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if !strings.Contains(errBody.Error, "too many") {
		t.Errorf("cap error = %q, want it to mention the cap", errBody.Error)
	}
}

// TestDownloadZip_requiresAuth proves an unauthenticated request is rejected,
// mirroring the single-photo download's guard (RBAC honoured).
func TestDownloadZip_requiresAuth(t *testing.T) {
	env := newEnv(t)
	resp := postZip(t, &http.Client{}, env.server.URL, map[string]any{"photo_uids": []string{"whatever"}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated zip status = %d, want 401", resp.StatusCode)
	}
}

// TestDownloadZip_emptyRequestRejected proves a request naming no resolvable
// photo is a 400 rather than an empty archive.
func TestDownloadZip_emptyRequestRejected(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	resp := postZip(t, client, env.server.URL, map[string]any{"photo_uids": []string{}})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty zip status = %d, want 400", resp.StatusCode)
	}
}

// TestDownloadZip_albumExpandsToLivePhotos proves an album UID expands to the
// album's photos server-side, that the archive is named from the caller's Name,
// and that an archived member is excluded (visibility honoured — a photo the
// album view hides is not leaked into its download).
func TestDownloadZip_albumExpandsToLivePhotos(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "editor", auth.RoleEditor)

	album, err := env.organize.CreateAlbum(t.Context(), organize.Album{Title: "Trip"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	live1 := env.seedPhoto(t, photos.Photo{Title: "L1", TakenAtSource: "unknown"}, "l1.jpg", 12, 22, 32)
	live2 := env.seedPhoto(t, photos.Photo{Title: "L2", TakenAtSource: "unknown"}, "l2.jpg", 42, 52, 62)
	archived := env.seedPhoto(t, photos.Photo{Title: "Arch", TakenAtSource: "unknown"}, "arch.jpg", 72, 82, 92)
	for _, uid := range []string{live1.UID, live2.UID, archived.UID} {
		if err := env.organize.AddPhoto(t.Context(), album.UID, uid); err != nil {
			t.Fatalf("AddPhoto(%s): %v", uid, err)
		}
	}
	if _, err := env.store.Archive(t.Context(), archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	resp := postZip(t, client, env.server.URL, map[string]any{
		"album_uid": album.UID,
		"name":      "Trip",
	})
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("album zip status = %d, want 200", resp.StatusCode)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != `attachment; filename="Trip.zip"` {
		t.Errorf("Content-Disposition = %q, want Trip.zip", cd)
	}
	entries := readZipEntries(t, resp)
	if len(entries) != 2 {
		t.Fatalf("album archive holds %d entries, want 2 (archived excluded): %v", len(entries), entryNames(entries))
	}
	for _, want := range []string{"l1.jpg", "l2.jpg"} {
		if _, ok := entries[want]; !ok {
			t.Errorf("album archive missing %q; has %v", want, entryNames(entries))
		}
	}
	if _, ok := entries["arch.jpg"]; ok {
		t.Error("archived album member was included, want excluded")
	}
}

// readOriginal returns the stored original bytes of a seeded photo, for
// comparing against a ZIP entry.
func (e *env) readOriginal(t *testing.T, p photos.Photo) []byte {
	t.Helper()
	rc, err := e.fs.Open(t.Context(), p.FilePath)
	if err != nil {
		t.Fatalf("open original %s: %v", p.FilePath, err)
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read original %s: %v", p.FilePath, err)
	}
	return data
}

// entryNames returns the sorted-ish set of entry names for a readable failure
// message.
func entryNames(entries map[string][]byte) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	return names
}
