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

// fakeService is a Service returning a canned result or error.
type fakeService struct {
	result outliers.Result
	err    error
}

// Outliers returns the canned result and error regardless of subject.
func (f fakeService) Outliers(_ context.Context, _ string) (outliers.Result, error) {
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

// outliersRequest builds a GET request for the given subject's outliers.
func outliersRequest(uid string) *http.Request {
	target := "/subjects/" + uid + "/outliers"
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
}

// TestHandleList_ok returns the ranking as JSON.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	svc := fakeService{result: outliers.Result{
		SubjectUID: "su_alice",
		Count:      1,
		Meaningful: false,
		Faces:      []outliers.OutlierFace{{PhotoUID: "p1", Distance: 0.4}},
	}}
	rec := httptest.NewRecorder()
	newServer(svc).ServeHTTP(rec, outliersRequest("su_alice"))

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
}

// TestHandleList_notFound maps the subject sentinel to 404.
func TestHandleList_notFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	svc := fakeService{err: people.ErrSubjectNotFound}
	newServer(svc).ServeHTTP(rec, outliersRequest("su_x"))
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
	r.ServeHTTP(rec, outliersRequest("su_x"))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
