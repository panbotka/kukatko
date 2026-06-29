package maintenanceapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/maintenance"
)

// fakeService is a stub Service capturing the repair options it received.
type fakeService struct {
	report     maintenance.Report
	scanErr    error
	result     maintenance.RepairResult
	repairErr  error
	lastOpts   maintenance.RepairOptions
	scanCalled bool
}

func (f *fakeService) Scan(context.Context) (maintenance.Report, error) {
	f.scanCalled = true
	return f.report, f.scanErr
}

func (f *fakeService) Repair(_ context.Context, opts maintenance.RepairOptions) (maintenance.RepairResult, error) {
	f.lastOpts = opts
	return f.result, f.repairErr
}

// passthrough is a no-op admin guard for tests.
func passthrough(next http.Handler) http.Handler { return next }

// newRouter mounts the API (over svc, which may be nil) on a chi router.
func newRouter(svc Service) http.Handler {
	r := chi.NewRouter()
	NewAPI(Config{Service: svc, RequireAdmin: passthrough}).RegisterRoutes(r)
	return r
}

// do issues a request and returns the recorder.
func do(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestScanOK verifies a successful scan returns 200 with the report.
func TestScanOK(t *testing.T) {
	t.Parallel()
	svc := &fakeService{report: maintenance.Report{Photos: 5, MissingThumbnails: maintenance.Finding{Count: 2}}}
	rec := do(newRouter(svc), http.MethodGet, "/maintenance/scan", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got maintenance.Report
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Photos != 5 || got.MissingThumbnails.Count != 2 {
		t.Errorf("report = %+v, want photos 5 / missing thumbs 2", got)
	}
}

// TestScanUnavailable verifies a nil service answers 503.
func TestScanUnavailable(t *testing.T) {
	t.Parallel()
	if rec := do(newRouter(nil), http.MethodGet, "/maintenance/scan", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// TestRepairOK verifies a valid repair request is decoded and dispatched.
func TestRepairOK(t *testing.T) {
	t.Parallel()
	svc := &fakeService{result: maintenance.RepairResult{ThumbnailsEnqueued: 4}}
	rec := do(newRouter(svc), http.MethodPost, "/maintenance/repair", `{"thumbnails":true,"faces":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if !svc.lastOpts.Thumbnails || !svc.lastOpts.Faces || svc.lastOpts.Embeddings {
		t.Errorf("opts = %+v, want thumbnails+faces only", svc.lastOpts)
	}
	var got maintenance.RepairResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ThumbnailsEnqueued != 4 {
		t.Errorf("ThumbnailsEnqueued = %d, want 4", got.ThumbnailsEnqueued)
	}
}

// TestRepairBadRequests verifies an empty selection, malformed body and unknown
// field are all rejected with 400.
func TestRepairBadRequests(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"no repair selected": `{}`,
		"malformed body":     `{not json`,
		"unknown field":      `{"bogus":true}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := do(newRouter(&fakeService{}), http.MethodPost, "/maintenance/repair", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestRepairOrphanUnavailable verifies ErrOrphanImportUnavailable maps to 503.
func TestRepairOrphanUnavailable(t *testing.T) {
	t.Parallel()
	svc := &fakeService{repairErr: maintenance.ErrOrphanImportUnavailable}
	rec := do(newRouter(svc), http.MethodPost, "/maintenance/repair", `{"import_orphans":true}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// TestRepairUnavailable verifies a nil service answers 503.
func TestRepairUnavailable(t *testing.T) {
	t.Parallel()
	rec := do(newRouter(nil), http.MethodPost, "/maintenance/repair", `{"faces":true}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
