package uploadlinkapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/uploadlinkapi"
)

// fakeQuota answers CheckQuota as a project that is full or has room.
type fakeQuota struct{ full bool }

// CheckQuota refuses every file when the project is full.
func (f fakeQuota) CheckQuota(_ context.Context, _ *project.Project, _ int64) error {
	if f.full {
		used := int64(1000)
		return &ingest.LimitError{Code: ingest.CodeQuotaExceeded, LimitBytes: 1000, UsedBytes: &used}
	}
	return nil
}

// TestUpload_linkCapReachesThePipeline verifies the link's own cap travels to
// the pipeline (which combines it with the project's and the instance's caps)
// with the size a file declared in the size field before it.
func TestUpload_linkCapReachesThePipeline(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *uploadlinkapi.Config) { c.MaxFileSize = 4 })
	got := results(t, h.upload(t, "", nil,
		part{field: ingest.SizeField, content: "5"},
		part{field: "files", filename: "big.jpg", content: "12345"},
		part{field: "files", filename: "ok.jpg", content: "1234"}))
	if len(got) != 2 || got[0].Code != ingest.CodeFileTooLarge || got[0].LimitBytes != 4 ||
		got[1].Outcome != ingest.OutcomeCreated {
		t.Fatalf("results = %+v", got)
	}
	h.ingest.mu.Lock()
	defer h.ingest.mu.Unlock()
	if r := h.ingest.requests; r[0].MaxFileSize != 4 || r[0].DeclaredSize != 5 || r[1].DeclaredSize != 0 {
		t.Errorf("requests = %+v, want the link cap on both and the declaration on the first only", r)
	}
}

// TestUpload_fullProject verifies a project with no room left is refused 413
// before the body is read, with no numbers in the body, and that the public
// page says the storage is full.
func TestUpload_fullProject(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *uploadlinkapi.Config) { c.Quota = fakeQuota{full: true} })
	rec := h.upload(t, "", nil, part{field: "files", filename: "a.jpg", content: "data"})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != `{"error":"quota_exceeded"}` {
		t.Errorf("body = %s, want the code alone", body)
	}
	if len(h.ingest.requests) != 0 || h.store.link.UploadCount != 0 {
		t.Errorf("pipeline saw %d files, link counted %d; want nothing", len(h.ingest.requests), h.store.link.UploadCount)
	}
	page := decode(t, h.do(t, http.MethodGet, "/u/"+testCode, "", ""))
	if page["storage_full"] != true {
		t.Errorf("page = %v, want storage_full", page)
	}
}

// TestUpload_projectWithRoom verifies a project with room reports no full
// storage and takes the upload.
func TestUpload_projectWithRoom(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *uploadlinkapi.Config) { c.Quota = fakeQuota{} })
	if page := decode(t, h.do(t, http.MethodGet, "/u/"+testCode, "", "")); page["storage_full"] != false {
		t.Errorf("page = %v, want storage_full false", page)
	}
	got := results(t, h.upload(t, "", nil, part{field: "files", filename: "a.jpg", content: "data"}))
	if len(got) != 1 || got[0].Outcome != ingest.OutcomeCreated {
		t.Errorf("results = %+v, want created", got)
	}
}

// TestUpload_quotaRefusalHasNoNumbers verifies a file the quota refused while
// streaming comes back without the project's sizes, and gives its slot back.
func TestUpload_quotaRefusalHasNoNumbers(t *testing.T) {
	t.Parallel()
	h := newHarness(t, nil)
	got := results(t, h.upload(t, "", nil, part{field: "files", filename: "a.jpg", content: "quota"}))
	if len(got) != 1 {
		t.Fatalf("results = %+v", got)
	}
	r := got[0]
	if r.Status != http.StatusRequestEntityTooLarge || r.Code != ingest.CodeQuotaExceeded ||
		r.LimitBytes != 0 || r.UsedBytes != nil || strings.Contains(r.Error, "990") {
		t.Errorf("result = %+v, want a bare quota_exceeded", r)
	}
	if h.store.released != 1 {
		t.Errorf("released %d slots, want the refused file's", h.store.released)
	}
}
