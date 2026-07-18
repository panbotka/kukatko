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

// fakeSidecarBackfiller records how it was called and returns a fixed count.
type fakeSidecarBackfiller struct {
	enqueued int
	err      error
	calls    int
	lastAll  bool
}

// BackfillSidecars records the call and reports the configured outcome.
func (f *fakeSidecarBackfiller) BackfillSidecars(_ context.Context, all bool) (int, error) {
	f.calls++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	return f.enqueued, nil
}

// newServerWithSidecars mounts the API with the given sidecar backfiller (the
// others stubbed) behind the given admin guard.
func newServerWithSidecars(
	t *testing.T, sb SidecarBackfiller, guard func(http.Handler) http.Handler,
) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		SidecarBackfiller: sb, RequireMaintainer: guard,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestBackfillSidecars_ok enqueues a sidecar job per pending photo and reports
// the count.
func TestBackfillSidecars_ok(t *testing.T) {
	t.Parallel()

	sb := &fakeSidecarBackfiller{enqueued: 2}
	srv := newServerWithSidecars(t, sb, passthrough)

	resp := postProcess(t, srv.URL+"/process/sidecars")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 2 {
		t.Errorf("enqueued = %d, want 2", body.Enqueued)
	}
	if sb.calls != 1 || sb.lastAll {
		t.Errorf("backfiller calls = %d, lastAll = %v, want 1 call with all=false", sb.calls, sb.lastAll)
	}
}

// TestBackfillSidecars_all forwards ?all=true so every non-archived photo is
// rewritten — the forced full run that recovers curation which changed without
// touching the photo row.
func TestBackfillSidecars_all(t *testing.T) {
	t.Parallel()

	sb := &fakeSidecarBackfiller{enqueued: 9}
	srv := newServerWithSidecars(t, sb, passthrough)

	resp := postProcess(t, srv.URL+"/process/sidecars?all=true")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !sb.lastAll {
		t.Error("lastAll = false, want the all flag forwarded")
	}
}

// TestBackfillSidecars_idempotent asserts a second run over a drained library
// reports zero rather than failing — the property that makes it safe from cron
// and before every risky operation.
func TestBackfillSidecars_idempotent(t *testing.T) {
	t.Parallel()

	sb := &fakeSidecarBackfiller{enqueued: 0}
	srv := newServerWithSidecars(t, sb, passthrough)

	for range 2 {
		resp := postProcess(t, srv.URL+"/process/sidecars")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	if sb.calls != 2 {
		t.Errorf("calls = %d, want 2", sb.calls)
	}
}

// TestBackfillSidecars_unavailable answers 503 when the export is switched off,
// so the client learns the difference between "nothing to do" and "not running".
func TestBackfillSidecars_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithSidecars(t, nil, passthrough)

	resp := postProcess(t, srv.URL+"/process/sidecars")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillSidecars_error answers 500 without leaking the internal error.
func TestBackfillSidecars_error(t *testing.T) {
	t.Parallel()

	sb := &fakeSidecarBackfiller{err: errors.New("bucket unreachable")}
	srv := newServerWithSidecars(t, sb, passthrough)

	resp := postProcess(t, srv.URL+"/process/sidecars")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body errorBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "bucket unreachable" {
		t.Error("response leaked the internal error")
	}
}

// TestBackfillSidecars_forbidden asserts the endpoint is admin-only.
func TestBackfillSidecars_forbidden(t *testing.T) {
	t.Parallel()

	sb := &fakeSidecarBackfiller{}
	srv := newServerWithSidecars(t, sb, forbid)

	resp := postProcess(t, srv.URL+"/process/sidecars")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if sb.calls != 0 {
		t.Errorf("backfiller called %d times behind a refusing guard, want 0", sb.calls)
	}
}
