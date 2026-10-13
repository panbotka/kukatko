package storage

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestFSCopy_duplicatesAndLeavesTheSource verifies a copy lands at the new key
// with the same bytes, leaves the source in place, survives the source's later
// deletion, and leaves nothing in the in-progress directory.
func TestFSCopy_duplicatesAndLeavesTheSource(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	ctx := t.Context()
	content := []byte("an original moving under its project's prefix\n")
	stored, err := fs.Store(ctx, bytes.NewReader(content), fixedTime, "fox.jpg")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	const to = "projects/pjaaaaaaaaaaaaaaaaaaaaaaaa/2024/05/fox.jpg"

	if err := fs.Copy(ctx, stored.RelPath, to); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := fs.Delete(ctx, stored.RelPath); err != nil {
		t.Fatalf("Delete source: %v", err)
	}
	head, err := fs.Head(ctx, to)
	if err != nil {
		t.Fatalf("Head copy: %v", err)
	}
	if head.Hash != hashOf(content) || head.Size != int64(len(content)) {
		t.Errorf("copy = %+v, want the source's bytes", head)
	}
	entries, err := os.ReadDir(fs.tmpDir)
	if err != nil {
		t.Fatalf("reading tmp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("tmp dir holds %d entries after Copy, want none", len(entries))
	}
}

// TestFSCopy_replacesTheDestination verifies a copy onto an occupied key
// replaces it, and that copying a key onto itself changes nothing.
func TestFSCopy_replacesTheDestination(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	ctx := t.Context()
	src, err := fs.Store(ctx, bytes.NewReader([]byte("new bytes")), fixedTime, "a.jpg")
	if err != nil {
		t.Fatalf("Store a: %v", err)
	}
	dst, err := fs.Store(ctx, bytes.NewReader([]byte("stale bytes")), fixedTime, "b.jpg")
	if err != nil {
		t.Fatalf("Store b: %v", err)
	}

	if err := fs.Copy(ctx, src.RelPath, dst.RelPath); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := fs.Copy(ctx, src.RelPath, src.RelPath); err != nil {
		t.Fatalf("Copy onto itself: %v", err)
	}
	// A second copy between two links of one inode must not leave a staged link.
	if err := fs.Copy(ctx, src.RelPath, dst.RelPath); err != nil {
		t.Fatalf("second Copy: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(fs.root, filepath.FromSlash(dst.RelPath)))
	if err != nil {
		t.Fatalf("reading destination: %v", err)
	}
	if string(got) != "new bytes" {
		t.Errorf("destination = %q, want the source's bytes", got)
	}
	if entries, _ := os.ReadDir(fs.tmpDir); len(entries) != 0 {
		t.Errorf("tmp dir holds %d entries after Copy, want none", len(entries))
	}
}

// TestFSCopy_errors verifies a missing source wraps os.ErrNotExist and a key
// resolving to the root is ErrInvalidPath.
func TestFSCopy_errors(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	tests := []struct {
		name     string
		from, to string
		want     error
	}{
		{name: "missing source", from: "2024/05/none.jpg", to: "x/none.jpg", want: os.ErrNotExist},
		{name: "root source", from: "/", to: "x/a.jpg", want: ErrInvalidPath},
		{name: "root destination", from: "2024/05/a.jpg", to: "..", want: ErrInvalidPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := fs.Copy(t.Context(), tt.from, tt.to); !errors.Is(err, tt.want) {
				t.Errorf("Copy(%q, %q) = %v, want %v", tt.from, tt.to, err, tt.want)
			}
		})
	}
}

// TestIsContentETag tells a single-part MD5 tag from a multipart one.
func TestIsContentETag(t *testing.T) {
	t.Parallel()
	tests := []struct {
		etag string
		want bool
	}{
		{etag: "9e107d9d372bb6826bd81d3542a419d6", want: true},
		{etag: "9E107D9D372BB6826BD81D3542A419D6", want: true},
		{etag: "9e107d9d372bb6826bd81d3542a419d6-3", want: false},
		{etag: "9e107d9d372bb6826bd81d3542a419dz", want: false},
		{etag: "", want: false},
	}
	for _, tt := range tests {
		if got := IsContentETag(tt.etag); got != tt.want {
			t.Errorf("IsContentETag(%q) = %v, want %v", tt.etag, got, tt.want)
		}
	}
}
