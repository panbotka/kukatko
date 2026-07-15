package expandapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/expand"
	"github.com/panbotka/kukatko/internal/organize"
)

// fakeService scripts the Service behaviour: it records the last call and returns a
// scripted result or error for album and label lookups.
type fakeService struct {
	albumResult expand.Result
	albumErr    error
	labelResult expand.Result
	labelErr    error
	gotUID      string
	gotReq      expand.Request
}

func (f *fakeService) Album(_ context.Context, uid string, req expand.Request) (expand.Result, error) {
	f.gotUID, f.gotReq = uid, req
	return f.albumResult, f.albumErr
}

func (f *fakeService) Label(_ context.Context, uid string, req expand.Request) (expand.Result, error) {
	f.gotUID, f.gotReq = uid, req
	return f.labelResult, f.labelErr
}

// passthrough is a no-op write guard for tests.
func passthrough(next http.Handler) http.Handler { return next }

// newRouter mounts an API over svc, scoped under /api/v1 like the real server.
func newRouter(svc Service) http.Handler {
	api := NewAPI(Config{Service: svc, RequireWrite: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r
}

// do issues a GET and returns the recorder.
func do(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))
	return rec
}

// TestAlbumSimilar_ok checks the happy path passes the UID and parsed query through
// and returns the service result as JSON.
func TestAlbumSimilar_ok(t *testing.T) {
	t.Parallel()
	svc := &fakeService{albumResult: expand.Result{Kind: expand.KindAlbum, CollectionUID: "al1", ResultCount: 0, Candidates: []expand.Candidate{}}}
	rec := do(t, newRouter(svc), "/api/v1/albums/al1/similar?threshold=0.25&limit=10")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.gotUID != "al1" || svc.gotReq.Threshold != 0.25 || svc.gotReq.Limit != 10 {
		t.Errorf("service call = uid %q req %+v, want al1 / {0.25 10}", svc.gotUID, svc.gotReq)
	}
	var body expand.Result
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Kind != expand.KindAlbum || body.CollectionUID != "al1" {
		t.Errorf("body = %+v, want album al1", body)
	}
}

// TestLabelSimilar_defaultsWhenNoQuery checks omitted query params reach the service
// as zero values (so it applies its own defaults).
func TestLabelSimilar_defaultsWhenNoQuery(t *testing.T) {
	t.Parallel()
	svc := &fakeService{labelResult: expand.Result{Candidates: []expand.Candidate{}}}
	rec := do(t, newRouter(svc), "/api/v1/labels/lb1/similar")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.gotUID != "lb1" || svc.gotReq != (expand.Request{}) {
		t.Errorf("service call = uid %q req %+v, want lb1 / zero request", svc.gotUID, svc.gotReq)
	}
}

// TestSimilar_notFound checks the organize sentinels map to 404.
func TestSimilar_notFound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		svc    *fakeService
		target string
	}{
		{"album", &fakeService{albumErr: organize.ErrAlbumNotFound}, "/api/v1/albums/nope/similar"},
		{"label", &fakeService{labelErr: organize.ErrLabelNotFound}, "/api/v1/labels/nope/similar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := do(t, newRouter(tt.svc), tt.target)
			if rec.Code != http.StatusNotFound {
				t.Errorf("status = %d, want 404", rec.Code)
			}
		})
	}
}

// TestSimilar_badQuery checks a non-numeric or negative threshold/limit is a 400.
func TestSimilar_badQuery(t *testing.T) {
	t.Parallel()
	tests := []string{
		"/api/v1/albums/al1/similar?threshold=abc",
		"/api/v1/albums/al1/similar?threshold=-0.1",
		"/api/v1/labels/lb1/similar?limit=xyz",
		"/api/v1/labels/lb1/similar?limit=-5",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			rec := do(t, newRouter(&fakeService{}), target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for %s", rec.Code, target)
			}
		})
	}
}

// TestSimilar_serviceUnavailable checks a nil service answers 503, not a panic.
func TestSimilar_serviceUnavailable(t *testing.T) {
	t.Parallel()
	rec := do(t, newRouter(nil), "/api/v1/albums/al1/similar")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// TestSimilar_internalError checks a non-sentinel error maps to 500.
func TestSimilar_internalError(t *testing.T) {
	t.Parallel()
	svc := &fakeService{albumErr: context.DeadlineExceeded}
	rec := do(t, newRouter(svc), "/api/v1/albums/al1/similar")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
