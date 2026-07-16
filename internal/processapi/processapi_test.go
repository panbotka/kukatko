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

// fakeBackfiller is a Backfiller stub returning canned values.
type fakeBackfiller struct {
	enqueued int
	err      error
	calls    int
}

// BackfillEmbeddings records the call and returns the canned result.
func (f *fakeBackfiller) BackfillEmbeddings(context.Context) (int, error) {
	f.calls++
	return f.enqueued, f.err
}

// fakeFaceBackfiller is a FaceBackfiller stub returning canned values.
type fakeFaceBackfiller struct {
	enqueued int
	err      error
	calls    int
}

// BackfillFaces records the call and returns the canned result.
func (f *fakeFaceBackfiller) BackfillFaces(context.Context) (int, error) {
	f.calls++
	return f.enqueued, f.err
}

// fakeReclusterer is a Reclusterer stub returning canned values.
type fakeReclusterer struct {
	created int
	err     error
	calls   int
}

// Recluster records the call and returns the canned result.
func (f *fakeReclusterer) Recluster(context.Context) (int, error) {
	f.calls++
	return f.created, f.err
}

// fakePlacesBackfiller is a PlacesBackfiller stub returning canned values.
type fakePlacesBackfiller struct {
	enqueued int
	err      error
	calls    int
}

// BackfillPlaces records the call and returns the canned result.
func (f *fakePlacesBackfiller) BackfillPlaces(context.Context) (int, error) {
	f.calls++
	return f.enqueued, f.err
}

// fakeThumbnailBackfiller models the thumbnail backfill over an in-memory queue:
// it "enqueues" a job per candidate uid (the missing set, or the active set when
// all is true), deduping against jobs already pending — mirroring the real
// service, which counts every candidate and leaves dedup to the queue. It records
// the last `all` flag and how many genuine jobs it created so a test can assert
// both the reported count and idempotency across repeat calls.
type fakeThumbnailBackfiller struct {
	missing []string
	active  []string
	pending map[string]bool
	created int
	calls   int
	lastAll bool
	err     error
}

// newFakeThumbnailBackfiller returns a fake seeded with the missing and active
// candidate uids.
func newFakeThumbnailBackfiller(missing, active []string) *fakeThumbnailBackfiller {
	return &fakeThumbnailBackfiller{missing: missing, active: active, pending: map[string]bool{}}
}

// BackfillThumbnails schedules the appropriate candidate set, deduping against
// already-pending jobs, and returns how many candidates it iterated.
func (f *fakeThumbnailBackfiller) BackfillThumbnails(_ context.Context, all bool) (int, error) {
	f.calls++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	candidates := f.missing
	if all {
		candidates = f.active
	}
	for _, uid := range candidates {
		if !f.pending[uid] {
			f.pending[uid] = true
			f.created++
		}
	}
	return len(candidates), nil
}

// passthrough is a no-op middleware standing in for the admin guard.
func passthrough(next http.Handler) http.Handler { return next }

// forbid is an admin guard that rejects every request with 403, standing in for
// RequireAdmin denying a non-admin caller.
func forbid(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
}

// newServerWithThumbnails mounts the API with the given thumbnail backfiller (the
// others are stubbed) behind the given admin guard.
func newServerWithThumbnails(
	t *testing.T, tb ThumbnailBackfiller, guard func(http.Handler) http.Handler,
) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		ThumbnailBackfiller: tb, RequireAdmin: guard,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// newServer mounts the API with the given backfillers behind a passthrough guard.
func newServer(t *testing.T, bf Backfiller, ff FaceBackfiller) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Backfiller: bf, FaceBackfiller: ff, RequireAdmin: passthrough})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// newServerWithRecluster mounts the API with the given reclusterer (the
// backfillers are stubbed) behind a passthrough guard.
func newServerWithRecluster(t *testing.T, rc Reclusterer) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		Reclusterer: rc, RequireAdmin: passthrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// newServerWithPlaces mounts the API with the given places backfiller (the others
// are stubbed) behind a passthrough guard.
func newServerWithPlaces(t *testing.T, pb PlacesBackfiller) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		PlacesBackfiller: pb, RequireAdmin: passthrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// postProcess issues a POST to the backfill endpoint with a request-scoped
// context.
func postProcess(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	return resp
}

// TestBackfillEmbeddings_ok reports the enqueued count on success.
func TestBackfillEmbeddings_ok(t *testing.T) {
	t.Parallel()

	bf := &fakeBackfiller{enqueued: 7}
	srv := newServer(t, bf, &fakeFaceBackfiller{})

	resp := postProcess(t, srv.URL+"/process/embeddings")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 7 {
		t.Errorf("enqueued = %d, want 7", body.Enqueued)
	}
	if bf.calls != 1 {
		t.Errorf("backfiller calls = %d, want 1", bf.calls)
	}
}

// TestBackfillEmbeddings_error maps a backfill failure to 500.
func TestBackfillEmbeddings_error(t *testing.T) {
	t.Parallel()

	srv := newServer(t, &fakeBackfiller{err: errors.New("boom")}, &fakeFaceBackfiller{})

	resp := postProcess(t, srv.URL+"/process/embeddings")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestBackfillFaces_ok reports the enqueued count on success.
func TestBackfillFaces_ok(t *testing.T) {
	t.Parallel()

	ff := &fakeFaceBackfiller{enqueued: 4}
	srv := newServer(t, &fakeBackfiller{}, ff)

	resp := postProcess(t, srv.URL+"/process/faces")
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
	if ff.calls != 1 {
		t.Errorf("face backfiller calls = %d, want 1", ff.calls)
	}
}

// TestBackfillFaces_error maps a face-backfill failure to 500.
func TestBackfillFaces_error(t *testing.T) {
	t.Parallel()

	srv := newServer(t, &fakeBackfiller{}, &fakeFaceBackfiller{err: errors.New("boom")})

	resp := postProcess(t, srv.URL+"/process/faces")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestRecluster_ok reports the created count on success.
func TestRecluster_ok(t *testing.T) {
	t.Parallel()

	rc := &fakeReclusterer{created: 3}
	srv := newServerWithRecluster(t, rc)

	resp := postProcess(t, srv.URL+"/process/clusters")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body reclusterResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Created != 3 {
		t.Errorf("created = %d, want 3", body.Created)
	}
	if rc.calls != 1 {
		t.Errorf("reclusterer calls = %d, want 1", rc.calls)
	}
}

// TestRecluster_error maps a clustering failure to 500.
func TestRecluster_error(t *testing.T) {
	t.Parallel()

	srv := newServerWithRecluster(t, &fakeReclusterer{err: errors.New("boom")})

	resp := postProcess(t, srv.URL+"/process/clusters")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestRecluster_unavailable answers 503 when no clustering backend is wired.
func TestRecluster_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithRecluster(t, nil)

	resp := postProcess(t, srv.URL+"/process/clusters")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// fakeStacksDetector is a StacksDetector stub returning canned values.
type fakeStacksDetector struct {
	created int
	err     error
	calls   int
}

// DetectStacks records the call and returns the canned result.
func (f *fakeStacksDetector) DetectStacks(context.Context) (int, error) {
	f.calls++
	return f.created, f.err
}

// newServerWithStacks mounts the API with the given stacks detector behind a
// passthrough guard.
func newServerWithStacks(t *testing.T, sd StacksDetector) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		StacksDetector: sd, RequireAdmin: passthrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestDetectStacks_ok reports the created count on success.
func TestDetectStacks_ok(t *testing.T) {
	t.Parallel()

	sd := &fakeStacksDetector{created: 4}
	srv := newServerWithStacks(t, sd)

	resp := postProcess(t, srv.URL+"/process/stacks")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body stacksResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Created != 4 {
		t.Errorf("created = %d, want 4", body.Created)
	}
	if sd.calls != 1 {
		t.Errorf("detector calls = %d, want 1", sd.calls)
	}
}

// TestDetectStacks_error maps a detection failure to 500.
func TestDetectStacks_error(t *testing.T) {
	t.Parallel()

	srv := newServerWithStacks(t, &fakeStacksDetector{err: errors.New("boom")})

	resp := postProcess(t, srv.URL+"/process/stacks")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestDetectStacks_unavailable answers 503 when stacking is disabled.
func TestDetectStacks_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithStacks(t, nil)

	resp := postProcess(t, srv.URL+"/process/stacks")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillPlaces_ok reports the enqueued count on success.
func TestBackfillPlaces_ok(t *testing.T) {
	t.Parallel()

	pb := &fakePlacesBackfiller{enqueued: 5}
	srv := newServerWithPlaces(t, pb)

	resp := postProcess(t, srv.URL+"/process/places")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 5 {
		t.Errorf("enqueued = %d, want 5", body.Enqueued)
	}
	if pb.calls != 1 {
		t.Errorf("places backfiller calls = %d, want 1", pb.calls)
	}
}

// TestBackfillPlaces_error maps a place-backfill failure to 500.
func TestBackfillPlaces_error(t *testing.T) {
	t.Parallel()

	srv := newServerWithPlaces(t, &fakePlacesBackfiller{err: errors.New("boom")})

	resp := postProcess(t, srv.URL+"/process/places")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestBackfillPlaces_unavailable answers 503 when no geocoding backend is wired.
func TestBackfillPlaces_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithPlaces(t, nil)

	resp := postProcess(t, srv.URL+"/process/places")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillThumbnails_ok enqueues a thumbnail job for a photo missing its
// thumbnail and reports the enqueued count.
func TestBackfillThumbnails_ok(t *testing.T) {
	t.Parallel()

	tb := newFakeThumbnailBackfiller([]string{"p1"}, nil)
	srv := newServerWithThumbnails(t, tb, passthrough)

	resp := postProcess(t, srv.URL+"/process/thumbnails")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 1 {
		t.Errorf("enqueued = %d, want 1", body.Enqueued)
	}
	if tb.calls != 1 || tb.lastAll {
		t.Errorf("backfiller calls = %d, lastAll = %v, want 1 call with all=false", tb.calls, tb.lastAll)
	}
}

// TestBackfillThumbnails_all forwards ?all=true so the backfiller runs the full
// re-run over every non-archived photo.
func TestBackfillThumbnails_all(t *testing.T) {
	t.Parallel()

	tb := newFakeThumbnailBackfiller([]string{"p1"}, []string{"p1", "p2", "p3"})
	srv := newServerWithThumbnails(t, tb, passthrough)

	resp := postProcess(t, srv.URL+"/process/thumbnails?all=true")
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
	if !tb.lastAll {
		t.Error("?all=true should pass all=true to the backfiller")
	}
}

// TestBackfillThumbnails_idempotent verifies a repeat call stays safe: the
// endpoint answers 200 both times and the queue's dedup means no redundant jobs
// pile up across the two runs.
func TestBackfillThumbnails_idempotent(t *testing.T) {
	t.Parallel()

	tb := newFakeThumbnailBackfiller([]string{"p1", "p2"}, nil)
	srv := newServerWithThumbnails(t, tb, passthrough)

	for i := range 2 {
		resp := postProcess(t, srv.URL+"/process/thumbnails")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("call %d status = %d, want 200", i+1, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
	if tb.calls != 2 {
		t.Errorf("backfiller calls = %d, want 2", tb.calls)
	}
	if tb.created != 2 {
		t.Errorf("jobs created across both runs = %d, want 2 (deduped)", tb.created)
	}
}

// TestBackfillThumbnails_error maps a backfill failure to 500.
func TestBackfillThumbnails_error(t *testing.T) {
	t.Parallel()

	tb := &fakeThumbnailBackfiller{err: errors.New("boom")}
	srv := newServerWithThumbnails(t, tb, passthrough)

	resp := postProcess(t, srv.URL+"/process/thumbnails")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestBackfillThumbnails_unavailable answers 503 when no thumbnail backfiller is
// wired.
func TestBackfillThumbnails_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithThumbnails(t, nil, passthrough)

	resp := postProcess(t, srv.URL+"/process/thumbnails")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillThumbnails_forbidden verifies RequireAdmin guards the endpoint: a
// non-admin caller is rejected with 403 and the backfiller is never invoked.
func TestBackfillThumbnails_forbidden(t *testing.T) {
	t.Parallel()

	tb := newFakeThumbnailBackfiller([]string{"p1"}, nil)
	srv := newServerWithThumbnails(t, tb, forbid)

	resp := postProcess(t, srv.URL+"/process/thumbnails")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if tb.calls != 0 {
		t.Errorf("backfiller calls = %d, want 0 (guard should block)", tb.calls)
	}
}

// fakeMetadataBackfiller models the metadata backfill: it "enqueues" a job per
// candidate uid (the unread set, or the active set when all is true), recording
// the last `all` flag and how many calls it saw.
type fakeMetadataBackfiller struct {
	unread  []string
	active  []string
	calls   int
	lastAll bool
	err     error
}

// BackfillMetadata schedules the appropriate candidate set and returns its size.
func (f *fakeMetadataBackfiller) BackfillMetadata(_ context.Context, all bool) (int, error) {
	f.calls++
	f.lastAll = all
	if f.err != nil {
		return 0, f.err
	}
	if all {
		return len(f.active), nil
	}
	return len(f.unread), nil
}

// newServerWithMetadata mounts the API with the given metadata backfiller (the
// others are stubbed) behind the given admin guard.
func newServerWithMetadata(
	t *testing.T, mb MetadataBackfiller, guard func(http.Handler) http.Handler,
) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		MetadataBackfiller: mb, RequireAdmin: guard,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestBackfillMetadata_ok enqueues a metadata job for every photo whose file has
// never been read and reports the enqueued count.
func TestBackfillMetadata_ok(t *testing.T) {
	t.Parallel()

	mb := &fakeMetadataBackfiller{unread: []string{"p1", "p2"}}
	srv := newServerWithMetadata(t, mb, passthrough)

	resp := postProcess(t, srv.URL+"/process/metadata")
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
	if mb.calls != 1 || mb.lastAll {
		t.Errorf("backfiller calls = %d, lastAll = %v, want 1 call with all=false", mb.calls, mb.lastAll)
	}
}

// TestBackfillMetadata_all forwards ?all=true so the backfiller re-reads every
// non-archived photo, not just the ones never read.
func TestBackfillMetadata_all(t *testing.T) {
	t.Parallel()

	mb := &fakeMetadataBackfiller{unread: []string{"p1"}, active: []string{"p1", "p2", "p3"}}
	srv := newServerWithMetadata(t, mb, passthrough)

	resp := postProcess(t, srv.URL+"/process/metadata?all=true")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body backfillResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enqueued != 3 || !mb.lastAll {
		t.Errorf("enqueued = %d, lastAll = %v, want 3 with all=true", body.Enqueued, mb.lastAll)
	}
}

// TestBackfillMetadata_error maps a backfiller failure to 500.
func TestBackfillMetadata_error(t *testing.T) {
	t.Parallel()

	mb := &fakeMetadataBackfiller{err: errors.New("boom")}
	srv := newServerWithMetadata(t, mb, passthrough)

	resp := postProcess(t, srv.URL+"/process/metadata")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// TestBackfillMetadata_unavailable answers 503 when no metadata backfiller is
// wired.
func TestBackfillMetadata_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithMetadata(t, nil, passthrough)

	resp := postProcess(t, srv.URL+"/process/metadata")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// TestBackfillMetadata_forbidden verifies RequireAdmin guards the endpoint: a
// non-admin caller is rejected with 403 and the backfiller is never invoked.
func TestBackfillMetadata_forbidden(t *testing.T) {
	t.Parallel()

	mb := &fakeMetadataBackfiller{unread: []string{"p1"}}
	srv := newServerWithMetadata(t, mb, forbid)

	resp := postProcess(t, srv.URL+"/process/metadata")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	if mb.calls != 0 {
		t.Errorf("backfiller calls = %d, want 0 (guard should block)", mb.calls)
	}
}

// fakeLocationEstimator is a LocationEstimator stub returning canned values.
type fakeLocationEstimator struct {
	estimated int
	err       error
	calls     int
}

// BackfillLocations records the call and returns the canned result.
func (f *fakeLocationEstimator) BackfillLocations(context.Context) (int, error) {
	f.calls++
	return f.estimated, f.err
}

// newServerWithLocations mounts the API with the given location estimator behind
// a passthrough guard.
func newServerWithLocations(t *testing.T, le LocationEstimator) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		LocationEstimator: le, RequireAdmin: passthrough,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// TestEstimateLocations_ok reports the estimated count on success.
func TestEstimateLocations_ok(t *testing.T) {
	t.Parallel()

	le := &fakeLocationEstimator{estimated: 7}
	srv := newServerWithLocations(t, le)

	res := postProcess(t, srv.URL+"/process/locations")
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	var body locationsResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Estimated != 7 {
		t.Errorf("estimated = %d, want 7", body.Estimated)
	}
	if le.calls != 1 {
		t.Errorf("BackfillLocations called %d times, want 1", le.calls)
	}
}

// TestEstimateLocations_error answers 500 when the estimator fails.
func TestEstimateLocations_error(t *testing.T) {
	t.Parallel()

	srv := newServerWithLocations(t, &fakeLocationEstimator{err: errors.New("boom")})

	res := postProcess(t, srv.URL+"/process/locations")
	defer res.Body.Close()

	if res.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", res.StatusCode)
	}
}

// TestEstimateLocations_unavailable answers 503 when the feature is switched off
// (a nil estimator), rather than pretending it estimated nothing.
func TestEstimateLocations_unavailable(t *testing.T) {
	t.Parallel()

	srv := newServerWithLocations(t, nil)

	res := postProcess(t, srv.URL+"/process/locations")
	defer res.Body.Close()

	if res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", res.StatusCode)
	}
}

// TestEstimateLocations_forbidden checks the endpoint sits behind the admin
// guard: inventing coordinates across the library is not a viewer's call.
func TestEstimateLocations_forbidden(t *testing.T) {
	t.Parallel()

	le := &fakeLocationEstimator{estimated: 7}
	api := NewAPI(Config{
		Backfiller: &fakeBackfiller{}, FaceBackfiller: &fakeFaceBackfiller{},
		LocationEstimator: le, RequireAdmin: forbid,
	})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	res := postProcess(t, srv.URL+"/process/locations")
	defer res.Body.Close()

	if res.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", res.StatusCode)
	}
	if le.calls != 0 {
		t.Errorf("BackfillLocations called %d times, want 0 behind a closed guard", le.calls)
	}
}
