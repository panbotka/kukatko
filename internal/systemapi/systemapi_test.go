package systemapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/system"
)

// fakeCollector is a StatusCollector returning a fixed snapshot, fixed library
// counts, or an error.
type fakeCollector struct {
	status  system.Status
	library system.Library
	err     error
}

// Collect returns the configured snapshot or error.
func (f fakeCollector) Collect(context.Context) (system.Status, error) {
	return f.status, f.err
}

// LibraryStats returns the configured library counts or error.
func (f fakeCollector) LibraryStats(context.Context) (system.Library, error) {
	return f.library, f.err
}

// passThrough is a no-op route guard so the handler logic can be tested without
// the auth subsystem; the maintainer and authenticated gates are covered by the
// integration test.
func passThrough(next http.Handler) http.Handler { return next }

// newRouter mounts the system API with the given collector behind pass-through
// guards, returning the router ready for httptest requests.
func newRouter(collector StatusCollector) chi.Router {
	api := NewAPI(Config{
		Service:           collector,
		RequireMaintainer: passThrough,
		RequireAuth:       passThrough,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r
}

// TestHandleStatus_OK verifies a successful collection is serialised as JSON.
func TestHandleStatus_OK(t *testing.T) {
	t.Parallel()

	snapshot := system.Status{
		Embeddings: system.Embeddings{Online: true, URL: "http://box:8000"},
		Jobs:       system.Jobs{Total: 3, DeadLetter: 1},
	}
	r := newRouter(fakeCollector{status: snapshot})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/system/status", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got system.Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if !got.Embeddings.Online || got.Jobs.Total != 3 || got.Jobs.DeadLetter != 1 {
		t.Errorf("decoded = %+v, want online with total 3 / dead 1", got)
	}
}

// TestHandleStatus_Error verifies a collection failure yields 500 with the error
// envelope.
func TestHandleStatus_Error(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeCollector{err: errors.New("db down")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/system/status", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Error == "" {
		t.Error("error message empty, want a message")
	}
}

// TestHandleStats_OK verifies the library counts are serialised as JSON,
// including the derived coverage gaps the page is opened for.
func TestHandleStats_OK(t *testing.T) {
	t.Parallel()

	counts := system.Library{
		Photos: 20310, PhotosWithEmbedding: 20092, PhotosWithoutEmbedding: 218,
		Faces: 112806, PhotosWithFaces: 14567,
	}
	r := newRouter(fakeCollector{library: counts})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/system/stats", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got system.Library
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got != counts {
		t.Errorf("decoded = %+v, want %+v", got, counts)
	}
}

// TestHandleStats_Error verifies a failed aggregation yields 500 with the error
// envelope rather than a zeroed body that would read as an empty library.
func TestHandleStats_Error(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeCollector{err: errors.New("db down")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/system/stats", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body errorBody
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Error == "" {
		t.Error("error message empty, want a message")
	}
}
