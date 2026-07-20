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

// fakeFailures is a FailureLister whose ListFailures returns a fixed page or error
// and records the filter it was called with.
type fakeFailures struct {
	failures  []importer.Failure
	err       error
	gotFilter importer.FailureFilter
}

// ListFailures records the filter and returns the configured result.
func (f *fakeFailures) ListFailures(
	_ context.Context, filter importer.FailureFilter,
) ([]importer.Failure, error) {
	f.gotFilter = filter
	return f.failures, f.err
}

// fakeVerifier is a Verifier returning a fixed report or error.
type fakeVerifier struct {
	report any
	err    error
}

// Verify returns the configured report or error.
func (v *fakeVerifier) Verify(_ context.Context) (any, error) { return v.report, v.err }

// newServer mounts the import API (both triggers enabled) with a pass-through
// maintainer guard over a fresh chi router, returning a test server.
func newServer(t *testing.T, q Queue) *httptest.Server {
	t.Helper()
	return newServerWithRuns(t, q, &fakeRuns{})
}

// newServerWithRuns mounts the import API with the given queue and run lister
// (both triggers enabled) behind a pass-through maintainer guard.
func newServerWithRuns(t *testing.T, q Queue, runs RunLister) *httptest.Server {
	t.Helper()
	return newServerWithConfig(t, Config{
		Queue:             q,
		Runs:              runs,
		Failures:          &fakeFailures{},
		RequireMaintainer: passthrough,
		EnablePhotoPrism:  true,
		EnablePhotoSorter: true,
		EnableFeeds:       true,
	})
}

// newServerWithConfig mounts the import API for the given config, defaulting the
// required stores when a test left them nil, and returns a test server.
func newServerWithConfig(t *testing.T, cfg Config) *httptest.Server {
	t.Helper()
	if cfg.Runs == nil {
		cfg.Runs = &fakeRuns{}
	}
	if cfg.Failures == nil {
		cfg.Failures = &fakeFailures{}
	}
	if cfg.RequireMaintainer == nil {
		cfg.RequireMaintainer = passthrough
	}
	api := NewAPI(cfg)
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// passthrough is a maintainer guard that allows every request.
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

// TestImportFeeds_enqueued verifies a queued feeds import yields 202 with its id
// and uses the ps_feeds_import job type.
func TestImportFeeds_enqueued(t *testing.T) {
	t.Parallel()
	q := &fakeQueue{job: jobs.Job{ID: 11, State: jobs.StateQueued}}
	srv := newServer(t, q)
	resp := post(t, srv, "/import/photosorter-feeds")
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}
	if q.last != jobs.TypePSFeedsImport {
		t.Errorf("enqueued type = %q, want %q", q.last, jobs.TypePSFeedsImport)
	}
}

// TestImportFeeds_conflict verifies an in-flight feeds import yields 409.
func TestImportFeeds_conflict(t *testing.T) {
	t.Parallel()
	srv := newServer(t, &fakeQueue{err: jobs.ErrDuplicate})
	resp := post(t, srv, "/import/photosorter-feeds")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

// TestRegisterRoutes_gatingFeeds verifies the feeds trigger is not registered
// (404) when the feeds source is disabled.
func TestRegisterRoutes_gatingFeeds(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{Queue: &fakeQueue{}, EnablePhotoPrism: true})

	resp := post(t, srv, "/import/photosorter-feeds")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("disabled feeds status = %d, want 404", resp.StatusCode)
	}
}

// TestRegisterRoutes_gating verifies disabled sources are not registered (404)
// while the run-history endpoint is always registered.
func TestRegisterRoutes_gating(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{Queue: &fakeQueue{}, EnablePhotoPrism: true})

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
	if !body.Sources.PhotoPrism || !body.Sources.PhotoSorter || !body.Sources.Feeds {
		t.Errorf("sources = %+v, want all enabled", body.Sources)
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
	_ = NewAPI(Config{Runs: &fakeRuns{}, RequireMaintainer: passthrough})
}

// TestNewAPI_panicsOnNilRuns verifies a missing run store is a startup panic.
func TestNewAPI_panicsOnNilRuns(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewAPI did not panic on nil runs")
		}
	}()
	_ = NewAPI(Config{Queue: &fakeQueue{}, RequireMaintainer: passthrough})
}

// TestNewAPI_panicsOnNilFailures verifies a missing failure store is a startup panic.
func TestNewAPI_panicsOnNilFailures(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewAPI did not panic on nil failures")
		}
	}()
	_ = NewAPI(Config{Queue: &fakeQueue{}, Runs: &fakeRuns{}, RequireMaintainer: passthrough})
}

// TestListFailures_returnsPageAndFilter verifies the failures endpoint returns the
// stored failures and forwards the query filters to the store.
func TestListFailures_returnsPageAndFilter(t *testing.T) {
	t.Parallel()
	failures := &fakeFailures{failures: []importer.Failure{{
		ID: 1, RunID: 4, Source: importer.SourcePhotoPrism, Stage: importer.StagePhoto,
		SourceRef: "pp1", Error: "download failed",
	}}}
	srv := newServerWithConfig(t, Config{Queue: &fakeQueue{}, Failures: failures})

	resp := get(t, srv, "/import/failures?source=photoprism&run_id=4&unresolved=true&limit=5&offset=2")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body failuresResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Failures) != 1 || body.Failures[0].Stage != importer.StagePhoto {
		t.Errorf("failures = %+v, want one StagePhoto failure", body.Failures)
	}
	got := failures.gotFilter
	if got.Source != importer.SourcePhotoPrism || got.RunID != 4 || !got.UnresolvedOnly {
		t.Errorf("filter = %+v, want photoprism/run 4/unresolved", got)
	}
	if got.Limit != 5 || got.Offset != 2 {
		t.Errorf("filter paging = (%d,%d), want (5,2)", got.Limit, got.Offset)
	}
}

// TestListFailures_invalidRunID verifies a malformed run_id yields 400.
func TestListFailures_invalidRunID(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{Queue: &fakeQueue{}})
	resp := get(t, srv, "/import/failures?run_id=oops")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestVerify_unavailable verifies the verify endpoint answers 503 when no verifier
// is configured.
func TestVerify_unavailable(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{Queue: &fakeQueue{}})
	resp := get(t, srv, "/import/verify")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestVerify_ok verifies the verify endpoint returns the reconciler's report.
func TestVerify_ok(t *testing.T) {
	t.Parallel()
	report := map[string]any{"complete": true, "photoprism": map[string]any{"missing_count": 0}}
	srv := newServerWithConfig(t, Config{Queue: &fakeQueue{}, Verifier: &fakeVerifier{report: report}})

	resp := get(t, srv, "/import/verify")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["complete"] != true {
		t.Errorf("body = %+v, want complete true", body)
	}
}

// TestVerify_error verifies a reconciliation failure yields 502.
func TestVerify_error(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{
		Queue: &fakeQueue{}, Verifier: &fakeVerifier{err: errors.New("source unreachable")},
	})
	resp := get(t, srv, "/import/verify")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}
