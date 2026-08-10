//go:build integration

package photoapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// storyboardBody is the wire shape of the storyboard status endpoint.
type storyboardBody struct {
	Status     string `json:"status"`
	Columns    int    `json:"columns"`
	Rows       int    `json:"rows"`
	Count      int    `json:"count"`
	TileWidth  int    `json:"tile_width"`
	TileHeight int    `json:"tile_height"`
	IntervalMs int    `json:"interval_ms"`
}

// seedTimedVideo catalogues a video with a known duration and dimensions, which
// is what makes it plannable into a storyboard.
func (e *env) seedTimedVideo(t *testing.T, name string, durationMs int) photos.Photo {
	t.Helper()
	stored, err := e.fs.Store(t.Context(), bytes.NewReader([]byte("not-a-real-clip")), time.Time{}, name)
	if err != nil {
		t.Fatalf("storage.Store(%s): %v", name, err)
	}
	created, err := e.store.Create(t.Context(), photos.Photo{
		FileHash:      stored.Hash,
		FilePath:      stored.RelPath,
		FileName:      name,
		FileSize:      stored.Size,
		FileMime:      "video/mp4",
		FileWidth:     1920,
		FileHeight:    1080,
		MediaType:     photos.MediaVideo,
		DurationMs:    &durationMs,
		TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("store.Create(%s): %v", name, err)
	}
	return created
}

// plantSprite writes a sprite into the generator's cache for the photo's hash,
// standing in for a completed storyboard job.
func (e *env) plantSprite(t *testing.T, photo photos.Photo, data []byte) {
	t.Helper()
	abs, err := e.storyboards.Path(photo.FileHash)
	if err != nil {
		t.Fatalf("storyboard.Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// getStoryboard fetches the status endpoint and decodes its body.
func getStoryboard(t *testing.T, client *http.Client, url string) storyboardBody {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	var body storyboardBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestStoryboard_pendingThenReady walks the lazy-generation contract end to end:
// the first ask for a video with no sprite reports "not generated yet" and
// schedules exactly one job, asking again does not schedule a second, the sprite
// route is a 404 throughout, and once the sprite lands the status turns ready with
// a layout and the bytes are served.
func TestStoryboard_pendingThenReady(t *testing.T) {
	env := newEnv(t)
	// A viewer, deliberately: whoever may watch the video may see its scrub
	// previews, and the status endpoint must not be write-gated.
	client, token := env.login(t, "viewer", auth.RoleViewer)
	video := env.seedTimedVideo(t, "clip.mp4", 20000)
	statusURL := env.server.URL + "/api/v1/photos/" + video.UID + "/storyboard"
	spriteURL := statusURL + "/sprite?t=" + token

	body := getStoryboard(t, client, statusURL)
	if body.Status != "pending" {
		t.Fatalf("first status = %q, want pending", body.Status)
	}
	if body.Count != 0 || body.IntervalMs != 0 {
		t.Errorf("pending body = %+v, want no geometry", body)
	}
	if got := countStoryboardJobs(t, env); got != 1 {
		t.Fatalf("queued storyboard jobs = %d, want 1", got)
	}

	// The sprite does not exist yet: the player must get a clean 404, not a 500.
	resp := mustDo(t, client, http.MethodGet, spriteURL, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("sprite before generation = %d, want 404", resp.StatusCode)
	}

	// Asking again while the job is queued must be absorbed by the dedup index.
	if body := getStoryboard(t, client, statusURL); body.Status != "pending" {
		t.Fatalf("second status = %q, want pending", body.Status)
	}
	if got := countStoryboardJobs(t, env); got != 1 {
		t.Errorf("queued storyboard jobs after a second ask = %d, want 1", got)
	}

	env.plantSprite(t, video, []byte("sprite-bytes"))

	ready := getStoryboard(t, client, statusURL)
	if ready.Status != "ready" {
		t.Fatalf("status after generation = %q, want ready", ready.Status)
	}
	if ready.Columns*ready.Rows != ready.Count || ready.Count == 0 {
		t.Errorf("grid = %dx%d/%d, want a full non-empty grid", ready.Columns, ready.Rows, ready.Count)
	}
	if ready.IntervalMs <= 0 || ready.TileWidth <= 0 || ready.TileHeight <= 0 {
		t.Errorf("geometry = %+v, want positive tile size and interval", ready)
	}

	resp = mustDo(t, client, http.MethodGet, spriteURL, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sprite after generation = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	if got := resp.Header.Get("ETag"); got != `"`+video.FileHash+`-sb"` {
		t.Errorf("ETag = %q, want the content-derived tag", got)
	}
}

// TestStoryboard_unavailableForStills verifies a photo that can never have a
// storyboard says so and schedules nothing — the guarantee that this feature does
// not quietly enqueue a job per photo in the library.
func TestStoryboard_unavailableForStills(t *testing.T) {
	env := newEnv(t)
	client, token := env.login(t, "viewer", auth.RoleViewer)
	still := env.seedPhoto(t, photos.Photo{TakenAtSource: "unknown"}, "still.jpg", 10, 20, 30)

	body := getStoryboard(t, client, env.server.URL+"/api/v1/photos/"+still.UID+"/storyboard")
	if body.Status != "unavailable" {
		t.Errorf("status = %q, want unavailable", body.Status)
	}
	if got := countStoryboardJobs(t, env); got != 0 {
		t.Errorf("queued storyboard jobs = %d, want 0 for a still", got)
	}

	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+still.UID+"/storyboard/sprite?t="+token, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("sprite for a still = %d, want 404", resp.StatusCode)
	}
}

// TestStoryboard_missingPhoto verifies an unknown uid is a 404 on both routes
// rather than a state the client would act on.
func TestStoryboard_missingPhoto(t *testing.T) {
	env := newEnv(t)
	client, token := env.login(t, "viewer", auth.RoleViewer)

	for _, path := range []string{"/storyboard", "/storyboard/sprite?t=" + token} {
		resp := mustDo(t, client, http.MethodGet, env.server.URL+"/api/v1/photos/ph_missing"+path, nil)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestStoryboard_spriteRequiresAuthorization verifies the sprite is media: an
// anonymous fetch with neither a session nor a download token is refused, so a
// storyboard never leaks frames of a photo the caller may not see.
func TestStoryboard_spriteRequiresAuthorization(t *testing.T) {
	env := newEnv(t)
	video := env.seedTimedVideo(t, "clip.mp4", 20000)
	env.plantSprite(t, video, []byte("sprite-bytes"))

	resp := mustDo(t, &http.Client{}, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+video.UID+"/storyboard/sprite", nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous sprite fetch = %d, want 401", resp.StatusCode)
	}
}

// countStoryboardJobs returns how many storyboard jobs the queue currently holds
// in a runnable state.
func countStoryboardJobs(t *testing.T, env *env) int {
	t.Helper()
	count, err := env.jobs.CountPending(t.Context(), jobs.TypeStoryboard)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	return count
}
