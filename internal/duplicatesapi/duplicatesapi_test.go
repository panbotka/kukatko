package duplicatesapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/dupmerge"
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

// fakeMerge records the last input and reports whether Merge or Preview was
// called, returning a canned result or error.
type fakeMerge struct {
	result       dupmerge.Result
	err          error
	gotInput     dupmerge.Input
	mergeCalls   int
	previewCalls int
}

// Merge records the input and returns the canned response.
func (f *fakeMerge) Merge(_ context.Context, in dupmerge.Input) (dupmerge.Result, error) {
	f.mergeCalls++
	f.gotInput = in
	return f.result, f.err
}

// Preview records the input and returns the canned response.
func (f *fakeMerge) Preview(_ context.Context, in dupmerge.Input) (dupmerge.Result, error) {
	f.previewCalls++
	f.gotInput = in
	return f.result, f.err
}

// mountMerge builds a chi router with only the merge dependency wired.
func mountMerge(merge MergeService) http.Handler {
	api := NewAPI(Config{Merge: merge, RequireWrite: passThrough})
	r := chi.NewRouter()
	api.RegisterRoutes(r)
	return r
}

// doMerge POSTs body to the merge endpoint and returns the recorder.
func doMerge(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "/duplicates/merge", strings.NewReader(body))
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestHandleMerge_ok forwards the keeper and members to Merge and returns its
// result.
func TestHandleMerge_ok(t *testing.T) {
	t.Parallel()
	merge := &fakeMerge{result: dupmerge.Result{KeeperUID: "ph_keep", Archived: 2, AlbumsAdded: 1}}
	rec := doMerge(t, mountMerge(merge),
		`{"keeper_uid":"ph_keep","member_uids":["ph_keep","ph_dup1","ph_dup2"]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if merge.mergeCalls != 1 || merge.previewCalls != 0 {
		t.Errorf("calls merge/preview = %d/%d, want 1/0", merge.mergeCalls, merge.previewCalls)
	}
	if merge.gotInput.KeeperUID != "ph_keep" || len(merge.gotInput.MemberUIDs) != 3 {
		t.Errorf("forwarded input = %+v", merge.gotInput)
	}
	var got dupmerge.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.KeeperUID != "ph_keep" || got.Archived != 2 || got.AlbumsAdded != 1 {
		t.Errorf("unexpected body: %+v", got)
	}
}

// TestHandleMerge_dryRun routes a preview request to Preview, not Merge.
func TestHandleMerge_dryRun(t *testing.T) {
	t.Parallel()
	merge := &fakeMerge{result: dupmerge.Result{KeeperUID: "ph_keep", DryRun: true}}
	rec := doMerge(t, mountMerge(merge),
		`{"keeper_uid":"ph_keep","member_uids":["ph_keep","ph_dup1"],"dry_run":true}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if merge.previewCalls != 1 || merge.mergeCalls != 0 {
		t.Errorf("calls merge/preview = %d/%d, want 0/1", merge.mergeCalls, merge.previewCalls)
	}
}

// TestHandleMerge_validationError maps a bad group to 400 without a 500.
func TestHandleMerge_validationError(t *testing.T) {
	t.Parallel()
	merge := &fakeMerge{err: dupmerge.ErrKeeperNotInGroup}
	rec := doMerge(t, mountMerge(merge),
		`{"keeper_uid":"ph_x","member_uids":["ph_a","ph_b"]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestHandleMerge_keeperNotFound maps a missing keeper to 404.
func TestHandleMerge_keeperNotFound(t *testing.T) {
	t.Parallel()
	merge := &fakeMerge{err: dupmerge.ErrKeeperNotFound}
	rec := doMerge(t, mountMerge(merge),
		`{"keeper_uid":"ph_gone","member_uids":["ph_gone","ph_b"]}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// TestHandleMerge_serviceError maps an unexpected failure to 500.
func TestHandleMerge_serviceError(t *testing.T) {
	t.Parallel()
	merge := &fakeMerge{err: errors.New("boom")}
	rec := doMerge(t, mountMerge(merge),
		`{"keeper_uid":"ph_keep","member_uids":["ph_keep","ph_b"]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// TestHandleMerge_badBody rejects malformed JSON with 400 and never calls merge.
func TestHandleMerge_badBody(t *testing.T) {
	t.Parallel()
	merge := &fakeMerge{}
	rec := doMerge(t, mountMerge(merge), `{"keeper_uid":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if merge.mergeCalls != 0 || merge.previewCalls != 0 {
		t.Errorf("merge was called on a bad body")
	}
}

// TestHandleMerge_notConfigured answers 503 when no merge service is wired.
func TestHandleMerge_notConfigured(t *testing.T) {
	t.Parallel()
	rec := doMerge(t, mountMerge(nil),
		`{"keeper_uid":"ph_keep","member_uids":["ph_keep","ph_b"]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
