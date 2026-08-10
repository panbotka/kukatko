package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storyboard"
	"github.com/panbotka/kukatko/internal/storyboardjob"
)

// fakeStoryboards is a controllable StoryboardService for the handler tests.
type fakeStoryboards struct {
	status     storyboardjob.Status
	statusErr  error
	spriteBody string
	openErr    error
	hash       string
	hashErr    error
	statusUID  string
	openCalls  int
}

// Status records the uid and returns the configured answer.
func (f *fakeStoryboards) Status(_ context.Context, uid string) (storyboardjob.Status, error) {
	f.statusUID = uid
	return f.status, f.statusErr
}

// Open returns the configured sprite bytes or failure.
func (f *fakeStoryboards) Open(context.Context, string) (io.ReadCloser, storyboard.Spec, error) {
	f.openCalls++
	if f.openErr != nil {
		return nil, storyboard.Spec{}, f.openErr
	}
	return io.NopCloser(strings.NewReader(f.spriteBody)), f.status.Spec, nil
}

// FileHash returns the configured ETag source.
func (f *fakeStoryboards) FileHash(context.Context, string) (string, error) {
	return f.hash, f.hashErr
}

// storyboardRouter mounts only the two storyboard handlers (no auth middleware)
// so the handler logic can be exercised directly.
func storyboardRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Get("/photos/{uid}/storyboard", api.handleStoryboard)
	r.Get("/photos/{uid}/storyboard/sprite", api.handleStoryboardSprite)
	return r
}

// readySpec is a plausible layout for a twenty-second 16:9 clip.
var readySpec = storyboard.Spec{
	Columns: 10, Rows: 1, Count: 10, TileWidth: 160, TileHeight: 90, IntervalMs: 2000,
}

// TestHandleStoryboard_states verifies the three states the player branches on
// all answer 200 with the state in the body, and that only a ready storyboard
// carries a layout — a pending one must not hand the client a zero grid to place
// previews against.
func TestHandleStoryboard_states(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     storyboardjob.Status
		wantStatus string
		wantGrid   bool
	}{
		{
			name:       "not generated yet",
			status:     storyboardjob.Status{State: storyboardjob.StatePending},
			wantStatus: "pending",
		},
		{
			name:       "never available",
			status:     storyboardjob.Status{State: storyboardjob.StateUnavailable},
			wantStatus: "unavailable",
		},
		{
			name:       "ready",
			status:     storyboardjob.Status{State: storyboardjob.StateReady, Spec: readySpec},
			wantStatus: "ready",
			wantGrid:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeStoryboards{status: tt.status}
			rec := httptest.NewRecorder()
			storyboardRouter(&API{storyboards: fake}).
				ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard"))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var body storyboardResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", body.Status, tt.wantStatus)
			}
			if fake.statusUID != "ph_1" {
				t.Errorf("asked about %q, want ph_1", fake.statusUID)
			}
			if tt.wantGrid {
				if body.Columns != 10 || body.Rows != 1 || body.Count != 10 {
					t.Errorf("grid = %dx%d/%d, want 10x1/10", body.Columns, body.Rows, body.Count)
				}
				if body.IntervalMs != 2000 || body.TileWidth != 160 || body.TileHeight != 90 {
					t.Errorf("geometry = %+v, want the ready spec", body)
				}
				return
			}
			if body.Columns != 0 || body.Count != 0 || body.IntervalMs != 0 {
				t.Errorf("body = %+v, want no geometry while %s", body, tt.wantStatus)
			}
		})
	}
}

// TestHandleStoryboard_noService verifies an instance with no storyboard wiring
// reports "unavailable" rather than 503: the player treats it as "no preview",
// and a video with no scrub thumbnails is not a broken page.
func TestHandleStoryboard_noService(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	storyboardRouter(&API{}).ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body storyboardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q, want unavailable", body.Status)
	}
}

// TestHandleStoryboard_errors verifies a missing photo is a 404 and an unexpected
// backend failure a 500.
func TestHandleStoryboard_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "missing photo", err: photos.ErrPhotoNotFound, wantStatus: http.StatusNotFound},
		{name: "backend failure", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			api := &API{storyboards: &fakeStoryboards{statusErr: tt.err}}
			storyboardRouter(api).ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard"))
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestHandleStoryboardSprite_streams verifies a rendered sprite is served as a
// cacheable JPEG carrying a content-derived ETag.
func TestHandleStoryboardSprite_streams(t *testing.T) {
	t.Parallel()

	fake := &fakeStoryboards{
		status:     storyboardjob.Status{State: storyboardjob.StateReady, Spec: readySpec},
		spriteBody: "jpeg-bytes",
		hash:       "abc123",
	}
	rec := httptest.NewRecorder()
	storyboardRouter(&API{storyboards: fake}).
		ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard/sprite"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "jpeg-bytes" {
		t.Errorf("body = %q, want the sprite bytes", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", got)
	}
	if got := rec.Header().Get("ETag"); got != `"abc123-sb"` {
		t.Errorf("ETag = %q, want the content-derived tag", got)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("Cache-Control = %q, want an immutable policy", rec.Header().Get("Cache-Control"))
	}
}

// TestHandleStoryboardSprite_notGenerated is the "not generated yet" case on the
// bytes route: every reason there is no sprite answers 404, which the player
// reads as "no preview" and nothing worse.
func TestHandleStoryboardSprite_notGenerated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "not generated yet", err: storyboard.ErrNotGenerated},
		{name: "no known duration", err: storyboard.ErrNoDuration},
		{name: "not a video", err: storyboardjob.ErrNotAVideo},
		{name: "missing photo", err: photos.ErrPhotoNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			api := &API{storyboards: &fakeStoryboards{openErr: tt.err}}
			storyboardRouter(api).ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard/sprite"))
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
		})
	}
}

// TestHandleStoryboardSprite_failure verifies an unexpected read failure is a 500
// rather than a silent empty image.
func TestHandleStoryboardSprite_failure(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	api := &API{storyboards: &fakeStoryboards{openErr: errors.New("disk on fire")}}
	storyboardRouter(api).ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard/sprite"))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestHandleStoryboardSprite_noService verifies an unwired instance answers 404
// and never touches a nil service.
func TestHandleStoryboardSprite_noService(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	storyboardRouter(&API{}).ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard/sprite"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestStoryboardETag_fallsBackToUID verifies the sprite still gets a stable
// validator when the content hash cannot be read, rather than no ETag at all.
func TestStoryboardETag_fallsBackToUID(t *testing.T) {
	t.Parallel()

	fake := &fakeStoryboards{
		status:     storyboardjob.Status{State: storyboardjob.StateReady, Spec: readySpec},
		spriteBody: "jpeg-bytes",
		hashErr:    errors.New("gone"),
	}
	rec := httptest.NewRecorder()
	storyboardRouter(&API{storyboards: fake}).
		ServeHTTP(rec, req(http.MethodGet, "/photos/ph_1/storyboard/sprite"))
	if got := rec.Header().Get("ETag"); got != `"ph_1-sb"` {
		t.Errorf("ETag = %q, want the uid fallback", got)
	}
}

// TestStoryboardRoutesAreGuarded verifies both routes sit behind a guard: the
// status route behind RequireAuth (a viewer watching a video must reach it) and
// the sprite route behind the media guard, which also accepts a download token.
func TestStoryboardRoutesAreGuarded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		api  *API
	}{
		{
			name: "status behind RequireAuth",
			path: "/photos/ph_1/storyboard",
			api: &API{
				requireAuth: regenDeny, requireWrite: regenPass,
				requireAdmin: regenPass, requireDownload: regenPass,
			},
		},
		{
			name: "sprite behind the media guard",
			path: "/photos/ph_1/storyboard/sprite",
			api: &API{
				requireAuth: regenPass, requireWrite: regenPass,
				requireAdmin: regenPass, requireDownload: regenDeny,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeStoryboards{status: storyboardjob.Status{State: storyboardjob.StateReady}}
			tt.api.storyboards = fake
			r := chi.NewRouter()
			tt.api.RegisterRoutes(r)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req(http.MethodGet, tt.path))

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", rec.Code)
			}
			if fake.statusUID != "" || fake.openCalls != 0 {
				t.Error("the handler ran despite the guard rejecting the request")
			}
		})
	}
}
