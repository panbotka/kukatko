package photoapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/embedding"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/worker"
)

// fakeReembedder is a controllable PhotoReembedder for the handler tests.
type fakeReembedder struct {
	err    error
	calls  int
	gotUID string
}

// ForceEmbed records the call and returns the configured error.
func (f *fakeReembedder) ForceEmbed(_ context.Context, uid string) error {
	f.calls++
	f.gotUID = uid
	return f.err
}

// fakeRedetector is a controllable PhotoRedetector for the handler tests.
type fakeRedetector struct {
	faces int
	err   error
	calls int
}

// ForceDetect records the call and returns the configured count/error.
func (f *fakeRedetector) ForceDetect(_ context.Context, _ string) (int, error) {
	f.calls++
	return f.faces, f.err
}

// fakeRegeocoder is a controllable PhotoRegeocoder for the handler tests.
type fakeRegeocoder struct {
	err   error
	calls int
}

// ForceGeocode records the call and returns the configured error.
func (f *fakeRegeocoder) ForceGeocode(_ context.Context, _ string) error {
	f.calls++
	return f.err
}

// fakeRebuildEnqueuer records the forced jobs a deferred rebuild schedules, and
// answers with the queue outcome the test is about — the empty value standing for
// the ordinary one, a job of its own.
type fakeRebuildEnqueuer struct {
	embeds  []string
	faces   []string
	places  []string
	err     error
	outcome jobs.ForceOutcome
}

// EnqueueImageEmbedRebuild records the uid or fails.
func (f *fakeRebuildEnqueuer) EnqueueImageEmbedRebuild(
	_ context.Context, uid string,
) (jobs.ForceOutcome, error) {
	return f.record(&f.embeds, uid)
}

// EnqueueFaceDetectRebuild records the uid or fails.
func (f *fakeRebuildEnqueuer) EnqueueFaceDetectRebuild(
	_ context.Context, uid string,
) (jobs.ForceOutcome, error) {
	return f.record(&f.faces, uid)
}

// EnqueuePlacesRebuild records the uid or fails.
func (f *fakeRebuildEnqueuer) EnqueuePlacesRebuild(
	_ context.Context, uid string,
) (jobs.ForceOutcome, error) {
	return f.record(&f.places, uid)
}

// record appends uid to the queue it belongs to and reports the configured
// outcome, or fails when the fake was given an error.
func (f *fakeRebuildEnqueuer) record(queue *[]string, uid string) (jobs.ForceOutcome, error) {
	if f.err != nil {
		return "", f.err
	}
	*queue = append(*queue, uid)
	if f.outcome == "" {
		return jobs.ForceScheduled, nil
	}
	return f.outcome, nil
}

// rebuildRouter mounts the three rebuild handlers without any guard, so the
// handler logic can be exercised directly.
func rebuildRouter(api *API) http.Handler {
	r := chi.NewRouter()
	r.Post("/photos/{uid}/reembed", api.handleReembed)
	r.Post("/photos/{uid}/redetect-faces", api.handleRedetectFaces)
	r.Post("/photos/{uid}/regeocode", api.handleRegeocode)
	return r
}

// postRebuild drives one rebuild endpoint and returns the recorder.
func postRebuild(t *testing.T, api *API, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/photos/ph1/"+path, nil)
	rebuildRouter(api).ServeHTTP(rec, req)
	return rec
}

// decodeRebuild decodes a rebuild response body.
func decodeRebuild(t *testing.T, rec *httptest.ResponseRecorder) rebuildResponse {
	t.Helper()
	var body rebuildResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding the rebuild response: %v", err)
	}
	return body
}

// TestHandleRebuild_notWired confirms each endpoint refuses with 503 when its
// service is missing, rather than pretending the work was scheduled.
func TestHandleRebuild_notWired(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"reembed", "redetect-faces", "regeocode"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			rec := postRebuild(t, &API{}, path)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

// TestHandleReembed_rebuilds proves the endpoint recomputes rather than schedules:
// the service is called once, and the answer says the work was done.
func TestHandleReembed_rebuilds(t *testing.T) {
	t.Parallel()

	svc := &fakeReembedder{}
	rec := postRebuild(t, &API{reembedder: svc}, "reembed")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.calls != 1 || svc.gotUID != "ph1" {
		t.Errorf("service calls = %d for %q, want 1 for ph1", svc.calls, svc.gotUID)
	}
	body := decodeRebuild(t, rec)
	if body.Status != RebuildStatusRebuilt || body.Step != "image_embed" {
		t.Errorf("body = %+v, want a rebuilt image_embed", body)
	}
	if body.Faces != nil {
		t.Errorf("body.Faces = %v, want omitted for an embedding rebuild", *body.Faces)
	}
}

// TestHandleRedetectFaces_reportsTheCount is the requirement that made the
// endpoint synchronous: after a re-detection the caller is told how many faces the
// photo has, including zero for a photo that turns out to hold none.
func TestHandleRedetectFaces_reportsTheCount(t *testing.T) {
	t.Parallel()

	for _, want := range []int{0, 3} {
		t.Run(map[bool]string{true: "no faces", false: "some faces"}[want == 0], func(t *testing.T) {
			t.Parallel()
			rec := postRebuild(t, &API{redetector: &fakeRedetector{faces: want}}, "redetect-faces")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			body := decodeRebuild(t, rec)
			if body.Faces == nil || *body.Faces != want {
				t.Errorf("body.Faces = %v, want %d", body.Faces, want)
			}
			if body.Step != "face_detect" || body.Status != RebuildStatusRebuilt {
				t.Errorf("body = %+v, want a rebuilt face_detect", body)
			}
		})
	}
}

// TestHandleRegeocode_rebuilds covers the third endpoint's happy path.
func TestHandleRegeocode_rebuilds(t *testing.T) {
	t.Parallel()

	svc := &fakeRegeocoder{}
	rec := postRebuild(t, &API{regeocoder: svc}, "regeocode")

	if rec.Code != http.StatusOK || svc.calls != 1 {
		t.Fatalf("status = %d after %d calls, want 200 after 1", rec.Code, svc.calls)
	}
	if body := decodeRebuild(t, rec); body.Step != "places" || body.Status != RebuildStatusRebuilt {
		t.Errorf("body = %+v, want a rebuilt places", body)
	}
}

// TestHandleRebuild_offlineQueues is the offline contract: a rebuild whose
// backing service is asleep must answer like the plain path — the forced job is
// queued and the caller is told so — rather than failing the request.
func TestHandleRebuild_offlineQueues(t *testing.T) {
	t.Parallel()

	offline := worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	tests := []struct {
		name  string
		path  string
		api   func(*fakeRebuildEnqueuer) *API
		queue func(*fakeRebuildEnqueuer) []string
	}{
		{
			name:  "embedding",
			path:  "reembed",
			api:   func(e *fakeRebuildEnqueuer) *API { return &API{reembedder: &fakeReembedder{err: offline}, rebuilds: e} },
			queue: func(e *fakeRebuildEnqueuer) []string { return e.embeds },
		},
		{
			name:  "faces",
			path:  "redetect-faces",
			api:   func(e *fakeRebuildEnqueuer) *API { return &API{redetector: &fakeRedetector{err: offline}, rebuilds: e} },
			queue: func(e *fakeRebuildEnqueuer) []string { return e.faces },
		},
		{
			name:  "place",
			path:  "regeocode",
			api:   func(e *fakeRebuildEnqueuer) *API { return &API{regeocoder: &fakeRegeocoder{err: offline}, rebuilds: e} },
			queue: func(e *fakeRebuildEnqueuer) []string { return e.places },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enq := &fakeRebuildEnqueuer{}
			rec := postRebuild(t, tt.api(enq), tt.path)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 — an offline service queues, it does not fail", rec.Code)
			}
			if body := decodeRebuild(t, rec); body.Status != RebuildStatusQueued {
				t.Errorf("status = %q, want %q", body.Status, RebuildStatusQueued)
			}
			if got := tt.queue(enq); len(got) != 1 || got[0] != "ph1" {
				t.Errorf("queued %v, want [ph1]", got)
			}
		})
	}
}

// TestHandleRebuild_offlineWithoutAQueue keeps the answer honest when there is no
// enqueuer to fall back to: the work is neither done nor scheduled, so the outage
// is reported rather than dressed up as a success.
func TestHandleRebuild_offlineWithoutAQueue(t *testing.T) {
	t.Parallel()

	offline := worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	rec := postRebuild(t, &API{reembedder: &fakeReembedder{err: offline}}, "reembed")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// TestHandleRebuild_errors maps the two failure shapes the endpoints share.
func TestHandleRebuild_errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "missing photo", err: photos.ErrPhotoNotFound, wantStatus: http.StatusNotFound},
		{name: "anything else", err: errors.New("boom"), wantStatus: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := postRebuild(t, &API{reembedder: &fakeReembedder{err: tt.err}}, "reembed")
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

// TestHandleRebuild_queueFailureIs500 covers the one case where the fallback
// itself breaks: the work was not done and could not be scheduled either.
func TestHandleRebuild_queueFailureIs500(t *testing.T) {
	t.Parallel()

	offline := worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	api := &API{
		reembedder: &fakeReembedder{err: offline},
		rebuilds:   &fakeRebuildEnqueuer{err: errors.New("queue is down")},
	}
	if rec := postRebuild(t, api, "reembed"); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
}

// TestHandleRebuild_inFlightIsAConflict is the answer that must not be 200: the
// forced job collided with a run already in flight, which holds the plain payload
// it was claimed with and will take its idempotent skip. Nothing was rebuilt and
// nothing is queued to rebuild it, so the endpoint says so and asks for a retry
// rather than reporting the same silent no-op it exists to kill.
func TestHandleRebuild_inFlightIsAConflict(t *testing.T) {
	t.Parallel()

	offline := worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	tests := []struct {
		name string
		path string
		api  func(*fakeRebuildEnqueuer) *API
	}{
		{
			name: "embedding",
			path: "reembed",
			api: func(e *fakeRebuildEnqueuer) *API {
				return &API{reembedder: &fakeReembedder{err: offline}, rebuilds: e}
			},
		},
		{
			name: "faces",
			path: "redetect-faces",
			api: func(e *fakeRebuildEnqueuer) *API {
				return &API{redetector: &fakeRedetector{err: offline}, rebuilds: e}
			},
		},
		{
			name: "place",
			path: "regeocode",
			api: func(e *fakeRebuildEnqueuer) *API {
				return &API{regeocoder: &fakeRegeocoder{err: offline}, rebuilds: e}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := postRebuild(t, tt.api(&fakeRebuildEnqueuer{outcome: jobs.ForceInFlight}), tt.path)
			if rec.Code != http.StatusConflict {
				t.Errorf("status = %d, want %d — a force that will not happen is not a success",
					rec.Code, http.StatusConflict)
			}
		})
	}
}

// TestHandleRebuild_resolvedCollisionsStillQueue keeps the other two collisions
// on the success side: a queued job upgraded to the forced payload and an
// already-forced job that absorbed the request both end in a forced job, which is
// what "queued" promises.
func TestHandleRebuild_resolvedCollisionsStillQueue(t *testing.T) {
	t.Parallel()

	offline := worker.RetryAfter(time.Minute, embedding.ErrUnavailable)
	for _, outcome := range []jobs.ForceOutcome{jobs.ForceScheduled, jobs.ForceUpgraded, jobs.ForceAbsorbed} {
		t.Run(string(outcome), func(t *testing.T) {
			t.Parallel()
			api := &API{
				reembedder: &fakeReembedder{err: offline},
				rebuilds:   &fakeRebuildEnqueuer{outcome: outcome},
			}
			rec := postRebuild(t, api, "reembed")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for a %s collision", rec.Code, outcome)
			}
			if body := decodeRebuild(t, rec); body.Status != RebuildStatusQueued {
				t.Errorf("status = %q, want %q", body.Status, RebuildStatusQueued)
			}
		})
	}
}
