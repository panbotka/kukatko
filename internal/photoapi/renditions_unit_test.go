package photoapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/storyboardjob"
)

// TestRenditionViews_dropsThePlaylist verifies the projection carries everything
// an operator asks about a rendition — its picture, its bitrate, its segments and
// when it was made — and deliberately leaves the playlist text behind, which is
// the largest column of the row and is served by its own route.
func TestRenditionViews_dropsThePlaylist(t *testing.T) {
	t.Parallel()

	encodedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	views := renditionViews([]hlsjob.Encoded{{
		PhotoUID: "pht1", Rendition: "1080p", Playlist: "#EXTM3U\n",
		Width: 1920, Height: 1080, Bandwidth: 5_128_000,
		Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 7, DurationMs: 42_000,
		EncodedAt: encodedAt,
	}})

	if len(views) != 1 {
		t.Fatalf("views = %d, want 1", len(views))
	}
	want := renditionView{
		Rendition: "1080p", Width: 1920, Height: 1080, Bandwidth: 5_128_000,
		Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 7, DurationMs: 42_000,
		EncodedAt: encodedAt,
	}
	if views[0] != want {
		t.Errorf("view = %+v, want %+v", views[0], want)
	}
	raw, err := json.Marshal(views[0])
	if err != nil {
		t.Fatalf("marshalling the view: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshalling the view: %v", err)
	}
	if _, ok := fields["playlist"]; ok {
		t.Errorf("the rendition view carries the playlist text: %s", raw)
	}
}

// TestRenditionViews_emptyIsAList verifies a video with nothing encoded projects
// onto an empty list rather than a null, so a client can iterate the answer
// without a nil check and "no renditions" never reads as "no answer".
func TestRenditionViews_emptyIsAList(t *testing.T) {
	t.Parallel()

	raw, err := json.Marshal(renditionsResponse{Renditions: renditionViews(nil)})
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if string(raw) != `{"renditions":[]}` {
		t.Errorf("body = %s, want an empty list", raw)
	}
}

// storyboardRebuildRouter mounts the forced rebuild without any auth middleware,
// so the handler's own answers can be exercised directly.
func storyboardRebuildRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Post("/photos/{uid}/regenerate-storyboard", api.handleRegenerateStoryboard)
	return r
}

// TestHandleRegenerateStoryboard_schedules verifies the happy path: the service is
// asked to re-render exactly the photo named in the path, and the answer says the
// work is queued — in the processing report's vocabulary, where "pending" would
// mean the opposite, that nothing was scheduled at all.
func TestHandleRegenerateStoryboard_schedules(t *testing.T) {
	t.Parallel()

	fake := &fakeStoryboards{}
	api := NewAPI(Config{Storyboards: fake})
	rec := httptest.NewRecorder()
	storyboardRebuildRouter(api).ServeHTTP(rec,
		httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/photos/pht1/regenerate-storyboard", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	var body storyboardRebuildResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the body: %v", err)
	}
	if body.Step != storyboardStep || body.State != string(processing.StateQueued) {
		t.Errorf("body = %+v, want the storyboard step queued", body)
	}
	if len(fake.regenUIDs) != 1 || fake.regenUIDs[0] != "pht1" {
		t.Errorf("regenerated = %v, want [pht1]", fake.regenUIDs)
	}
}

// TestHandleRegenerateStoryboard_refusals verifies each refusal reaches the caller
// as the status code that says what to do about it: retry a different photo (404),
// stop asking for this one (409), or come back to a differently-configured
// instance (503).
func TestHandleRegenerateStoryboard_refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "unknown photo", err: photos.ErrPhotoNotFound, want: http.StatusNotFound},
		{name: "a still", err: storyboardjob.ErrNotAVideo, want: http.StatusConflict},
		{name: "unknown length", err: storyboard.ErrNoDuration, want: http.StatusConflict},
		{name: "nothing can render", err: storyboardjob.ErrCannotRender, want: http.StatusServiceUnavailable},
		{name: "anything else", err: errors.New("boom"), want: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := NewAPI(Config{Storyboards: &fakeStoryboards{regenErr: tt.err}})
			rec := httptest.NewRecorder()
			storyboardRebuildRouter(api).ServeHTTP(rec,
				httptest.NewRequestWithContext(t.Context(), http.MethodPost,
					"/photos/pht1/regenerate-storyboard", nil))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
		})
	}
}

// TestHandleRegenerateStoryboard_unwired verifies an instance with no storyboard
// service answers 503 rather than panicking on the nil.
func TestHandleRegenerateStoryboard_unwired(t *testing.T) {
	t.Parallel()

	api := NewAPI(Config{})
	rec := httptest.NewRecorder()
	storyboardRebuildRouter(api).ServeHTTP(rec,
		httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/photos/pht1/regenerate-storyboard", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// compile-time assurance the fake still satisfies the interface it stands in for.
var _ StoryboardService = (*fakeStoryboards)(nil)
