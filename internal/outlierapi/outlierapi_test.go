package outlierapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/outlierapi"
	"github.com/panbotka/kukatko/internal/outliers"
	"github.com/panbotka/kukatko/internal/people"
)

// fakeService is a Service returning a canned result or error and recording the
// options it was called with.
type fakeService struct {
	result outliers.Result
	err    error
	opts   outliers.Options
}

// Outliers records opts and returns the canned result and error regardless of
// subject.
func (f *fakeService) Outliers(_ context.Context, _ string, opts outliers.Options) (outliers.Result, error) {
	f.opts = opts
	return f.result, f.err
}

// passThrough is a no-op write guard so handler behaviour is tested without auth.
func passThrough(next http.Handler) http.Handler {
	return next
}

// newServer mounts an API backed by svc behind the pass-through guard.
func newServer(svc outlierapi.Service) http.Handler {
	api := outlierapi.NewAPI(outlierapi.Config{Service: svc, RequireWrite: passThrough})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// outliersRequest builds a GET request for the given subject's outliers, query
// string included when non-empty.
func outliersRequest(uid, query string) *http.Request {
	target := "/subjects/" + uid + "/outliers"
	if query != "" {
		target += "?" + query
	}
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
}

// TestHandleList_ok returns the ranking as JSON with default (zero) options.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	svc := &fakeService{result: outliers.Result{
		SubjectUID:  "su_alice",
		Count:       1,
		Meaningful:  false,
		AvgDistance: 0.4,
		NoEmbedding: 2,
		Faces:       []outliers.OutlierFace{{PhotoUID: "p1", Distance: 0.4}},
	}}
	rec := httptest.NewRecorder()
	newServer(svc).ServeHTTP(rec, outliersRequest("su_alice", ""))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got outliers.Result
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SubjectUID != "su_alice" || got.Count != 1 || len(got.Faces) != 1 || got.Faces[0].PhotoUID != "p1" {
		t.Errorf("body mismatch: %+v", got)
	}
	if got.AvgDistance != 0.4 || got.NoEmbedding != 2 {
		t.Errorf("stats mismatch: %+v", got)
	}
	if svc.opts != (outliers.Options{}) {
		t.Errorf("default options = %+v, want zero value", svc.opts)
	}
}

// TestHandleList_params forwards threshold and limit to the service.
func TestHandleList_params(t *testing.T) {
	t.Parallel()
	svc := &fakeService{}
	rec := httptest.NewRecorder()
	newServer(svc).ServeHTTP(rec, outliersRequest("su_alice", "threshold=0.35&limit=20"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.opts.Threshold != 0.35 || svc.opts.Limit != 20 {
		t.Errorf("options = %+v, want threshold 0.35 limit 20", svc.opts)
	}
}

// TestHandleList_badParams answers 400 for malformed threshold or limit values.
func TestHandleList_badParams(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		query string
	}{
		{name: "non-numeric threshold", query: "threshold=high"},
		{name: "negative threshold", query: "threshold=-0.1"},
		{name: "threshold above cosine range", query: "threshold=2.5"},
		{name: "non-numeric limit", query: "limit=all"},
		{name: "negative limit", query: "limit=-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &fakeService{}
			rec := httptest.NewRecorder()
			newServer(svc).ServeHTTP(rec, outliersRequest("su_alice", tt.query))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestHandleList_notFound maps the subject sentinel to 404.
func TestHandleList_notFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	svc := &fakeService{err: people.ErrSubjectNotFound}
	newServer(svc).ServeHTTP(rec, outliersRequest("su_x", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestHandleList_unavailable answers 503 when no backend is wired.
func TestHandleList_unavailable(t *testing.T) {
	t.Parallel()
	api := outlierapi.NewAPI(outlierapi.Config{Service: nil, RequireWrite: passThrough})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, outliersRequest("su_x", ""))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
