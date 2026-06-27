package importapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/jobs"
)

// fakeQueue is a Queue whose Enqueue returns a fixed job or error.
type fakeQueue struct {
	job  jobs.Job
	err  error
	last string
}

// Enqueue records the job type and returns the configured result.
func (q *fakeQueue) Enqueue(
	_ context.Context, jobType string, _ json.RawMessage, _ jobs.EnqueueOptions,
) (jobs.Job, error) {
	q.last = jobType
	return q.job, q.err
}

// newServer mounts the import API with a pass-through admin guard over a fresh
// chi router, returning a test server.
func newServer(t *testing.T, q Queue) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Queue: q, RequireAdmin: passthrough})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// passthrough is an admin guard that allows every request.
func passthrough(next http.Handler) http.Handler { return next }

// post issues a POST to the server path and returns the response.
func post(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

// TestImportPhotoPrism_enqueued verifies a queued job yields 202 with its id and
// uses the pp_import job type.
func TestImportPhotoPrism_enqueued(t *testing.T) {
	t.Parallel()
	q := &fakeQueue{job: jobs.Job{ID: 7, State: jobs.StateQueued}}
	srv := newServer(t, q)
	resp := post(t, srv, "/import/photoprism")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if q.last != jobs.TypePPImport {
		t.Errorf("enqueued type = %q, want %q", q.last, jobs.TypePPImport)
	}
	var body importResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.JobID != 7 || body.Status != string(jobs.StateQueued) {
		t.Errorf("body = %+v, want job 7 queued", body)
	}
}

// TestImportPhotoPrism_conflict verifies an in-flight import (dedup collision)
// yields 409.
func TestImportPhotoPrism_conflict(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeQueue{err: jobs.ErrDuplicate})
	resp := post(t, srv, "/import/photoprism")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// TestImportPhotoPrism_error verifies an unexpected enqueue failure yields 500.
func TestImportPhotoPrism_error(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeQueue{err: errors.New("boom")})
	resp := post(t, srv, "/import/photoprism")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestNewAPI_panicsOnNilQueue verifies a missing queue is a startup panic.
func TestNewAPI_panicsOnNilQueue(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewAPI did not panic on nil queue")
		}
	}()
	_ = NewAPI(Config{RequireAdmin: passthrough})
}
