package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// scrape renders the registry's /metrics output as a string for assertions.
func scrape(t *testing.T, r *Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	r.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

// TestRegistry_exposesExpectedMetricNames exercises every observation method once
// and verifies the resulting /metrics output carries every expected series name.
func TestRegistry_exposesExpectedMetricNames(t *testing.T) {
	t.Parallel()

	r := New()
	exerciseAll(r)

	body := scrape(t, r)
	wantNames := []string{
		"kukatko_http_requests_total",
		"kukatko_http_request_duration_seconds",
		"kukatko_http_inflight_requests",
		"kukatko_jobs_started_total",
		"kukatko_jobs_finished_total",
		"kukatko_jobs_execution_duration_seconds",
		"kukatko_jobs_queue_depth",
		"kukatko_jobs_queue_depth_by_type",
		"kukatko_embedding_request_duration_seconds",
		"kukatko_embedding_service_up",
		"kukatko_import_run_photos",
		"kukatko_thumbnail_generation_duration_seconds",
		"go_goroutines",
	}
	for _, name := range wantNames {
		if !strings.Contains(body, name) {
			t.Errorf("/metrics output missing series %q", name)
		}
	}
}

// exerciseAll drives every Registry observation method so each series appears in
// a scrape (counters/histograms with label vectors are otherwise absent).
func exerciseAll(r *Registry) {
	r.JobStarted("image_embed")
	r.JobFinished("image_embed", OutcomeSuccess, 250*time.Millisecond)
	r.ObserveEmbeddingCall("image", 10*time.Millisecond, nil)
	r.SetEmbeddingUp(true)
	r.SetImportProgress("photoprism", 1, 2, 3, 4)
	r.ObserveThumbnail(5 * time.Millisecond)
	r.RegisterJobQueue(
		func(context.Context) (map[string]int, error) { return map[string]int{"queued": 2}, nil },
		func(context.Context) (map[string]int, error) { return map[string]int{"image_embed": 2}, nil },
	)
	serveOnce(r, http.MethodGet, "/probe")
}

// serveOnce runs one request through the Middleware against a chi router with a
// registered route so the route label is the matched pattern, returning the
// recorder for assertions.
func serveOnce(r *Registry, method, path string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	router.Use(r.Middleware(RouteLabel))
	router.Get("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	router.ServeHTTP(rec, req)
	return rec
}

// TestMiddleware_recordsRequest verifies the HTTP middleware records the request
// against requests_total with the matched route pattern and response status.
func TestMiddleware_recordsRequest(t *testing.T) {
	t.Parallel()

	r := New()
	rec := serveOnce(r, http.MethodGet, "/probe")
	if rec.Code != http.StatusTeapot {
		t.Fatalf("handler status = %d, want %d", rec.Code, http.StatusTeapot)
	}

	body := scrape(t, r)
	want := `kukatko_http_requests_total{method="GET",route="/probe",status="418"} 1`
	if !strings.Contains(body, want) {
		t.Errorf("/metrics output missing %q\n--- got ---\n%s", want, body)
	}
}

// TestMiddleware_preservesFlusher verifies the status-recording wrapper stays
// transparent to http.Flusher, so streaming handlers (the recognition sweep) can push
// each line instead of buffering the whole response.
func TestMiddleware_preservesFlusher(t *testing.T) {
	t.Parallel()

	router := chi.NewRouter()
	router.Use(New().Middleware(RouteLabel))
	reached := false
	router.Get("/probe", func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("handler ResponseWriter is not an http.Flusher through the metrics middleware")
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("chunk")); err != nil {
			t.Errorf("write: %v", err)
		}
		flusher.Flush()
		reached = true
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/probe", nil)
	router.ServeHTTP(rec, req)

	if !reached {
		t.Fatal("handler did not reach the flush")
	}
	if !rec.Flushed {
		t.Error("recorder was not flushed through the middleware")
	}
}

// TestMiddleware_skipsMetricsPath verifies a scrape of /metrics is not itself
// counted as an HTTP request.
func TestMiddleware_skipsMetricsPath(t *testing.T) {
	t.Parallel()

	r := New()
	mw := r.Middleware(RouteLabel)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	handler.ServeHTTP(rec, req)

	if strings.Contains(scrape(t, r), `route="/metrics"`) {
		t.Error("scrape of /metrics was counted as a request")
	}
}

// TestObserveEmbeddingCall_outcome verifies the outcome label reflects the call
// error.
func TestObserveEmbeddingCall_outcome(t *testing.T) {
	t.Parallel()

	r := New()
	r.ObserveEmbeddingCall("text", time.Millisecond, errors.New("boom"))

	body := scrape(t, r)
	if !strings.Contains(body, `operation="text",outcome="error"`) {
		t.Errorf("expected an error-outcome embedding sample, got:\n%s", body)
	}
}
