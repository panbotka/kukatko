package photoapi

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
)

// fakeZipStorage is a storage.Storage that serves a fixed set of in-memory files
// by relative path and reports every other path as missing (wrapping
// os.ErrNotExist), so streamZip's dedup, skip-and-report and streaming can be
// exercised without a disk or a database. Only Open is meaningful; the other
// methods are unused by streamZip and return errors.
type fakeZipStorage struct {
	files map[string][]byte
}

// Open returns a reader over the stored bytes at relPath, or an error wrapping
// os.ErrNotExist when nothing is stored there.
func (f *fakeZipStorage) Open(_ context.Context, relPath string) (io.ReadCloser, error) {
	data, ok := f.files[relPath]
	if !ok {
		return nil, fmt.Errorf("open %s: %w", relPath, os.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (f *fakeZipStorage) Store(context.Context, io.Reader, time.Time, string) (storage.StoredFile, error) {
	return storage.StoredFile{}, errors.New("unused")
}
func (f *fakeZipStorage) Put(context.Context, io.Reader, storage.StoredFile) error {
	return errors.New("unused")
}
func (f *fakeZipStorage) Head(context.Context, string) (storage.StoredFile, error) {
	return storage.StoredFile{}, errors.New("unused")
}
func (f *fakeZipStorage) Check(context.Context) error { return nil }
func (f *fakeZipStorage) Stat(context.Context, string) (os.FileInfo, error) {
	return nil, errors.New("unused")
}
func (f *fakeZipStorage) Delete(context.Context, string) error { return errors.New("unused") }
func (f *fakeZipStorage) URL(string) string                    { return "" }
func (f *fakeZipStorage) Materialize(context.Context, string) (string, func(), error) {
	return "", func() {}, errors.New("unused")
}

// TestSanitizeEntryName covers the reduction of an original file name to a safe
// single-segment ZIP entry name.
func TestSanitizeEntryName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain name kept", "IMG_1234.jpg", "IMG_1234.jpg"},
		{"directory stripped", "2024/05/holiday.jpg", "holiday.jpg"},
		{"backslash directory stripped", `C:\Users\me\pic.png`, "pic.png"},
		{"control chars removed", "a\tb\nc.jpg", "abc.jpg"},
		{"empty falls back", "", "file"},
		{"dot falls back", ".", "file"},
		{"dotdot falls back", "..", "file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeEntryName(tt.in); got != tt.want {
				t.Errorf("sanitizeEntryName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestUniqueEntryName covers collision resolution, including the suffix landing
// before the extension and names that carry no extension.
func TestUniqueEntryName(t *testing.T) {
	t.Parallel()
	used := make(map[string]struct{})
	seq := []struct{ in, want string }{
		{"IMG.jpg", "IMG.jpg"},
		{"IMG.jpg", "IMG (2).jpg"},
		{"IMG.jpg", "IMG (3).jpg"},
		{"clip.mp4", "clip.mp4"},
		{"noext", "noext"},
		{"noext", "noext (2)"},
		{"IMG (2).jpg", "IMG (2) (2).jpg"}, // "IMG (2).jpg" was already taken by an earlier collision
	}
	for _, step := range seq {
		if got := uniqueEntryName(step.in, used); got != step.want {
			t.Errorf("uniqueEntryName(%q) = %q, want %q", step.in, got, step.want)
		}
	}
}

// TestZipArchiveName covers the archive filename derivation for the name, dated
// and empty cases, and that separators in a caller value are stripped.
func TestZipArchiveName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		req  zipDownloadRequest
		want string
	}{
		{"name wins", zipDownloadRequest{Name: "Summer 2024", Date: "2026-07-12"}, "Summer 2024.zip"},
		{"date default", zipDownloadRequest{Date: "2026-07-12"}, "kukatko-photos-2026-07-12.zip"},
		{"empty default", zipDownloadRequest{}, "kukatko-photos.zip"},
		{"name separators stripped", zipDownloadRequest{Name: "a/b\\c"}, "abc.zip"},
		{"blank name falls through to date", zipDownloadRequest{Name: "   ", Date: "2026-01-01"}, "kukatko-photos-2026-01-01.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := zipArchiveName(tt.req); got != tt.want {
				t.Errorf("zipArchiveName(%+v) = %q, want %q", tt.req, got, tt.want)
			}
		})
	}
}

// TestOrderByUIDs proves the explicit list is returned in request order and that
// a UID with no matching photo is dropped.
func TestOrderByUIDs(t *testing.T) {
	t.Parallel()
	list := []photos.Photo{{UID: "b"}, {UID: "a"}, {UID: "c"}}
	got := orderByUIDs([]string{"a", "missing", "c", "b"}, list)
	want := []string{"a", "c", "b"}
	if len(got) != len(want) {
		t.Fatalf("orderByUIDs length = %d, want %d", len(got), len(want))
	}
	for i, uid := range want {
		if got[i].UID != uid {
			t.Errorf("orderByUIDs[%d] = %q, want %q", i, got[i].UID, uid)
		}
	}
}

// TestAppendUnique proves photos are de-duplicated by UID across appends.
func TestAppendUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	out := appendUnique(nil, seen, []photos.Photo{{UID: "a"}, {UID: "b"}})
	out = appendUnique(out, seen, []photos.Photo{{UID: "b"}, {UID: "c"}})
	want := []string{"a", "b", "c"}
	if len(out) != len(want) {
		t.Fatalf("appendUnique length = %d, want %d", len(out), len(want))
	}
	for i, uid := range want {
		if out[i].UID != uid {
			t.Errorf("appendUnique[%d] = %q, want %q", i, out[i].UID, uid)
		}
	}
}

// TestStreamZip_dedupAndMissing proves streamZip writes a valid ZIP with one
// entry per opened original, de-duplicates colliding entry names, streams the
// exact bytes, skips an original that is missing from storage, and reports the
// skip in a MISSING.txt manifest instead of aborting the archive.
func TestStreamZip_dedupAndMissing(t *testing.T) {
	t.Parallel()
	store := &fakeZipStorage{files: map[string][]byte{
		"2024/05/1.jpg": []byte("AAAA"),
		"2024/05/2.jpg": []byte("BBBB"),
	}}
	api := &API{storage: store}
	list := []photos.Photo{
		{UID: "p1", FileName: "IMG.jpg", FilePath: "2024/05/1.jpg"},
		{UID: "p2", FileName: "IMG.jpg", FilePath: "2024/05/2.jpg"},
		{UID: "p3", FileName: "gone.jpg", FilePath: "2024/05/missing.jpg"},
	}

	rec := httptest.NewRecorder()
	api.streamZip(t.Context(), rec, list, "test.zip")

	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="test.zip"` {
		t.Errorf("Content-Disposition = %q, want attachment filename test.zip", cd)
	}

	body := rec.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	got := make(map[string]string, len(zr.File))
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening entry %s: %v", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("reading entry %s: %v", f.Name, err)
		}
		got[f.Name] = string(data)
	}

	if got["IMG.jpg"] != "AAAA" {
		t.Errorf("entry IMG.jpg = %q, want AAAA", got["IMG.jpg"])
	}
	if got["IMG (2).jpg"] != "BBBB" {
		t.Errorf("entry IMG (2).jpg = %q, want BBBB", got["IMG (2).jpg"])
	}
	if _, ok := got["gone.jpg"]; ok {
		t.Error("missing original gone.jpg was included, want skipped")
	}
	manifest, ok := got[missingManifestName]
	if !ok {
		t.Fatalf("archive has no %s manifest", missingManifestName)
	}
	if !strings.Contains(manifest, "gone.jpg") {
		t.Errorf("%s does not mention the skipped file: %q", missingManifestName, manifest)
	}
}
