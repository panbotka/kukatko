package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/project"
)

// fakeUsage answers Usage with fixed stored bytes, or err.
type fakeUsage struct {
	bytes int64
	err   error
	calls int
}

// Usage returns the configured originals byte count.
func (f *fakeUsage) Usage(_ context.Context, _ string) (project.Usage, error) {
	f.calls++
	if f.err != nil {
		return project.Usage{}, f.err
	}
	return project.Usage{OriginalsBytes: f.bytes}, nil
}

// limitProject is a project with the given per-file cap and quota (0 = none).
func limitProject(maxFile, maxTotal int64) *project.Project {
	p := &project.Project{UID: "pjlimits", Active: true}
	if maxFile > 0 {
		p.MaxFileBytes = &maxFile
	}
	if maxTotal > 0 {
		p.MaxTotalBytes = &maxTotal
	}
	return p
}

// TestFileCap_smallestNonZeroWins covers how the project's, the instance's and
// the caller's (an upload link's) per-file caps combine.
func TestFileCap_smallestNonZeroWins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		instance int64
		project  int64
		link     int64
		want     int64
	}{
		{name: "no caps at all", want: 0},
		{name: "instance only", instance: 100, want: 100},
		{name: "project only", project: 50, want: 50},
		{name: "link only", link: 70, want: 70},
		{name: "project below instance", instance: 100, project: 50, want: 50},
		{name: "instance below project", instance: 30, project: 50, want: 30},
		{name: "link cap still applies under both", instance: 100, project: 50, link: 20, want: 20},
		{name: "link above both changes nothing", instance: 100, project: 50, link: 900, want: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := New(Config{MaxFileSize: tt.instance})
			req := Request{MaxFileSize: tt.link, project: limitProject(tt.project, 0)}
			if got := svc.fileCap(req); got != tt.want {
				t.Errorf("fileCap = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestCheckQuota covers the quota arithmetic: exactly reached is fine, one
// byte over is refused, no quota is unlimited.
func TestCheckQuota(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		quota    int64
		used     int64
		incoming int64
		wantFull bool
	}{
		{name: "quota NULL is unlimited", quota: 0, used: 1 << 40, incoming: 1 << 40},
		{name: "room to spare", quota: 1000, used: 100, incoming: 100},
		{name: "quota exactly reached", quota: 1000, used: 900, incoming: 100},
		{name: "one byte over", quota: 1000, used: 900, incoming: 101, wantFull: true},
		{name: "unknown size into a full project", quota: 1000, used: 1000, incoming: 0, wantFull: true},
		{name: "unknown size with a byte left", quota: 1000, used: 999, incoming: 0},
		{name: "already over (concurrent overshoot)", quota: 1000, used: 1200, incoming: 1, wantFull: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			usage := &fakeUsage{bytes: tt.used}
			svc := New(Config{Usage: usage})
			err := svc.CheckQuota(context.Background(), limitProject(0, tt.quota), tt.incoming)
			if !tt.wantFull {
				if err != nil {
					t.Fatalf("CheckQuota = %v, want nil", err)
				}
				return
			}
			var limitErr *LimitError
			if !errors.As(err, &limitErr) || !errors.Is(err, ErrQuotaExceeded) {
				t.Fatalf("CheckQuota = %v, want a quota LimitError", err)
			}
			if limitErr.LimitBytes != tt.quota || limitErr.UsedBytes == nil || *limitErr.UsedBytes != tt.used {
				t.Errorf("refusal = %+v, want limit %d used %d", limitErr, tt.quota, tt.used)
			}
		})
	}
}

// TestCheckQuota_noReadWithoutQuota verifies a project without a quota (and a
// service without a usage reader) costs no usage query.
func TestCheckQuota_noReadWithoutQuota(t *testing.T) {
	t.Parallel()
	usage := &fakeUsage{}
	svc := New(Config{Usage: usage})
	if err := svc.CheckQuota(context.Background(), limitProject(0, 0), 10); err != nil {
		t.Fatalf("CheckQuota = %v", err)
	}
	if usage.calls != 0 {
		t.Errorf("usage read %d times for a project without a quota", usage.calls)
	}
	if err := New(Config{}).CheckQuota(context.Background(), limitProject(0, 1), 10); err != nil {
		t.Errorf("CheckQuota without a usage reader = %v, want nil", err)
	}
}

// TestCheckQuota_readFailureRefuses verifies an unknown usage refuses the file
// rather than letting it past the quota.
func TestCheckQuota_readFailureRefuses(t *testing.T) {
	t.Parallel()
	svc := New(Config{Usage: &fakeUsage{err: errors.New("db down")}})
	err := svc.CheckQuota(context.Background(), limitProject(0, 1000), 10)
	if err == nil || errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("CheckQuota = %v, want the read error (not a quota refusal)", err)
	}
	if res := errorResult("a.jpg", err); res.Status != http.StatusInternalServerError {
		t.Errorf("result status = %d, want 500", res.Status)
	}
}

// TestIngestFile_preflightRefusesBeforeReading verifies a declared size over a
// cap, or a full project, is refused without reading a byte of the body.
func TestIngestFile_preflightRefusesBeforeReading(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cfg      Config
		proj     *project.Project
		req      Request
		wantCode string
		wantLim  int64
	}{
		{
			name: "over the project cap", proj: limitProject(100, 0),
			req: Request{DeclaredSize: 101}, wantCode: CodeFileTooLarge, wantLim: 100,
		},
		{
			name: "over the instance cap", cfg: Config{MaxFileSize: 50}, proj: limitProject(100, 0),
			req: Request{DeclaredSize: 60}, wantCode: CodeFileTooLarge, wantLim: 50,
		},
		{
			name: "over the link cap", cfg: Config{MaxFileSize: 500}, proj: limitProject(100, 0),
			req: Request{DeclaredSize: 30, MaxFileSize: 20}, wantCode: CodeFileTooLarge, wantLim: 20,
		},
		{
			name: "declared over the quota's remainder", cfg: Config{Usage: &fakeUsage{bytes: 900}},
			proj: limitProject(0, 1000), req: Request{DeclaredSize: 101}, wantCode: CodeQuotaExceeded, wantLim: 1000,
		},
		{
			name: "undeclared into a full project", cfg: Config{Usage: &fakeUsage{bytes: 1000}},
			proj: limitProject(0, 1000), req: Request{}, wantCode: CodeQuotaExceeded, wantLim: 1000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.cfg.TempDir = t.TempDir()
			src := &endlessReader{}
			tt.req.Filename = "big.jpg"
			res := New(tt.cfg).IngestFile(context.Background(), tt.proj, src, tt.req)
			if res.Status != http.StatusRequestEntityTooLarge || res.Code != tt.wantCode || res.LimitBytes != tt.wantLim {
				t.Errorf("result = %+v, want 413 %s limit %d", res, tt.wantCode, tt.wantLim)
			}
			if src.read != 0 {
				t.Errorf("read %d bytes of a file refused up front", src.read)
			}
		})
	}
}

// TestIngestFile_streamingAbort verifies an undeclared file over the project
// cap is cut off while streaming, after at most the cap + 1 bytes.
func TestIngestFile_streamingAbort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := &endlessReader{}
	res := New(Config{TempDir: dir}).IngestFile(context.Background(), limitProject(4096, 0), src,
		Request{Filename: "endless.jpg"})
	if res.Status != http.StatusRequestEntityTooLarge || res.Code != CodeFileTooLarge || res.LimitBytes != 4096 {
		t.Errorf("result = %+v, want 413 file_too_large limit 4096", res)
	}
	if res.UsedBytes != nil {
		t.Errorf("a file-size refusal carries used_bytes %d", *res.UsedBytes)
	}
	if src.read > 4097 {
		t.Errorf("read %d bytes, want at most the cap + 1", src.read)
	}
	assertEmptyDir(t, dir)
}

// TestLimitError_body pins the 413 bodies' JSON shape.
func TestLimitError_body(t *testing.T) {
	t.Parallel()
	used := int64(900)
	tests := []struct {
		name string
		err  *LimitError
		want string
	}{
		{name: "file too large", err: tooLarge(100), want: `{"error":"file_too_large","limit_bytes":100}`},
		{
			name: "quota exceeded",
			err:  &LimitError{Code: CodeQuotaExceeded, LimitBytes: 1000, UsedBytes: &used},
			want: `{"error":"quota_exceeded","limit_bytes":1000,"used_bytes":900}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.err)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tt.want {
				t.Errorf("body = %s, want %s", raw, tt.want)
			}
			if tt.err.Error() == "" {
				t.Error("empty Error()")
			}
		})
	}
}

// TestErrorResult_limitFields verifies a limit refusal's per-file result
// carries its code and numbers, and a bare ErrFileTooLarge still gets the code.
func TestErrorResult_limitFields(t *testing.T) {
	t.Parallel()
	used := int64(7)
	quota := errorResult("q.jpg", &LimitError{Code: CodeQuotaExceeded, LimitBytes: 10, UsedBytes: &used})
	if quota.Status != http.StatusRequestEntityTooLarge || quota.Code != CodeQuotaExceeded ||
		quota.LimitBytes != 10 || quota.UsedBytes == nil || *quota.UsedBytes != 7 {
		t.Errorf("quota result = %+v", quota)
	}
	bare := errorResult("b.jpg", ErrFileTooLarge)
	if bare.Status != http.StatusRequestEntityTooLarge || bare.Code != CodeFileTooLarge {
		t.Errorf("bare too-large result = %+v", bare)
	}
	other := errorResult("o.jpg", errors.New("boom"))
	if other.Code != "" || other.LimitBytes != 0 {
		t.Errorf("other result = %+v, want no limit fields", other)
	}
}

// TestPartSize covers the declared size of a multipart file part.
func TestPartSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		header   string
		declared int64
		want     int64
	}{
		{name: "nothing declared", want: 0},
		{name: "size field", declared: 42, want: 42},
		{name: "Content-Length wins", header: "17", declared: 42, want: 17},
		{name: "malformed Content-Length falls back", header: "lots", declared: 42, want: 42},
		{name: "negative Content-Length falls back", header: "-1", declared: 42, want: 42},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			part := filePart(t, tt.header)
			if got := PartSize(part, tt.declared); got != tt.want {
				t.Errorf("PartSize = %d, want %d", got, tt.want)
			}
		})
	}
}

// filePart builds one multipart file part, with a Content-Length header when
// contentLength is not empty.
func filePart(t *testing.T, contentLength string) *multipart.Part {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="a.jpg"`)
	if contentLength != "" {
		h.Set("Content-Length", contentLength)
	}
	w, err := mw.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("x"))
	_ = mw.Close()
	part, err := multipart.NewReader(&body, mw.Boundary()).NextPart()
	if err != nil {
		t.Fatal(err)
	}
	return part
}

// TestReadSizeField covers the parsing of a declared size.
func TestReadSizeField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int64
	}{
		{in: "1234", want: 1234},
		{in: " 99 \n", want: 99},
		{in: "", want: 0},
		{in: "-5", want: 0},
		{in: "ten", want: 0},
		{in: strings.Repeat("9", 64), want: 0},
	}
	for _, tt := range tests {
		if got := ReadSizeField(strings.NewReader(tt.in)); got != tt.want {
			t.Errorf("ReadSizeField(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestQuotaRefusal verifies the request-level check: a full project yields its
// 413 body, room or a failed read yields none.
func TestQuotaRefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	full := New(Config{Usage: &fakeUsage{bytes: 1000}})
	if body, refused := QuotaRefusal(ctx, full, limitProject(0, 1000)); !refused || body.Code != CodeQuotaExceeded {
		t.Errorf("full project: %+v, %v", body, refused)
	}
	room := New(Config{Usage: &fakeUsage{bytes: 10}})
	if _, refused := QuotaRefusal(ctx, room, limitProject(0, 1000)); refused {
		t.Error("project with room refused")
	}
	broken := New(Config{Usage: &fakeUsage{err: errors.New("db down")}})
	if _, refused := QuotaRefusal(ctx, broken, limitProject(0, 1000)); refused {
		t.Error("failed usage read refused the whole request")
	}
}
