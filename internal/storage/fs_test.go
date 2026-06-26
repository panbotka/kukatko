package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestFS returns an FS rooted at a fresh temp directory for use in a test.
func newTestFS(t *testing.T) *FS {
	t.Helper()
	fs, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return fs
}

// hashOf returns the hex SHA256 digest of b, used to cross-check Store's hashing.
func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fixedTime is a stable timestamp used so date-layout assertions are
// deterministic regardless of when the test runs.
var fixedTime = time.Date(2024, time.May, 17, 10, 30, 0, 0, time.UTC)

func TestFSStore_hashAndLayout(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte("the quick brown fox\n")

	stored, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "fox.jpg")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	if want := hashOf(content); stored.Hash != want {
		t.Errorf("Hash = %s, want %s", stored.Hash, want)
	}
	if want := "2024/05/fox.jpg"; stored.RelPath != want {
		t.Errorf("RelPath = %s, want %s", stored.RelPath, want)
	}
	if stored.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", stored.Size, len(content))
	}
}

func TestFSStore_knownSHA256(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	// Independently known digest of "hello world".
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"

	stored, err := fs.Store(t.Context(), strings.NewReader("hello world"), fixedTime, "h.txt")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if stored.Hash != want {
		t.Errorf("Hash = %s, want %s", stored.Hash, want)
	}
}

func TestFSStore_zeroTimeUsesNow(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	stored, err := fs.Store(t.Context(), strings.NewReader("x"), time.Time{}, "z.dat")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	wantPrefix := time.Now().Format("2006/01") + "/"
	if !strings.HasPrefix(stored.RelPath, wantPrefix) {
		t.Errorf("RelPath = %s, want prefix %s", stored.RelPath, wantPrefix)
	}
}

func TestFSStore_readBack(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := bytes.Repeat([]byte("payload-"), 4096) // ~32 KiB, exercises streaming

	stored, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "big.bin")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	reader, err := fs.Open(t.Context(), stored.RelPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = reader.Close() }()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("read-back differs from written bytes (%d vs %d)", len(got), len(content))
	}
}

func TestFSStore_duplicateSameContent(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte("identical")

	first, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "dup.jpg")
	if err != nil {
		t.Fatalf("first Store: %v", err)
	}

	second, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "dup.jpg")
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Store error = %v, want ErrAlreadyExists", err)
	}
	if second.RelPath != first.RelPath {
		t.Errorf("duplicate RelPath = %s, want %s", second.RelPath, first.RelPath)
	}
	if second.Hash != first.Hash {
		t.Errorf("duplicate Hash = %s, want %s", second.Hash, first.Hash)
	}
}

func TestFSStore_collisionDifferentContent(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	first, err := fs.Store(t.Context(), strings.NewReader("content A"), fixedTime, "clash.jpg")
	if err != nil {
		t.Fatalf("first Store: %v", err)
	}
	second, err := fs.Store(t.Context(), strings.NewReader("content B"), fixedTime, "clash.jpg")
	if err != nil {
		t.Fatalf("second Store: %v", err)
	}

	if first.RelPath != "2024/05/clash.jpg" {
		t.Errorf("first RelPath = %s, want 2024/05/clash.jpg", first.RelPath)
	}
	if second.RelPath != "2024/05/clash_1.jpg" {
		t.Errorf("second RelPath = %s, want 2024/05/clash_1.jpg", second.RelPath)
	}
	assertContent(t, fs, first.RelPath, "content A")
	assertContent(t, fs, second.RelPath, "content B")
}

// assertContent fails the test unless the stored file at relPath holds want.
func assertContent(t *testing.T, fs *FS, relPath, want string) {
	t.Helper()
	reader, err := fs.Open(t.Context(), relPath)
	if err != nil {
		t.Fatalf("Open %s: %v", relPath, err)
	}
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll %s: %v", relPath, err)
	}
	if string(got) != want {
		t.Errorf("%s content = %q, want %q", relPath, got, want)
	}
}

func TestFSStore_concurrentIdenticalWrites(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte("racing writers store the same bytes")
	const writers = 16

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		newCount int
		paths    = make(map[string]struct{})
	)
	for range writers {
		wg.Go(func() {
			stored, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "race.jpg")
			if err != nil && !errors.Is(err, ErrAlreadyExists) {
				t.Errorf("Store: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			paths[stored.RelPath] = struct{}{}
			if err == nil {
				newCount++
			}
		})
	}
	wg.Wait()

	if newCount != 1 {
		t.Errorf("new-write count = %d, want exactly 1", newCount)
	}
	if len(paths) != 1 {
		t.Errorf("distinct RelPaths = %d (%v), want 1", len(paths), paths)
	}
	assertContent(t, fs, "2024/05/race.jpg", string(content))
	// No suffixed duplicates must have been created.
	if _, err := os.Stat(fs.AbsPath("2024/05/race_1.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected suffixed duplicate exists (stat err = %v)", err)
	}
}

func TestFSStore_emptyNameFallsBackToHash(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	stored, err := fs.Store(t.Context(), strings.NewReader("body"), fixedTime, "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if want := "2024/05/" + stored.Hash; stored.RelPath != want {
		t.Errorf("RelPath = %s, want %s", stored.RelPath, want)
	}
}

func TestFSStore_stripsDirectoryComponents(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	stored, err := fs.Store(t.Context(), strings.NewReader("body"), fixedTime, "../../etc/evil.jpg")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if want := "2024/05/evil.jpg"; stored.RelPath != want {
		t.Errorf("RelPath = %s, want %s", stored.RelPath, want)
	}
}

func TestFSStatAndDelete(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)
	content := []byte("to be deleted")

	stored, err := fs.Store(t.Context(), bytes.NewReader(content), fixedTime, "d.jpg")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	info, err := fs.Stat(t.Context(), stored.RelPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(len(content)) {
		t.Errorf("Stat size = %d, want %d", info.Size(), len(content))
	}

	if err := fs.Delete(t.Context(), stored.RelPath); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := fs.Stat(t.Context(), stored.RelPath); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Stat after Delete err = %v, want os.ErrNotExist", err)
	}
}

func TestFSOpen_notFound(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	_, err := fs.Open(t.Context(), "2024/05/missing.jpg")
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Open missing err = %v, want os.ErrNotExist", err)
	}
}

func TestFSOpen_invalidPath(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	for _, relPath := range []string{"", "/", "..", "../.."} {
		if _, err := fs.Open(t.Context(), relPath); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Open(%q) err = %v, want ErrInvalidPath", relPath, err)
		}
	}
}

func TestFSAbsPath_confinement(t *testing.T) {
	t.Parallel()
	fs := newTestFS(t)

	tests := []struct {
		name    string
		relPath string
		wantRel string
	}{
		{"plain", "2024/05/a.jpg", "2024/05/a.jpg"},
		{"escape", "../../etc/passwd", "etc/passwd"},
		{"leading slash", "/2024/05/a.jpg", "2024/05/a.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fs.AbsPath(tt.relPath)
			want := filepath.Join(fs.root, filepath.FromSlash(tt.wantRel))
			if got != want {
				t.Errorf("AbsPath(%q) = %q, want %q", tt.relPath, got, want)
			}
			if !strings.HasPrefix(got, fs.root) {
				t.Errorf("AbsPath(%q) = %q escapes root %q", tt.relPath, got, fs.root)
			}
		})
	}
}

func TestSuffixName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		attempt int
		want    string
	}{
		{"zero keeps name", "photo.jpg", 0, "photo.jpg"},
		{"first suffix", "photo.jpg", 1, "photo_1.jpg"},
		{"second suffix", "photo.jpg", 2, "photo_2.jpg"},
		{"no extension", "photo", 3, "photo_3"},
		{"dotted name", "a.tar.gz", 1, "a.tar_1.gz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := suffixName(tt.input, tt.attempt); got != tt.want {
				t.Errorf("suffixName(%q, %d) = %q, want %q", tt.input, tt.attempt, got, tt.want)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{"plain", "photo.jpg", "fb", "photo.jpg"},
		{"strips dir", "a/b/c.jpg", "fb", "c.jpg"},
		{"empty", "", "fb", "fb"},
		{"dot", ".", "fb", "fb"},
		{"dotdot", "..", "fb", "fb"},
		{"whitespace", "   ", "fb", "fb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeName(tt.input, tt.fallback); got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRelDirFor(t *testing.T) {
	t.Parallel()
	if got := relDirFor(fixedTime); got != "2024/05" {
		t.Errorf("relDirFor = %q, want 2024/05", got)
	}
	if got := relDirFor(time.Time{}); got != time.Now().Format("2006/01") {
		t.Errorf("relDirFor(zero) = %q, want current month", got)
	}
}
