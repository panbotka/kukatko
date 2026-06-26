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

// passthrough is a no-op middleware standing in for the admin guard.
func passthrough(next http.Handler) http.Handler { return next }

// newServer mounts the API with the given backfiller behind a passthrough guard.
func newServer(t *testing.T, bf Backfiller) *httptest.Server {
	t.Helper()
	api := NewAPI(Config{Backfiller: bf, RequireAdmin: passthrough})
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
	srv := newServer(t, bf)

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

	srv := newServer(t, &fakeBackfiller{err: errors.New("boom")})

	resp := postProcess(t, srv.URL+"/process/embeddings")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}
