package ctl

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// uploadBody is a realistic answer of POST /upload: one new photo, one file the
// library already had, one that could not be read at all.
const uploadBody = `{"results":[
	{"filename":"a.jpg","status":201,"outcome":"created","photo_uid":"pht01",
	 "warnings":[{"code":"near_duplicate","message":"looks like pht09","photo_uid":"pht01"}]},
	{"filename":"b.jpg","status":409,"outcome":"duplicate","photo_uid":"pht02"},
	{"filename":"c.txt","status":500,"outcome":"error","error":"unsupported media type"}
]}`

// writeTempFile writes content into a file named name under a fresh temporary
// directory and returns its path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// TestResolveUploadFiles verifies the local checks that spare a doomed upload its
// round trip: nothing to send, a path that is not a file, and a path that is not
// there at all.
func TestResolveUploadFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := writeTempFile(t, "a.jpg", "bytes")

	tests := []struct {
		name    string
		paths   []string
		wantErr error
	}{
		{name: "no paths", paths: nil, wantErr: ErrNoUploadFiles},
		{name: "only blanks", paths: []string{"  ", ""}, wantErr: ErrNoUploadFiles},
		{name: "a directory", paths: []string{dir}, wantErr: ErrNotRegularFile},
		{name: "a missing file", paths: []string{filepath.Join(dir, "nope.jpg")}, wantErr: os.ErrNotExist},
		{name: "a real file", paths: []string{file}, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files, err := resolveUploadFiles(tt.paths)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("resolveUploadFiles(%v) error = %v, want %v", tt.paths, err, tt.wantErr)
			}
			if tt.wantErr == nil && (len(files) != 1 || files[0].name != "a.jpg") {
				t.Errorf("resolveUploadFiles(%v) = %+v, want one file named a.jpg", tt.paths, files)
			}
		})
	}
}

// TestClient_UploadPhotos verifies the files reach the server as a multipart
// body carrying their bytes and their base names, authenticated like every other
// call, and that the server's answer comes back unchanged.
func TestClient_UploadPhotos(t *testing.T) {
	t.Parallel()

	var (
		gotPath, gotAuth string
		gotNames         []string
		gotContents      []string
	)
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		gotNames, gotContents = readMultipart(t, r)
		w.Write([]byte(uploadBody))
	})

	first := writeTempFile(t, "a.jpg", "first bytes")
	second := writeTempFile(t, "b.jpg", "second bytes")
	raw, err := client.UploadPhotos(t.Context(), []string{first, second})
	if err != nil {
		t.Fatalf("UploadPhotos returned %v", err)
	}
	if gotPath != "/api/v1/upload" {
		t.Errorf("path = %q, want /api/v1/upload", gotPath)
	}
	if gotAuth != "Bearer kkt_a_b" {
		t.Errorf("Authorization = %q, want the bearer token", gotAuth)
	}
	if strings.Join(gotNames, ",") != "a.jpg,b.jpg" {
		t.Errorf("file names = %v, want the two base names in order", gotNames)
	}
	if strings.Join(gotContents, "|") != "first bytes|second bytes" {
		t.Errorf("file contents = %v, want both files' bytes", gotContents)
	}
	report, err := DecodeUploadReport(raw)
	if err != nil {
		t.Fatalf("DecodeUploadReport returned %v", err)
	}
	if len(report.Results) != 3 || report.Results[0].PhotoUID != "pht01" {
		t.Errorf("report = %+v, want the three per-file results", report)
	}
}

// readMultipart decodes the request's multipart body, returning each part's file
// name and its content.
func readMultipart(t *testing.T, r *http.Request) (names, contents []string) {
	t.Helper()

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		t.Fatalf("Content-Type = %q, want a multipart body", r.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return names, contents
		}
		if err != nil {
			t.Fatalf("reading the multipart body: %v", err)
		}
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatalf("reading a multipart part: %v", err)
		}
		names = append(names, part.FileName())
		contents = append(contents, string(body))
	}
}

// TestClient_UploadPhotos_refusedBeforeSending verifies an unusable path fails
// without the server ever hearing about it.
func TestClient_UploadPhotos_refusedBeforeSending(t *testing.T) {
	t.Parallel()

	called := false
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(uploadBody))
	})

	if _, err := client.UploadPhotos(t.Context(), []string{t.TempDir()}); !errors.Is(err, ErrNotRegularFile) {
		t.Fatalf("UploadPhotos(dir) error = %v, want ErrNotRegularFile", err)
	}
	if called {
		t.Error("the server was contacted for an upload that could never have been sent")
	}
}

// TestClient_UploadPhotos_forbidden verifies a viewer's token gets the typed
// role error, not the server's opaque body.
func TestClient_UploadPhotos_forbidden(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"insufficient permissions"}`))
	})

	_, err := client.UploadPhotos(t.Context(), []string{writeTempFile(t, "a.jpg", "bytes")})
	var forbidden *ForbiddenError
	if !errors.As(err, &forbidden) {
		t.Fatalf("UploadPhotos error = %v, want *ForbiddenError", err)
	}
}

// TestUploadReport_Counts verifies the tally, including that an outcome nobody
// anticipated is counted as a failure rather than passed over.
func TestUploadReport_Counts(t *testing.T) {
	t.Parallel()

	report := UploadReport{Results: []UploadResult{
		{Outcome: UploadCreated}, {Outcome: UploadCreated},
		{Outcome: UploadDuplicate},
		{Outcome: UploadFailed},
		{Outcome: "something new"},
	}}
	want := UploadCounts{Total: 5, Created: 2, Duplicate: 1, Failed: 2}
	if got := report.Counts(); got != want {
		t.Errorf("Counts() = %+v, want %+v", got, want)
	}
}

// TestWriteUploadReport verifies every file gets a row naming what became of it,
// and that the summary separates duplicates from failures.
func TestWriteUploadReport(t *testing.T) {
	t.Parallel()

	report, err := DecodeUploadReport([]byte(uploadBody))
	if err != nil {
		t.Fatalf("DecodeUploadReport returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteUploadReport(&buf, report); err != nil {
		t.Fatalf("WriteUploadReport returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"a.jpg", "created", "pht01", "near_duplicate",
		"b.jpg", "duplicate", "c.txt", "unsupported media type",
		"3 files · 1 created · 1 already in the library · 1 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("upload report is missing %q:\n%s", want, out)
		}
	}
}

// TestWriteUploadReport_empty verifies an empty report says so in prose rather
// than printing a bare header an agent could mistake for a row.
func TestWriteUploadReport_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteUploadReport(&buf, UploadReport{}); err != nil {
		t.Fatalf("WriteUploadReport returned %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "nothing was uploaded" {
		t.Errorf("output = %q, want the empty line", got)
	}
}
