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
)

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

// newServerWithRuns mounts the import API with the given run lister behind a
// pass-through maintainer guard.
func newServerWithRuns(t *testing.T, runs RunLister) *httptest.Server {
	t.Helper()
	return newServerWithConfig(t, Config{Runs: runs})
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

// TestRegisterRoutes_noTriggers pins that the retired migration triggers are
// gone for good: the PhotoPrism/photo-sorter import and the completeness verify
// were removed with their importers in August 2026, and nothing may re-mount
// them. A 404 here is the contract, not an accident of configuration.
func TestRegisterRoutes_noTriggers(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{})

	for _, path := range []string{"/import/photoprism", "/import/photosorter", "/import/photosorter-feeds"} {
		resp := post(t, srv, path)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s status = %d, want 404", path, resp.StatusCode)
		}
	}
	verify := get(t, srv, "/import/verify")
	defer func() { _ = verify.Body.Close() }()
	if verify.StatusCode != http.StatusNotFound {
		t.Errorf("GET /import/verify status = %d, want 404", verify.StatusCode)
	}
}

// TestListRuns_returnsRuns verifies the history endpoint returns the stored runs.
// The run it serves carries the retired photoprism source, because the finished
// migration's rows are kept as the catalogue's provenance record and must stay
// readable.
func TestListRuns_returnsRuns(t *testing.T) {
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
	srv := newServerWithRuns(t, runs)

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
	if body.Runs[0].Source != importer.SourcePhotoPrism {
		t.Errorf("run source = %q, want the historical photoprism source preserved", body.Runs[0].Source)
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
	srv := newServerWithRuns(t, runs)

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
	srv := newServerWithRuns(t, &fakeRuns{err: errors.New("boom")})
	resp := get(t, srv, "/import/runs")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestNewAPI_panicsOnNilRuns verifies a missing run store is a startup panic.
func TestNewAPI_panicsOnNilRuns(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewAPI did not panic on nil runs")
		}
	}()
	_ = NewAPI(Config{Failures: &fakeFailures{}, RequireMaintainer: passthrough})
}

// TestNewAPI_panicsOnNilFailures verifies a missing failure store is a startup panic.
func TestNewAPI_panicsOnNilFailures(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("NewAPI did not panic on nil failures")
		}
	}()
	_ = NewAPI(Config{Runs: &fakeRuns{}, RequireMaintainer: passthrough})
}

// TestListFailures_returnsPageAndFilter verifies the failures endpoint returns the
// stored failures and forwards the query filters to the store.
func TestListFailures_returnsPageAndFilter(t *testing.T) {
	t.Parallel()
	failures := &fakeFailures{failures: []importer.Failure{{
		ID: 1, RunID: 4, Source: importer.SourceFolder, Stage: importer.StagePhoto,
		SourceRef: "/srv/incoming/beach.jpg", Error: "decode failed",
	}}}
	srv := newServerWithConfig(t, Config{Failures: failures})

	resp := get(t, srv, "/import/failures?source=folder&run_id=4&unresolved=true&limit=5&offset=2")
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
	if got.Source != importer.SourceFolder || got.RunID != 4 || !got.UnresolvedOnly {
		t.Errorf("filter = %+v, want folder/run 4/unresolved", got)
	}
	if got.Limit != 5 || got.Offset != 2 {
		t.Errorf("filter paging = (%d,%d), want (5,2)", got.Limit, got.Offset)
	}
}

// TestListFailures_invalidRunID verifies a malformed run_id yields 400.
func TestListFailures_invalidRunID(t *testing.T) {
	t.Parallel()
	srv := newServerWithConfig(t, Config{})
	resp := get(t, srv, "/import/failures?run_id=oops")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
