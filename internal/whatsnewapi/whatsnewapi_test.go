package whatsnewapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/whatsnew"
)

// passthrough is a guard that authenticates nothing, used to reach the handler
// directly. Requests through it carry no principal.
func passthrough(next http.Handler) http.Handler { return next }

// fakeStore is a Summarizer that counts the calls it receives. The digest itself
// is exercised end to end in the integration test, which drives the real auth
// guard; here the store exists to prove the handler does *not* reach it.
type fakeStore struct {
	calls int
}

// Summary records the call and returns an empty digest.
func (f *fakeStore) Summary(context.Context, string, time.Time) (whatsnew.Summary, error) {
	f.calls++
	return whatsnew.Summary{}, nil
}

// newRouter mounts the API with a pass-through guard over store.
func newRouter(store Summarizer, now func() time.Time) *chi.Mux {
	api := NewAPI(Config{Store: store, RequireAuth: passthrough, Now: now})
	router := chi.NewRouter()
	api.RegisterRoutes(router)
	return router
}

// get issues GET /whats-new against router and returns the recorder.
func get(t *testing.T, router *chi.Mux) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/whats-new", nil)
	router.ServeHTTP(rec, req)
	return rec
}

// TestWithoutPrincipal verifies the handler fails closed: a request that reaches
// it without an authenticated user — a guard wired wrong — is refused rather
// than served somebody else's digest, and the store is never touched.
func TestWithoutPrincipal(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	rec := get(t, newRouter(store, nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if store.calls != 0 {
		t.Errorf("store calls = %d, want 0", store.calls)
	}
}

// TestNewAPIDefaultsNow verifies Now defaults to the wall clock when the caller
// leaves it unset, so production wiring need not supply one.
func TestNewAPIDefaultsNow(t *testing.T) {
	t.Parallel()

	api := NewAPI(Config{Store: &fakeStore{}, RequireAuth: passthrough})
	if api.now == nil {
		t.Fatalf("now = nil, want a default clock")
	}
	if got := api.now(); got.IsZero() {
		t.Errorf("default now() = zero time, want the wall clock")
	}
}

// TestWriteJSONEmptySummary verifies the shape a client sees for "nothing to
// report": has_news false, no since, no lists — one payload shape for every
// outcome, so the panel branches on a single flag.
func TestWriteJSONEmptySummary(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, whatsnew.Summary{})
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body["has_news"] != false {
		t.Errorf("has_news = %v, want false", body["has_news"])
	}
	if _, ok := body["since"]; ok {
		t.Errorf("since present in an empty digest: %v", body["since"])
	}
	if _, ok := body["albums"]; ok {
		t.Errorf("albums present in an empty digest: %v", body["albums"])
	}
}

// TestWriteError verifies the standard error envelope.
func TestWriteError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeError(rec, http.StatusInternalServerError, "boom")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Error != "boom" {
		t.Errorf("error = %q, want %q", body.Error, "boom")
	}
}
