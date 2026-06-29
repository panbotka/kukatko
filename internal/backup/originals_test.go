package backup

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeFile writes data to a file under root, creating parent directories.
func writeFile(t *testing.T, root, rel, data string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func TestDiskOriginals_List(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "2026/01/a.jpg", "aaa")
	writeFile(t, root, "2026/02/b.png", "bb")
	// Files under the temporary upload dir must be skipped.
	writeFile(t, root, ".tmp/upload-123", "partial")

	originals, err := NewDiskOriginals(root).List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	sort.Slice(originals, func(i, j int) bool { return originals[i].Key < originals[j].Key })
	if len(originals) != 2 {
		t.Fatalf("List() returned %d originals, want 2: %+v", len(originals), originals)
	}
	if originals[0].Key != "2026/01/a.jpg" || originals[0].Size != 3 {
		t.Errorf("originals[0] = %+v, want 2026/01/a.jpg size 3", originals[0])
	}
	if originals[1].Key != "2026/02/b.png" || originals[1].Size != 2 {
		t.Errorf("originals[1] = %+v, want 2026/02/b.png size 2", originals[1])
	}
}

func TestDiskOriginals_List_missingRoot(t *testing.T) {
	t.Parallel()
	originals, err := NewDiskOriginals(filepath.Join(t.TempDir(), "does-not-exist")).List(context.Background())
	if err != nil {
		t.Fatalf("List() on missing root error = %v, want nil", err)
	}
	if len(originals) != 0 {
		t.Errorf("List() on missing root = %v, want empty", originals)
	}
}

func TestDiskOriginals_Open(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "2026/01/a.jpg", "hello")
	reader, err := NewDiskOriginals(root).Open(context.Background(), "2026/01/a.jpg")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("Open() content = %q, want hello", data)
	}
}

func TestDiskOriginals_Open_confined(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "secret-inside", "x")
	// A traversal key must be confined to the root, never escaping above it.
	if _, err := NewDiskOriginals(root).Open(context.Background(), "../../../etc/passwd"); err == nil {
		t.Error("Open() with a traversal key did not error")
	}
}

func TestDiskOriginals_Stat(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, root, "2026/01/a.jpg", "hello")
	disk := NewDiskOriginals(root)

	got, ok, err := disk.Stat(context.Background(), "2026/01/a.jpg")
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if !ok || got.Size != 5 {
		t.Errorf("Stat() = (%+v, %v), want present size 5", got, ok)
	}

	_, ok, err = disk.Stat(context.Background(), "2026/01/missing.jpg")
	if err != nil {
		t.Fatalf("Stat() missing error = %v, want nil", err)
	}
	if ok {
		t.Error("Stat() ok = true for a missing file")
	}
}

func TestDiskOriginals_Write(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disk := NewDiskOriginals(root)

	if err := disk.Write(context.Background(), "2026/03/new.jpg", strings.NewReader("payload")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	abs := filepath.Join(root, "2026", "03", "new.jpg")
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if string(data) != "payload" {
		t.Errorf("written content = %q, want payload", data)
	}
	// No partial file must be left in the temp directory after a successful write.
	entries, err := os.ReadDir(filepath.Join(root, tmpDirName))
	if err != nil {
		t.Fatalf("reading temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("temp dir holds %d leftover files after a successful write", len(entries))
	}
}

func TestDiskOriginals_Write_confined(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	disk := NewDiskOriginals(root)
	// A traversal key must be confined to the root, never escaping above it.
	if err := disk.Write(context.Background(), "../../escape.jpg", strings.NewReader("x")); err != nil {
		t.Fatalf("Write() with traversal key error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escape.jpg")); err != nil {
		t.Errorf("confined write did not land inside root: %v", err)
	}
}

func TestConfineKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want string
	}{
		{"2026/01/a.jpg", "2026/01/a.jpg"},
		{"/2026/01/a.jpg", "2026/01/a.jpg"},
		{"../../etc/passwd", "etc/passwd"},
		{"a/../b.jpg", "b.jpg"},
	}
	for _, tt := range tests {
		if got := confineKey(tt.key); got != tt.want {
			t.Errorf("confineKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}
