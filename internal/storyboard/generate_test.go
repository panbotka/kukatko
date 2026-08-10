package storyboard

import (
	"errors"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

// testHash is a syntactically valid SHA256 hex digest used to key sprites in the
// tests; what it names is irrelevant to the cache layout.
const testHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// harness is a Generator wired to a real filesystem store, with both roots kept
// so a test can plant an original or inspect the cache.
type harness struct {
	gen      *Generator
	store    *storage.FS
	cacheDir string
}

// newHarness returns a Generator over a fresh filesystem store and cache under
// t.TempDir().
func newHarness(t *testing.T) harness {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	cacheDir := filepath.Join(root, "cache")
	return harness{gen: New(store, cacheDir), store: store, cacheDir: cacheDir}
}

// writeSprite plants a fake sprite at the generator's canonical cache path and
// returns its absolute path, standing in for a completed generation.
func (h harness) writeSprite(t *testing.T, hash string, data []byte) string {
	t.Helper()
	abs, err := h.gen.Path(hash)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(abs, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return abs
}

// synthesizeClip renders a seconds-long test pattern with ffmpeg and stores it in
// the harness's store, returning its store-relative path. It gives the end-to-end
// test a real, decodable video without checking a binary fixture into the repo.
func (h harness) synthesizeClip(t *testing.T, seconds int) string {
	t.Helper()
	abs := filepath.Join(t.TempDir(), "clip.mp4")
	cmd := exec.CommandContext(t.Context(), "ffmpeg",
		"-nostdin", "-y",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10:duration="+strconv.Itoa(seconds),
		"-pix_fmt", "yuv420p",
		abs,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot synthesize a test clip here: %v (%s)", err, out)
	}
	file, err := os.Open(abs)
	if err != nil {
		t.Fatalf("opening the synthesized clip: %v", err)
	}
	defer func() { _ = file.Close() }()
	stored, err := h.store.Store(t.Context(), file, time.Time{}, "clip.mp4")
	if err != nil {
		t.Fatalf("storing the synthesized clip: %v", err)
	}
	return stored.RelPath
}

// TestFFmpegArgs verifies the rendered command line: a rational fps derived from
// the interval, an exact tile scale, the grid, and a single output frame — the
// four things that decide whether the sprite matches the Spec the client is told.
func TestFFmpegArgs(t *testing.T) {
	t.Parallel()

	spec := Spec{Columns: 10, Rows: 2, Count: 20, TileWidth: 160, TileHeight: 90, IntervalMs: 1500}
	args := FFmpegArgs("/src.mp4", "/dst.jpg", spec)

	filterAt := slices.Index(args, "-vf")
	if filterAt == -1 || filterAt+1 >= len(args) {
		t.Fatalf("FFmpegArgs = %v, want a -vf filter", args)
	}
	want := "fps=1000/1500,scale=160:90,tile=10x2"
	if got := args[filterAt+1]; got != want {
		t.Errorf("filter = %q, want %q", got, want)
	}
	if got := args[len(args)-1]; got != "/dst.jpg" {
		t.Errorf("output = %q, want the destination last", got)
	}
	if !slices.Contains(args, "/src.mp4") {
		t.Errorf("FFmpegArgs = %v, want the source as an input", args)
	}
	for _, flag := range []string{"-nostdin", "-frames:v", "-an"} {
		if !slices.Contains(args, flag) {
			t.Errorf("FFmpegArgs = %v, want %s", args, flag)
		}
	}
}

// TestGenerator_Exists covers the three answers the status path branches on: no
// sprite, a truncated one (which must not count as generated), and a real one.
func TestGenerator_Exists(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	got, err := h.gen.Exists(testHash)
	if err != nil {
		t.Fatalf("Exists on empty cache: %v", err)
	}
	if got {
		t.Error("Exists on empty cache = true, want false")
	}

	h.writeSprite(t, testHash, nil)
	got, err = h.gen.Exists(testHash)
	if err != nil {
		t.Fatalf("Exists on empty file: %v", err)
	}
	if got {
		t.Error("Exists on a zero-length sprite = true, want false")
	}

	h.writeSprite(t, testHash, []byte("jpeg"))
	got, err = h.gen.Exists(testHash)
	if err != nil {
		t.Fatalf("Exists on a written sprite: %v", err)
	}
	if !got {
		t.Error("Exists on a written sprite = false, want true")
	}
}

// TestGenerator_Exists_invalidHash verifies a malformed hash is refused rather
// than resolved into some path outside the cache.
func TestGenerator_Exists_invalidHash(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	if _, err := h.gen.Exists("../escape"); !errors.Is(err, ErrInvalidHash) {
		t.Errorf("Exists(../escape) error = %v, want ErrInvalidHash", err)
	}
}

// TestGenerator_Open_notGenerated verifies the "not there yet" answer is the
// typed sentinel the HTTP layer turns into a 404 and the player into "no
// preview" — never an opaque I/O error.
func TestGenerator_Open_notGenerated(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	if _, err := h.gen.Open(testHash); !errors.Is(err, ErrNotGenerated) {
		t.Errorf("Open on empty cache error = %v, want ErrNotGenerated", err)
	}
}

// TestGenerator_OpenAndRemove verifies a generated sprite reads back byte for
// byte, that Remove deletes it, and that removing twice is not an error.
func TestGenerator_OpenAndRemove(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.writeSprite(t, testHash, []byte("sprite-bytes"))

	reader, err := h.gen.Open(testHash)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(data) != "sprite-bytes" {
		t.Errorf("Open read %q, want %q", data, "sprite-bytes")
	}

	if err := h.gen.Remove(testHash); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := h.gen.Open(testHash); !errors.Is(err, ErrNotGenerated) {
		t.Errorf("Open after Remove error = %v, want ErrNotGenerated", err)
	}
	if err := h.gen.Remove(testHash); err != nil {
		t.Errorf("second Remove = %v, want nil (idempotent)", err)
	}
}

// TestGenerator_Generate_skipsWhenCached verifies generation is idempotent: with
// a sprite already cached it returns without touching the source at all — the
// source named here does not exist, so any attempt to read it would fail.
func TestGenerator_Generate_skipsWhenCached(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	abs := h.writeSprite(t, testHash, []byte("already-here"))

	spec, err := Plan(10000, 1920, 1080)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := h.gen.Generate(t.Context(), testHash, "2026/01/missing.mp4", spec); err != nil {
		t.Fatalf("Generate on a cached sprite = %v, want nil", err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "already-here" {
		t.Errorf("cached sprite was rewritten: %q", data)
	}
}

// TestGenerator_Generate_invalidHash verifies a malformed hash fails before any
// external tool runs.
func TestGenerator_Generate_invalidHash(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	err := h.gen.Generate(t.Context(), "nope", "2026/01/clip.mp4", Spec{})
	if !errors.Is(err, ErrInvalidHash) {
		t.Errorf("Generate with a bad hash = %v, want ErrInvalidHash", err)
	}
}

// TestGenerator_Generate_missingOriginal verifies a source the store does not
// hold fails the job (so it retries or dead-letters) rather than leaving a
// half-written sprite behind.
func TestGenerator_Generate_missingOriginal(t *testing.T) {
	t.Parallel()

	if !video.FFmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	h := newHarness(t)
	spec, err := Plan(10000, 1920, 1080)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := h.gen.Generate(t.Context(), testHash, "2026/01/missing.mp4", spec); err == nil {
		t.Fatal("Generate over a missing original = nil, want an error")
	}
	cached, err := h.gen.Exists(testHash)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if cached {
		t.Error("a failed generation left a sprite behind")
	}
}

// TestGenerator_Generate_realVideo is the end-to-end render: it synthesizes a
// short clip with ffmpeg, generates its storyboard, and checks the JPEG that
// comes out is exactly the grid the Spec promised. It is the only assurance that
// the filter chain and the Spec handed to the client describe the same image.
func TestGenerator_Generate_realVideo(t *testing.T) {
	t.Parallel()

	if !video.FFmpegAvailable() {
		t.Skip("ffmpeg not installed")
	}
	h := newHarness(t)
	src := h.synthesizeClip(t, 4)

	spec, err := Plan(4000, 320, 240)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := h.gen.Generate(t.Context(), testHash, src, spec); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	abs, err := h.gen.Path(testHash)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !strings.HasPrefix(abs, filepath.Join(h.cacheDir, CacheSubdir)) {
		t.Errorf("sprite %q is not under the storyboard cache", abs)
	}
	assertSpriteGrid(t, abs, spec)

	// Generating again must be a no-op, not a re-encode.
	before, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if err := h.gen.Generate(t.Context(), testHash, src, spec); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	after, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("a second Generate rewrote the cached sprite")
	}
}

// assertSpriteGrid decodes the JPEG at absPath and fails unless its pixel
// dimensions are exactly the grid spec describes.
func assertSpriteGrid(t *testing.T, absPath string, spec Spec) {
	t.Helper()
	file, err := os.Open(absPath)
	if err != nil {
		t.Fatalf("opening the sprite: %v", err)
	}
	defer func() { _ = file.Close() }()
	cfg, err := jpeg.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decoding the sprite: %v", err)
	}
	wantW, wantH := spec.Columns*spec.TileWidth, spec.Rows*spec.TileHeight
	if cfg.Width != wantW || cfg.Height != wantH {
		t.Errorf("sprite = %dx%d, want %dx%d (%d×%d tiles of %dx%d)",
			cfg.Width, cfg.Height, wantW, wantH,
			spec.Columns, spec.Rows, spec.TileWidth, spec.TileHeight)
	}
}
