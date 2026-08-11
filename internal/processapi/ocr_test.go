package processapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// fakeOCRBackfiller records how it was called and returns a fixed count.
type fakeOCRBackfiller struct {
	enqueued int
	err      error
	calls    int
	lastAll  bool
}

// BackfillOCR records the call and reports the configured outcome.
func (f *fakeOCRBackfiller) BackfillOCR(_ context.Context, all bool) (int, error) {
	f.calls++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	return f.enqueued, nil
}

// newServerWithOCR mounts the API with the given OCR backfiller (the others
// stubbed) behind the given maintainer guard.
func newServerWithOCR(
	t *testing.T, ob OCRBackfiller, guard func(http.Handler) http.Handler,
) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		OCRBackfiller: ob, RequireMaintainer: guard,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestBackfillOCR_ok enqueues an `ocr` job per never-recognised photo and reports
// the count.
func TestBackfillOCR_ok(t *testing.T) {
	t.Parallel()

	ob := &fakeOCRBackfiller{enqueued: 3}
	srv := newServerWithOCR(t, ob, passthrough)

	resp := postProcess(t, srv.URL+"/process/ocr")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 3 {
		t.Errorf("enqueued = %d, want 3", body.Enqueued)
	}
	if ob.calls != 1 || ob.lastAll {
		t.Errorf("backfiller calls = %d, lastAll = %v, want 1 call with all=false", ob.calls, ob.lastAll)
	}
}

// TestBackfillOCR_all forwards ?all=true so every non-archived still is re-read —
// the forced full run that picks up a better recognition model.
func TestBackfillOCR_all(t *testing.T) {
	t.Parallel()

	ob := &fakeOCRBackfiller{enqueued: 20664}
	srv := newServerWithOCR(t, ob, passthrough)

	resp := postProcess(t, srv.URL+"/process/ocr?all=true")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !ob.lastAll {
		t.Error("lastAll = false, want the all flag forwarded")
	}
}

// TestBackfillOCR_idempotent asserts a second run over a drained library reports
// zero rather than failing.
func TestBackfillOCR_idempotent(t *testing.T) {
	t.Parallel()

	ob := &fakeOCRBackfiller{enqueued: 0}
	srv := newServerWithOCR(t, ob, passthrough)

	for range 2 {
		resp := postProcess(t, srv.URL+"/process/ocr")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	if ob.calls != 2 {
		t.Errorf("calls = %d, want 2", ob.calls)
	}
}

// TestBackfillOCR_unavailable answers 503 when text recognition is switched off,
// so the client learns the difference between "nothing to do" and "not running".
func TestBackfillOCR_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithOCR(t, nil, passthrough)

	resp := postProcess(t, srv.URL+"/process/ocr")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillOCR_error answers 500 without leaking the internal error.
func TestBackfillOCR_error(t *testing.T) {
	t.Parallel()

	ob := &fakeOCRBackfiller{err: errors.New("queue unreachable")}
	srv := newServerWithOCR(t, ob, passthrough)

	resp := postProcess(t, srv.URL+"/process/ocr")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "queue unreachable" {
		t.Error("response leaked the internal error")
	}
}

// TestBackfillOCR_forbidden asserts the endpoint is maintainer-only.
func TestBackfillOCR_forbidden(t *testing.T) {
	t.Parallel()

	ob := &fakeOCRBackfiller{}
	srv := newServerWithOCR(t, ob, forbid)

	resp := postProcess(t, srv.URL+"/process/ocr")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if ob.calls != 0 {
		t.Errorf("backfiller called %d times behind a refusing guard, want 0", ob.calls)
	}
}
