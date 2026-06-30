//go:build integration

package photoapi_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// seedVideo stores raw bytes as a video original and catalogues a video photo
// with a primary file, returning the created photo and the exact stored bytes so
// a range test can compare byte ranges.
func (e *env) seedVideo(t *testing.T, name string, data []byte) photos.Photo {
	t.Helper()
	stored, err := e.fs.Store(t.Context(), bytes.NewReader(data), time.Time{}, name)
	if err != nil {
		t.Fatalf("storage.Store(%s): %v", name, err)
	}
	created, err := e.store.Create(t.Context(), photos.Photo{
		FileHash:      stored.Hash,
		FilePath:      stored.RelPath,
		FileName:      name,
		FileSize:      stored.Size,
		FileMime:      "video/mp4",
		MediaType:     photos.MediaVideo,
		VideoCodec:    "h264",
		TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("store.Create(%s): %v", name, err)
	}
	if _, err := e.store.CreateFile(t.Context(), photos.PhotoFile{
		PhotoUID: created.UID, FilePath: created.FilePath, FileHash: created.FileHash,
		FileSize: created.FileSize, FileMime: "video/mp4", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("store.CreateFile(%s): %v", name, err)
	}
	return created
}

// TestVideoStream_rangeRequest verifies the video endpoint answers a Range
// request with 206 Partial Content carrying exactly the requested byte range,
// advertises Accept-Ranges, and serves the whole file on an unranged request.
func TestVideoStream_rangeRequest(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	// A payload several KiB long so a sub-range is a meaningful slice.
	data := make([]byte, 8192)
	for i := range data {
		data[i] = byte(i % 251)
	}
	video := env.seedVideo(t, "clip.mp4", data)
	url := env.server.URL + "/api/v1/photos/" + video.UID + "/video"

	// Ranged fetch: bytes 1000-1999 inclusive (1000 bytes).
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Range", "bytes=1000-1999")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET range: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	wantCR := fmt.Sprintf("bytes 1000-1999/%d", len(data))
	if got := resp.Header.Get("Content-Range"); got != wantCR {
		t.Errorf("Content-Range = %q, want %q", got, wantCR)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if !bytes.Equal(body, data[1000:2000]) {
		t.Errorf("range body mismatch: got %d bytes, want the 1000-1999 slice", len(body))
	}
}

// TestVideoStream_fullAndMime verifies an unranged request streams the whole
// clip with the right content type and advertises range support.
func TestVideoStream_fullAndMime(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	data := bytes.Repeat([]byte("kukatko-video"), 64)
	video := env.seedVideo(t, "movie.mp4", data)
	url := env.server.URL + "/api/v1/photos/" + video.UID + "/video"

	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "video/mp4" {
		t.Errorf("Content-Type = %q, want video/mp4", got)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if !bytes.Equal(body, data) {
		t.Errorf("body mismatch: got %d bytes, want %d", len(body), len(data))
	}
}

// TestVideoStream_notAVideo verifies the endpoint 404s for a still image, which
// has no playable video.
func TestVideoStream_notAVideo(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)

	image := env.seedPhoto(t, photos.Photo{Title: "still", TakenAtSource: "unknown"}, "still.jpg", 9, 9, 9)
	url := env.server.URL + "/api/v1/photos/" + image.UID + "/video"

	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a non-video", resp.StatusCode)
	}
}
