//go:build integration

package ingest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/project"
)

// createLimitedProject creates a project with the given limits (nil = none).
func createLimitedProject(t *testing.T, env *testEnv, slug string, maxFile, maxTotal *int64) *project.Project {
	t.Helper()
	p, err := project.NewStore(env.db.Pool()).Create(t.Context(), audit.Meta{}, project.CreateParams{
		Slug: slug, Name: slug, MaxFileBytes: maxFile, MaxTotalBytes: maxTotal,
	})
	if err != nil {
		t.Fatalf("Create project %s: %v", slug, err)
	}
	return p
}

// projectPhotoCount counts the photo rows of a project.
func projectPhotoCount(t *testing.T, env *testEnv, projectUID string) int64 {
	t.Helper()
	usage, err := project.NewStore(env.db.Pool()).Usage(t.Context(), projectUID)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	return usage.Photos
}

// TestIngest_projectFileCap verifies a file over the project's max_file_bytes is
// refused with its cap and nothing is catalogued, while a file within it is
// created.
func TestIngest_projectFileCap(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	ctx := t.Context()
	small := jpegBytes(t, 10, 200, 10, 50)
	large := append(jpegBytes(t, 200, 10, 10, 100), make([]byte, 4096)...)
	limit := int64(len(small))
	proj := createLimitedProject(t, env, "capped", &limit, nil)

	refused := env.svc.Ingest(ctx, proj, bytes.NewReader(large), "large.jpg", env.uploader)
	if refused.Status != http.StatusRequestEntityTooLarge || refused.Code != ingest.CodeFileTooLarge ||
		refused.LimitBytes != limit {
		t.Fatalf("large file = %+v, want 413 file_too_large limit %d", refused, limit)
	}
	if n := projectPhotoCount(t, env, proj.UID); n != 0 {
		t.Fatalf("project holds %d photos after a refusal, want 0", n)
	}
	if ok := env.svc.Ingest(ctx, proj, bytes.NewReader(small), "small.jpg", env.uploader); ok.Outcome != ingest.OutcomeCreated {
		t.Fatalf("file at the cap = %+v, want created", ok)
	}
}

// TestIngest_projectQuota verifies the total quota: a file that would take the
// project past it is refused with the quota and the stored bytes and nothing is
// catalogued or deleted, while a byte-identical re-upload still answers
// duplicate (it stores nothing) as long as the project has a byte left.
func TestIngest_projectQuota(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	ctx := t.Context()
	first := jpegBytes(t, 210, 30, 30, 90)
	second := jpegBytes(t, 30, 210, 30, 90)
	third := jpegBytes(t, 30, 30, 210, 90)
	used := int64(len(first) + len(second))
	quota := used + 1
	proj := createLimitedProject(t, env, "quota", nil, &quota)

	for name, data := range map[string][]byte{"first.jpg": first, "second.jpg": second} {
		if res := env.svc.Ingest(ctx, proj, bytes.NewReader(data), name, env.uploader); res.Outcome != ingest.OutcomeCreated {
			t.Fatalf("%s = %+v, want created", name, res)
		}
	}
	refused := env.svc.Ingest(ctx, proj, bytes.NewReader(third), "third.jpg", env.uploader)
	if refused.Status != http.StatusRequestEntityTooLarge || refused.Code != ingest.CodeQuotaExceeded ||
		refused.LimitBytes != quota || refused.UsedBytes == nil || *refused.UsedBytes != used {
		t.Fatalf("third = %+v, want 413 quota_exceeded limit %d used %d", refused, quota, used)
	}
	if n := projectPhotoCount(t, env, proj.UID); n != 2 {
		t.Fatalf("project holds %d photos, want the 2 that fit (nothing deleted, nothing added)", n)
	}
	if dup := env.svc.Ingest(ctx, proj, bytes.NewReader(first), "again.jpg", env.uploader); dup.Outcome != ingest.OutcomeDuplicate {
		t.Fatalf("re-upload into a nearly full project = %+v, want duplicate", dup)
	}
}

// TestIngest_projectQuotaExactlyReached verifies a file that fills the quota to
// the byte is created, and that a project with no byte left then refuses every
// file before reading it — a byte-identical copy included, which the pipeline
// cannot recognise without reading it.
func TestIngest_projectQuotaExactlyReached(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	ctx := t.Context()
	first := jpegBytes(t, 210, 30, 30, 90)
	quota := int64(len(first))
	proj := createLimitedProject(t, env, "exact", nil, &quota)

	if res := env.svc.Ingest(ctx, proj, bytes.NewReader(first), "first.jpg", env.uploader); res.Outcome != ingest.OutcomeCreated {
		t.Fatalf("file filling the quota exactly = %+v, want created", res)
	}
	again := env.svc.Ingest(ctx, proj, bytes.NewReader(first), "again.jpg", env.uploader)
	if again.Code != ingest.CodeQuotaExceeded || again.UsedBytes == nil || *again.UsedBytes != quota {
		t.Fatalf("upload into a full project = %+v, want quota_exceeded", again)
	}
}

// TestIngest_noQuotaIsUnlimited verifies a project whose max_total_bytes is NULL
// takes everything.
func TestIngest_noQuotaIsUnlimited(t *testing.T) {
	env := newEnv(t, config.DuplicateConfig{Enabled: false})
	unlimited := createLimitedProject(t, env, "unlimited", nil, nil)
	files := map[string][]byte{
		"a.jpg": jpegBytes(t, 210, 30, 30, 90), "b.jpg": jpegBytes(t, 30, 210, 30, 90),
		"c.jpg": jpegBytes(t, 30, 30, 210, 90),
	}
	for name, data := range files {
		res := env.svc.Ingest(t.Context(), unlimited, bytes.NewReader(data), name, env.uploader)
		if res.Outcome != ingest.OutcomeCreated {
			t.Fatalf("%s into a project without a quota = %+v, want created", name, res)
		}
	}
}

// uploadPart is one part of an ordered multipart body: a plain field when
// filename is empty, otherwise a file.
type uploadPart struct {
	field    string
	filename string
	data     []byte
}

// uploadParts posts parts in order and returns the status and raw body.
func (e *httpEnv) uploadParts(t *testing.T, client *http.Client, parts ...uploadPart) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p.filename == "" {
			w, err = writer.CreateFormField(p.field)
		} else {
			w, err = writer.CreateFormFile(p.field, p.filename)
		}
		if err != nil {
			t.Fatalf("creating part: %v", err)
		}
		_, _ = w.Write(p.data)
	}
	_ = writer.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, e.server.URL+"/api/v1/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

// setBootstrapLimits sets the bootstrap project's limits (nil = leave as is).
func (e *httpEnv) setBootstrapLimits(t *testing.T, maxFile, maxTotal *int64) {
	t.Helper()
	_, err := e.projects.Update(t.Context(), audit.Meta{}, project.BootstrapUID, project.UpdatePatch{
		MaxFileBytes: maxFile, MaxTotalBytes: maxTotal,
	})
	if err != nil {
		t.Fatalf("Update bootstrap limits: %v", err)
	}
}

// decodeResults decodes a 200 upload body.
func decodeResults(t *testing.T, raw []byte) []ingest.FileResult {
	t.Helper()
	var decoded struct {
		Results []ingest.FileResult `json:"results"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decoding %q: %v", raw, err)
	}
	return decoded.Results
}

// TestHTTPUpload_fileCap verifies the 413 per-file body of a file over the
// project's cap — refused from its declared size alone and from its streamed
// size alike — beside a file within it, which is still created.
func TestHTTPUpload_fileCap(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "editor", auth.AccountEditor)
	small := jpegBytes(t, 10, 200, 10, 50)
	large := append(jpegBytes(t, 200, 10, 10, 100), make([]byte, 4096)...)
	limit := int64(len(small))
	env.setBootstrapLimits(t, &limit, nil)

	status, raw := env.uploadParts(t, client,
		uploadPart{field: ingest.SizeField, data: []byte(strconv.Itoa(len(large)))},
		uploadPart{field: "files", filename: "declared.jpg", data: large},
		uploadPart{field: "files", filename: "streamed.jpg", data: large},
		uploadPart{field: "files", filename: "small.jpg", data: small},
	)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, raw)
	}
	results := decodeResults(t, raw)
	if len(results) != 3 {
		t.Fatalf("results = %+v, want 3", results)
	}
	for _, res := range results[:2] {
		if res.Status != http.StatusRequestEntityTooLarge || res.Code != ingest.CodeFileTooLarge || res.LimitBytes != limit {
			t.Errorf("%s = %+v, want 413 file_too_large limit %d", res.Filename, res, limit)
		}
	}
	if results[2].Outcome != ingest.OutcomeCreated {
		t.Errorf("small.jpg = %+v, want created", results[2])
	}
}

// TestHTTPUpload_quota verifies the quota over HTTP: in a multi-file request
// the file that does not fit is refused with the numbers and the others go on;
// once the quota is used up the whole request is refused 413 before its body
// is read.
func TestHTTPUpload_quota(t *testing.T) {
	env := newHTTPEnv(t)
	client := env.loginClient(t, "editor", auth.AccountEditor)
	first := jpegBytes(t, 210, 30, 30, 90)
	second := jpegBytes(t, 30, 210, 30, 90)
	third := jpegBytes(t, 30, 30, 210, 90)
	quota := int64(len(first) + len(third))
	env.setBootstrapLimits(t, nil, &quota)

	status, raw := env.uploadParts(t, client,
		uploadPart{field: "files", filename: "first.jpg", data: first},
		uploadPart{field: "files", filename: "second.jpg", data: append(second, make([]byte, len(third))...)},
		uploadPart{field: "files", filename: "third.jpg", data: third},
	)
	if status != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", status, raw)
	}
	results := decodeResults(t, raw)
	if len(results) != 3 || results[0].Outcome != ingest.OutcomeCreated || results[2].Outcome != ingest.OutcomeCreated {
		t.Fatalf("results = %+v, want first and third created", results)
	}
	used := int64(len(first))
	if r := results[1]; r.Status != http.StatusRequestEntityTooLarge || r.Code != ingest.CodeQuotaExceeded ||
		r.LimitBytes != quota || r.UsedBytes == nil || *r.UsedBytes != used {
		t.Errorf("second = %+v, want 413 quota_exceeded limit %d used %d", r, quota, used)
	}

	status, raw = env.uploadParts(t, client, uploadPart{field: "files", filename: "x.jpg", data: second})
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("upload into a full project = %d (%s), want 413", status, raw)
	}
	want := `{"error":"quota_exceeded","limit_bytes":` + strconv.FormatInt(quota, 10) +
		`,"used_bytes":` + strconv.FormatInt(quota, 10) + `}`
	if got := string(bytes.TrimSpace(raw)); got != want {
		t.Errorf("413 body = %s, want %s", got, want)
	}
}
