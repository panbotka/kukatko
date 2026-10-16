package systemapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/system"
)

// fakeCollector is a StatusCollector returning a fixed snapshot, fixed library
// counts, fixed chart series, or an error.
type fakeCollector struct {
	status  system.Status
	library system.Library
	charts  system.Charts
	err     error
}

// Collect returns the configured snapshot or error.
func (f fakeCollector) Collect(context.Context) (system.Status, error) {
	return f.status, f.err
}

// LibraryStats returns the configured library counts or error.
func (f fakeCollector) LibraryStats(context.Context, string) (system.Library, error) {
	return f.library, f.err
}

// LibraryCharts returns the configured chart series or error.
func (f fakeCollector) LibraryCharts(context.Context, string) (system.Charts, error) {
	return f.charts, f.err
}

// passThrough is a no-op route guard so the handler logic can be tested without
// the auth subsystem; the maintainer and authenticated gates are covered by the
// integration test.
func passThrough(next http.Handler) http.Handler { return next }

// newRouter mounts the system API with the given collector behind pass-through
// guards — the instance routes and the project routes both under /api/v1 (the
// server mounts the latter under /api/v1/p/{project}, which the pass-through
// guards make irrelevant here) — returning the router ready for httptest
// requests.
func newRouter(collector StatusCollector) chi.Router {
	api := NewAPI(Config{
		Service:           collector,
		RequireMaintainer: passThrough,
		RequireAuth:       passThrough,
	})
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		api.RegisterRoutes(r)
		api.RegisterProjectRoutes(r)
	})
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
		Photos: 20000, PhotosWithEmbedding: 19800, PhotosWithoutEmbedding: 200,
		Faces: 110000, PhotosWithFaces: 14000,
		PhotosLive: 19990, PhotosHidden: 30, PhotosStacked: 240, PhotosListed: 19720,
	}
	r := newRouter(fakeCollector{library: counts})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/stats", nil)
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

// TestHandleStatsCharts_OK verifies the chart series are serialised as JSON on
// their own endpoint, so the page can draw them without waiting on anything else.
func TestHandleStatsCharts_OK(t *testing.T) {
	t.Parallel()

	charts := system.Charts{
		PhotosByYear:   []system.YearPhotos{{Year: 1905, Photos: 3}, {Year: 1906, Photos: 0}},
		AddedByMonth:   []system.MonthPhotos{{Month: "2026-08", Photos: 412}},
		TopCameras:     []system.CameraPhotos{{Camera: "Canon EOS 5D", Model: "Canon EOS 5D", Photos: 88}},
		StorageByMedia: []system.MediaStorage{{Media: "image", Photos: 20, Bytes: 4096}},
		StorageByYear:  []system.YearStorage{{Year: 2026, Photos: 20, Bytes: 4096, CumulativeBytes: 4096}},
	}
	r := newRouter(fakeCollector{charts: charts})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/stats/charts", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got system.Charts
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if !reflect.DeepEqual(got, charts) {
		t.Errorf("decoded = %+v, want %+v", got, charts)
	}
}

// TestHandleStatsCharts_Error verifies a failed aggregation yields 500 rather
// than empty series, which would draw as an empty library.
func TestHandleStatsCharts_Error(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeCollector{err: errors.New("db down")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/stats/charts", nil)
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

// TestHandleStats_Error verifies a failed aggregation yields 500 with the error
// envelope rather than a zeroed body that would read as an empty library.
func TestHandleStats_Error(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeCollector{err: errors.New("db down")})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/stats", nil)
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

// TestRegisterRoutes_noInstanceStats verifies the statistics are no longer
// served at the instance level, under /system: those routes counted every
// project's rows, so they must not come back by accident.
func TestRegisterRoutes_noInstanceStats(t *testing.T) {
	t.Parallel()

	r := newRouter(fakeCollector{})
	const instance = "/api/v1/system"
	for _, path := range []string{instance + "/stats", instance + "/stats/charts"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}
}
