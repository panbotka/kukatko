package importapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/importer"
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

// fakeRuns is a RunLister whose List returns a fixed page or error and records
// the paging it was called with.
type fakeRuns struct {
	runs      []importer.Run
	err       error
	gotLimit  int
	gotOffset int
	wasCalled bool
}

// List records the paging and returns the configured result.
func (f *fakeRuns) List(_ context.Context, limit, offset int) ([]importer.Run, error) {
	f.wasCalled = true
	f.gotLimit = limit
	f.gotOffset = offset
	return f.runs, f.err
}

// newServer mounts the import API (both triggers enabled) with a pass-through
// admin guard over a fresh chi router, returning a test server.
func newServer(t *testing.T, q Queue) *httptest.Server {
	t.Helper()
	return newServerWithRuns(t, q, &fakeRuns{})
}

// newServerWithRuns mounts the import API with the given queue and run lister
// (both triggers enabled) behind a pass-through admin guard.
func newServerWithRuns(t *testing.T, q Queue, runs RunLister) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Queue:             q,
		Runs:              runs,
		RequireAdmin:      passthrough,
		EnablePhotoPrism:  true,
		EnablePhotoSorter: true,
	})
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
	return do(t, srv, http.MethodPost, path)
}

// get issues a GET to the server path and returns the response.
func get(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	return do(t, srv, http.MethodGet, path)
}

// do issues a request with the given method to the server path and returns the
// response.
func do(t *testing.T, srv *httptest.Server, method, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
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

// TestImportPhotoSorter_enqueued verifies a queued migration yields 202 with its
// id and uses the ps_migrate job type.
func TestImportPhotoSorter_enqueued(t *testing.T) {
	t.Parallel()
	q := &fakeQueue{job: jobs.Job{ID: 9, State: jobs.StateQueued}}
	srv := newServer(t, q)
	resp := post(t, srv, "/import/photosorter")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if q.last != jobs.TypePSMigrate {
		t.Errorf("enqueued type = %q, want %q", q.last, jobs.TypePSMigrate)
	}
	var body importResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.JobID != 9 || body.Status != string(jobs.StateQueued) {
		t.Errorf("body = %+v, want job 9 queued", body)
	}
}

// TestImportPhotoSorter_conflict verifies an in-flight migration yields 409.
func TestImportPhotoSorter_conflict(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeQueue{err: jobs.ErrDuplicate})
	resp := post(t, srv, "/import/photosorter")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// TestRegisterRoutes_gating verifies disabled sources are not registered (404)
// while the run-history endpoint is always registered.
func TestRegisterRoutes_gating(t *testing.T) {
	t.Parallel()
	api := NewAPI(Config{
		Queue: &fakeQueue{}, Runs: &fakeRuns{}, RequireAdmin: passthrough, EnablePhotoPrism: true,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := post(t, srv, "/import/photosorter")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("disabled photosorter status = %d, want 404", resp.StatusCode)
	}

	runsResp := get(t, srv, "/import/runs")
	defer func() { _ = runsResp.Body.Close() }()
	if runsResp.StatusCode != http.StatusOK {
		t.Errorf("runs status = %d, want 200", runsResp.StatusCode)
	}
}

// TestListRuns_returnsRunsAndSources verifies the history endpoint returns the
// stored runs and reflects which sources are enabled.
func TestListRuns_returnsRunsAndSources(t *testing.T) {
	t.Parallel()
	finished := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	runs := &fakeRuns{runs: []importer.Run{{
		ID:         3,
		Source:     importer.SourcePhotoPrism,
		StartedAt:  time.Date(2026, 6, 1, 11, 0, 0, 0, time.UTC),
		FinishedAt: &finished,
		Status:     importer.StatusDone,
		Counts:     importer.Counts{Imported: 5, Updated: 1, Skipped: 2, Failed: 0},
	}}}
	srv := newServerWithRuns(t, &fakeQueue{}, runs)

	resp := get(t, srv, "/import/runs")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body runsResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Runs) != 1 || body.Runs[0].ID != 3 || body.Runs[0].Counts.Imported != 5 {
		t.Errorf("runs = %+v, want one run id 3 with 5 imported", body.Runs)
	}
	if !body.Sources.PhotoPrism || !body.Sources.PhotoSorter {
		t.Errorf("sources = %+v, want both enabled", body.Sources)
	}
	if body.Limit != defaultRunsLimit {
		t.Errorf("limit = %d, want default %d", body.Limit, defaultRunsLimit)
	}
}

// TestListRuns_paging verifies a valid limit/offset reaches the store and an
// invalid one yields 400.
func TestListRuns_paging(t *testing.T) {
	t.Parallel()
	runs := &fakeRuns{}
	srv := newServerWithRuns(t, &fakeQueue{}, runs)

	ok := get(t, srv, "/import/runs?limit=10&offset=20")
	defer func() { _ = ok.Body.Close() }()
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", ok.StatusCode)
	}
	if runs.gotLimit != 10 || runs.gotOffset != 20 {
		t.Errorf("store paging = (%d,%d), want (10,20)", runs.gotLimit, runs.gotOffset)
	}

	bad := get(t, srv, "/import/runs?limit=oops")
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid limit status = %d, want 400", bad.StatusCode)
	}
}

// TestListRuns_storeError verifies a store failure yields 500.
func TestListRuns_storeError(t *testing.T) {
	t.Parallel()
	srv := newServerWithRuns(t, &fakeQueue{}, &fakeRuns{err: errors.New("boom")})
	resp := get(t, srv, "/import/runs")
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
	_ = NewAPI(Config{Runs: &fakeRuns{}, RequireAdmin: passthrough})
}

// TestNewAPI_panicsOnNilRuns verifies a missing run store is a startup panic.
func TestNewAPI_panicsOnNilRuns(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewAPI did not panic on nil runs")
		}
	}()
	_ = NewAPI(Config{Queue: &fakeQueue{}, RequireAdmin: passthrough})
}
