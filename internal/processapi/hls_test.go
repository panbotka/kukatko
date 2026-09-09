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

// fakeHLSBackfiller records how it was called and returns a fixed count.
type fakeHLSBackfiller struct {
	enqueued int
	err      error
	calls    int
	lastAll  bool
}

// BackfillHLS records the call and reports the configured outcome.
func (f *fakeHLSBackfiller) BackfillHLS(_ context.Context, all bool) (int, error) {
	f.calls++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	return f.enqueued, nil
}

// newServerWithHLS mounts the API with the given HLS backfiller (the others
// stubbed) behind the given maintainer guard.
func newServerWithHLS(
	t *testing.T, hb HLSBackfiller, guard func(http.Handler) http.Handler,
) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		HLSBackfiller: hb, RequireMaintainer: guard,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestBackfillHLS_ok schedules an `hls_transcode` job per never-encoded video and
// reports the count.
func TestBackfillHLS_ok(t *testing.T) {
	t.Parallel()

	hb := &fakeHLSBackfiller{enqueued: 4}
	srv := newServerWithHLS(t, hb, passthrough)

	resp := postProcess(t, srv.URL+"/process/hls")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 4 {
		t.Errorf("enqueued = %d, want 4", body.Enqueued)
	}
	if hb.calls != 1 || hb.lastAll {
		t.Errorf("backfiller calls = %d, lastAll = %v, want 1 call with all=false", hb.calls, hb.lastAll)
	}
}

// TestBackfillHLS_all forwards ?all=true so every non-archived video is
// re-encoded — how a library picks up a newly enabled quality level.
func TestBackfillHLS_all(t *testing.T) {
	t.Parallel()

	hb := &fakeHLSBackfiller{enqueued: 112}
	srv := newServerWithHLS(t, hb, passthrough)

	resp := postProcess(t, srv.URL+"/process/hls?all=true")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !hb.lastAll {
		t.Error("lastAll = false, want the all flag forwarded")
	}
}

// TestBackfillHLS_unavailable answers 503 when streaming is switched off, so the
// client learns the difference between "nothing to do" and "not running".
func TestBackfillHLS_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithHLS(t, nil, passthrough)

	resp := postProcess(t, srv.URL+"/process/hls")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillHLS_error answers 500 without leaking the internal error.
func TestBackfillHLS_error(t *testing.T) {
	t.Parallel()

	hb := &fakeHLSBackfiller{err: errors.New("queue unreachable")}
	srv := newServerWithHLS(t, hb, passthrough)

	resp := postProcess(t, srv.URL+"/process/hls")
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

// TestBackfillHLS_forbidden asserts the endpoint is maintainer-only.
func TestBackfillHLS_forbidden(t *testing.T) {
	t.Parallel()

	hb := &fakeHLSBackfiller{}
	srv := newServerWithHLS(t, hb, forbid)

	resp := postProcess(t, srv.URL+"/process/hls")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if hb.calls != 0 {
		t.Errorf("backfiller called %d times behind a refusing guard, want 0", hb.calls)
	}
}
