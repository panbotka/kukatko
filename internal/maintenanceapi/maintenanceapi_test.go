package maintenanceapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
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

// fakeAudit is a stub AuditPurger capturing the cutoff it purged and the
// self-audit entry it recorded.
type fakeAudit struct {
	deleted     int
	purgeErr    error
	recordErr   error
	lastCutoff  time.Time
	lastEntry   audit.Entry
	purgeCalls  int
	recordCalls int
}

func (f *fakeAudit) PurgeOlderThan(_ context.Context, cutoff time.Time) (int, error) {
	f.purgeCalls++
	f.lastCutoff = cutoff
	return f.deleted, f.purgeErr
}

func (f *fakeAudit) Record(_ context.Context, entry audit.Entry) error {
	f.recordCalls++
	f.lastEntry = entry
	return f.recordErr
}

// passthrough is a no-op maintainer guard for tests.
func passthrough(next http.Handler) http.Handler { return next }

// newRouter mounts the API (over svc, which may be nil) on a chi router.
func newRouter(svc Service) http.Handler {
	r := chi.NewRouter()
	NewAPI(Config{Service: svc, RequireMaintainer: passthrough}).RegisterRoutes(r)
	return r
}

// newPurgeRouter mounts the API (over the audit purger, which may be nil) on a
// chi router; the maintenance Service is left nil since the purge does not use it.
func newPurgeRouter(a AuditPurger) http.Handler {
	r := chi.NewRouter()
	NewAPI(Config{Audit: a, RequireMaintainer: passthrough}).RegisterRoutes(r)
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

// TestAuditPurgeOK verifies a valid purge deletes older-than the derived cutoff,
// writes the self-audit record, and returns the deleted count.
func TestAuditPurgeOK(t *testing.T) {
	t.Parallel()
	fa := &fakeAudit{deleted: 7}
	before := time.Now().AddDate(0, 0, -30)
	rec := do(newPurgeRouter(fa), http.MethodPost, "/maintenance/audit/purge", `{"older_than_days":30}`)
	after := time.Now().AddDate(0, 0, -30)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if fa.purgeCalls != 1 {
		t.Fatalf("purge calls = %d, want 1", fa.purgeCalls)
	}
	// The cutoff is ~ now minus 30 days; allow a minute of slack around the call.
	if fa.lastCutoff.Before(before.Add(-time.Minute)) || fa.lastCutoff.After(after.Add(time.Minute)) {
		t.Errorf("cutoff = %v, want roughly now-30d", fa.lastCutoff)
	}
	if fa.recordCalls != 1 || fa.lastEntry.Action != audit.ActionAuditPurge {
		t.Errorf("self-audit = %d calls, action %q, want 1 call of %q",
			fa.recordCalls, fa.lastEntry.Action, audit.ActionAuditPurge)
	}
	if fa.lastEntry.Details["deleted"] != 7 || fa.lastEntry.Details["older_than_days"] != 30 {
		t.Errorf("self-audit details = %v, want deleted 7 / older_than_days 30", fa.lastEntry.Details)
	}
	var got auditPurgeResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Deleted != 7 || got.OlderThanDays != 30 || got.Cutoff == "" {
		t.Errorf("response = %+v, want deleted 7 / days 30 / cutoff set", got)
	}
}

// TestAuditPurgeBadRequests verifies a missing, non-positive, oversized, malformed
// or unknown-field body is rejected with 400 and never reaches the purge.
func TestAuditPurgeBadRequests(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"missing window":   `{}`,
		"zero window":      `{"older_than_days":0}`,
		"negative window":  `{"older_than_days":-5}`,
		"oversized window": `{"older_than_days":40000}`,
		"malformed body":   `{not json`,
		"unknown field":    `{"older_than_days":30,"bogus":true}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fa := &fakeAudit{}
			rec := do(newPurgeRouter(fa), http.MethodPost, "/maintenance/audit/purge", body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if fa.purgeCalls != 0 {
				t.Errorf("purge called %d times on a bad request, want 0", fa.purgeCalls)
			}
		})
	}
}

// TestAuditPurgeUnavailable verifies a nil audit purger answers 503.
func TestAuditPurgeUnavailable(t *testing.T) {
	t.Parallel()
	rec := do(newPurgeRouter(nil), http.MethodPost, "/maintenance/audit/purge", `{"older_than_days":30}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
