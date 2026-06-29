package duplicatesapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/duplicates"
)

// fakeService records the arguments of FindGroups and returns a canned result or
// error.
type fakeService struct {
	result    duplicates.Result
	err       error
	gotLimit  int
	gotOffset int
	calls     int
}

// FindGroups records the paging arguments and returns the canned response.
func (f *fakeService) FindGroups(_ context.Context, limit, offset int) (duplicates.Result, error) {
	f.calls++
	f.gotLimit, f.gotOffset = limit, offset
	return f.result, f.err
}

// passThrough is a no-op middleware standing in for the auth write guard.
func passThrough(next http.Handler) http.Handler { return next }

// mount builds a chi router with the API registered, ready for httptest.
func mount(svc Service) http.Handler {
	api := NewAPI(Config{Service: svc, RequireWrite: passThrough})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// do issues a GET against the mounted router and returns the recorder.
func do(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHandleList_ok returns the service result with default paging.
func TestHandleList_ok(t *testing.T) {
	t.Parallel()
	svc := &fakeService{result: duplicates.Result{
		Groups: []duplicates.Group{{ID: "ph_a", KeeperUID: "ph_b", Reason: duplicates.ReasonPhash}},
		Total:  1, Limit: 20, Offset: 0,
	}}
	rec := do(t, mount(svc), "/duplicates")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got duplicates.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Total != 1 || len(got.Groups) != 1 || got.Groups[0].ID != "ph_a" {
		t.Errorf("unexpected body: %+v", got)
	}
}

// TestHandleList_paging forwards valid limit and offset.
func TestHandleList_paging(t *testing.T) {
	t.Parallel()
	svc := &fakeService{}
	do(t, mount(svc), "/duplicates?limit=5&offset=10")
	if svc.gotLimit != 5 || svc.gotOffset != 10 {
		t.Errorf("forwarded limit/offset = %d/%d, want 5/10", svc.gotLimit, svc.gotOffset)
	}
}

// TestHandleList_invalidPaging rejects malformed parameters with 400.
func TestHandleList_invalidPaging(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"/duplicates?limit=abc", "/duplicates?offset=-1"} {
		svc := &fakeService{}
		rec := do(t, mount(svc), target)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", target, rec.Code)
		}
		if svc.calls != 0 {
			t.Errorf("%s: service was called despite a bad request", target)
		}
	}
}

// TestHandleList_serviceError maps a scan failure to 500.
func TestHandleList_serviceError(t *testing.T) {
	t.Parallel()
	svc := &fakeService{err: errors.New("boom")}
	rec := do(t, mount(svc), "/duplicates")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestHandleList_notConfigured answers 503 when no service is wired.
func TestHandleList_notConfigured(t *testing.T) {
	t.Parallel()
	rec := do(t, mount(nil), "/duplicates")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
