package restoreapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/backup"
)

// fakeService is a stub Service for the HTTP tests.
type fakeService struct {
	dumps     []backup.DumpInfo
	report    backup.VerifyReport
	listErr   error
	verifyErr error
}

// ListDumps returns the configured dumps or error.
func (f *fakeService) ListDumps(context.Context) ([]backup.DumpInfo, error) {
	return f.dumps, f.listErr
}

// Verify returns the configured report or error.
func (f *fakeService) Verify(context.Context) (backup.VerifyReport, error) {
	return f.report, f.verifyErr
}

// passthrough is a maintainer guard that allows every request (auth is tested in
// the auth package; here we only test the restore handlers).
func passthrough(next http.Handler) http.Handler { return next }

// newRouter mounts the API under /api/v1 with the given service.
func newRouter(svc Service) http.Handler {
	api := NewAPI(Config{Service: svc, RequireMaintainer: passthrough})
	r := chi.NewRouter()
	r.Route("/api/v1", api.RegisterRoutes)
	return r
}

// do issues a request to path with method and returns the recorder.
func do(t *testing.T, svc Service, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, path, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	rec := httptest.NewRecorder()
	newRouter(svc).ServeHTTP(rec, req)
	return rec
}

func TestHandleListDumps(t *testing.T) {
	t.Parallel()
	svc := &fakeService{dumps: []backup.DumpInfo{
		{Key: "db/kukatko-20260102T000000Z.dump", Size: 20},
		{Key: "db/kukatko-20260101T000000Z.dump", Size: 10},
	}}
	rec := do(t, svc, http.MethodGet, "/api/v1/restore/dumps")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body dumpsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(body.Dumps) != 2 || body.Dumps[0].Key != "db/kukatko-20260102T000000Z.dump" {
		t.Errorf("dumps = %+v, want two newest-first", body.Dumps)
	}
}

func TestHandleListDumps_notConfigured(t *testing.T) {
	t.Parallel()
	rec := do(t, nil, http.MethodGet, "/api/v1/restore/dumps")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandleListDumps_error(t *testing.T) {
	t.Parallel()
	rec := do(t, &fakeService{listErr: errors.New("boom")}, http.MethodGet, "/api/v1/restore/dumps")
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestHandleVerify(t *testing.T) {
	t.Parallel()
	svc := &fakeService{report: backup.VerifyReport{
		PhotosInDB: 5, FilesInDB: 5, OriginalsOnDisk: 5, Consistent: true,
	}}
	rec := do(t, svc, http.MethodPost, "/api/v1/restore/verify")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body backup.VerifyReport
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if !body.Consistent || body.PhotosInDB != 5 {
		t.Errorf("report = %+v, want consistent with 5 photos", body)
	}
}

func TestHandleVerify_notConfigured(t *testing.T) {
	t.Parallel()
	rec := do(t, nil, http.MethodPost, "/api/v1/restore/verify")
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandleVerify_error(t *testing.T) {
	t.Parallel()
	rec := do(t, &fakeService{verifyErr: errors.New("boom")}, http.MethodPost, "/api/v1/restore/verify")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
