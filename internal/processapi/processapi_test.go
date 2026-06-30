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

// passthrough is a no-op middleware standing in for the admin guard.
func passthrough(next http.Handler) http.Handler { return next }

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
