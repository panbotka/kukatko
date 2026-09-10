//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// renditionsBody is the wire shape of the rendition-inventory endpoint.
type renditionsBody struct {
	Renditions []struct {
		Rendition    string    `json:"rendition"`
		Width        int       `json:"width"`
		Height       int       `json:"height"`
		Bandwidth    int       `json:"bandwidth"`
		Codecs       string    `json:"codecs"`
		SegmentCount int       `json:"segment_count"`
		DurationMs   int       `json:"duration_ms"`
		EncodedAt    time.Time `json:"encoded_at"`
	} `json:"renditions"`
}

// renditionsURL builds the address of the inventory endpoint for one photo.
func renditionsURL(e *env, uid string) string {
	return e.server.URL + "/api/v1/photos/" + uid + "/renditions"
}

// getRenditions fetches the inventory and decodes it, failing on anything but 200.
func getRenditions(t *testing.T, client *http.Client, url string) renditionsBody {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	var body renditionsBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return body
}

// TestRenditions_reportsWhatWasEncoded verifies the inventory answers with the
// recorded rendition's own numbers — its picture, its bitrate, its segments, its
// measured length and when it was produced — which is what nothing else exposes.
// A viewer may read it: it says no more than the master playlist they are already
// allowed to play.
func TestRenditions_reportsWhatWasEncoded(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	before := time.Now().Add(-time.Minute)
	video := seedRendition(t, env, nil, env.seedVideo(t, "clip.mp4", []byte("mp4-bytes")))

	body := getRenditions(t, client, renditionsURL(env, video.UID))
	if len(body.Renditions) != 1 {
		t.Fatalf("renditions = %d, want 1", len(body.Renditions))
	}
	got := body.Renditions[0]
	if got.Rendition != "1080p" || got.Width != 1920 || got.Height != 1080 {
		t.Errorf("rendition = %+v, want the 1080p picture", got)
	}
	if got.Bandwidth != 5_128_000 || got.SegmentCount != 2 || got.DurationMs != 10_000 {
		t.Errorf("rendition = %+v, want the recorded bitrate, segments and length", got)
	}
	if got.Codecs != "avc1.640028,mp4a.40.2" {
		t.Errorf("codecs = %q, want the recorded codecs string", got.Codecs)
	}
	if !got.EncodedAt.After(before) {
		t.Errorf("encoded_at = %v, want a stamp from this test run", got.EncodedAt)
	}
}

// TestRenditions_emptyAndMissing covers the two answers that are not an
// inventory: a photo with nothing encoded — a still, or a video the encoder has
// not reached — answers an empty list, while a uid that names no photo at all is a
// 404, so "not encoded" is never confused with "not there".
func TestRenditions_emptyAndMissing(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "viewer", auth.RoleViewer)
	still := env.seedPhoto(t, photos.Photo{Title: "still", TakenAtSource: "unknown"}, "still.jpg", 9, 9, 9)
	unencoded := env.seedVideo(t, "pending.mp4", []byte("not-encoded-yet"))

	for _, uid := range []string{still.UID, unencoded.UID} {
		if body := getRenditions(t, client, renditionsURL(env, uid)); len(body.Renditions) != 0 {
			t.Errorf("renditions of %s = %d, want none", uid, len(body.Renditions))
		}
	}

	resp := mustDo(t, client, http.MethodGet, renditionsURL(env, "ptnosuchphotoatall000000000000000"), nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("renditions of an unknown photo = %d, want 404", resp.StatusCode)
	}
}

// TestRegenerateStoryboard_discardsAndSchedules walks the forced rebuild end to
// end: a video whose sprite is already cached is not left alone (which is what
// re-running the ordinary job would do) — the sprite is deleted and a fresh render
// queued, and the status endpoint immediately reports the clip as pending again.
func TestRegenerateStoryboard_discardsAndSchedules(t *testing.T) {
	env := newEnv(t)
	maintainer, _ := env.login(t, "boss", auth.RoleMaintainer)
	viewer, _ := env.login(t, "guest", auth.RoleViewer)
	video := env.seedTimedVideo(t, "clip.mp4", 20000)
	env.plantSprite(t, video, []byte("stale-sprite-bytes"))

	statusURL := env.server.URL + "/api/v1/photos/" + video.UID + "/storyboard"
	if body := getStoryboard(t, viewer, statusURL); body.Status != "ready" {
		t.Fatalf("status before the rebuild = %q, want ready", body.Status)
	}

	rebuildURL := env.server.URL + "/api/v1/photos/" + video.UID + "/regenerate-storyboard"
	resp := mustDo(t, maintainer, http.MethodPost, rebuildURL, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rebuild status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Step  string `json:"step"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Step != "storyboard" || body.State != "queued" {
		t.Errorf("body = %+v, want the storyboard step queued", body)
	}

	abs, err := env.storyboards.Path(video.FileHash)
	if err != nil {
		t.Fatalf("storyboard.Path: %v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("the stale sprite survived the rebuild (stat err = %v)", err)
	}
	if got := countStoryboardJobs(t, env); got != 1 {
		t.Errorf("queued storyboard jobs = %d, want 1", got)
	}
}

// TestRegenerateStoryboard_guardAndRefusals verifies the endpoint is maintainer
// work — scheduling a full decode of a clip is operations, not curation — and that
// a photo which can never have a preview is refused with 409 rather than quietly
// queued.
func TestRegenerateStoryboard_guardAndRefusals(t *testing.T) {
	env := newEnv(t)
	editor, _ := env.login(t, "editor", auth.RoleEditor)
	maintainer, _ := env.login(t, "boss", auth.RoleMaintainer)
	video := env.seedTimedVideo(t, "clip.mp4", 20000)
	still := env.seedPhoto(t, photos.Photo{Title: "still", TakenAtSource: "unknown"}, "still.jpg", 9, 9, 9)

	url := env.server.URL + "/api/v1/photos/" + video.UID + "/regenerate-storyboard"
	resp := mustDo(t, editor, http.MethodPost, url, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("editor rebuild = %d, want 403", resp.StatusCode)
	}

	stillURL := env.server.URL + "/api/v1/photos/" + still.UID + "/regenerate-storyboard"
	resp = mustDo(t, maintainer, http.MethodPost, stillURL, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("rebuild of a still = %d, want 409", resp.StatusCode)
	}

	missingURL := env.server.URL + "/api/v1/photos/ptnosuchphotoatall000000000000000/regenerate-storyboard"
	resp = mustDo(t, maintainer, http.MethodPost, missingURL, nil)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("rebuild of an unknown photo = %d, want 404", resp.StatusCode)
	}
}
