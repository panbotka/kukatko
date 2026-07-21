package thumb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
)

// TestVipsArgs verifies the vipsthumbnail argument list for both modes: fit
// sizes use the shrink-only "WxH>" geometry, crop-square sizes add
// "--smartcrop centre", and the output carries the quality plus strip option.
func TestVipsArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec sizeSpec
		want []string
	}{
		{
			name: "fit shrink-only",
			spec: sizeSpec{Max: 720, Quality: 90, Mode: modeFit},
			want: []string{"/src.jpg", "--size", "720x720>", "-o", "/dst.jpg[Q=90,strip]"},
		},
		{
			name: "crop square centre",
			spec: sizeSpec{Max: 224, Quality: 85, Mode: modeCropSquare},
			want: []string{"/src.jpg", "--size", "224x224", "--smartcrop", "centre", "-o", "/dst.jpg[Q=85,strip]"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := vipsArgs("/src.jpg", "/dst.jpg", tt.spec)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("vipsArgs = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestVipsSupportsMime checks the directly-handled formats are accepted and
// everything else (HEIC/RAW/video) is rejected so it falls back to pure-Go.
func TestVipsSupportsMime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mime string
		want bool
	}{
		{"image/jpeg", true},
		{"image/jpg", true},
		{"image/png", true},
		{"image/webp", true},
		{"image/heic", false},
		{"image/x-canon-cr2", false},
		{"video/mp4", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := vipsSupportsMime(tt.mime); got != tt.want {
			t.Errorf("vipsSupportsMime(%q) = %v, want %v", tt.mime, got, tt.want)
		}
	}
}

// TestVipsAvailable resolves a present binary and rejects an absent one.
func TestVipsAvailable(t *testing.T) {
	t.Parallel()

	if !VipsAvailable("sh") {
		t.Error("VipsAvailable(sh) = false, want true (sh is on PATH)")
	}
	if VipsAvailable("definitely-not-a-real-binary-xyz") {
		t.Error("VipsAvailable(missing) = true, want false")
	}
}

// TestWithVips_missingBinaryIsNoOp confirms requesting a missing vips binary
// leaves the thumbnailer on the pure-Go engine instead of failing.
func TestWithVips_missingBinaryIsNoOp(t *testing.T) {
	t.Parallel()
	th, _ := newThumbnailer(t)
	WithVips("definitely-not-a-real-binary-xyz")(th)
	if th.usesVips() {
		t.Error("usesVips() = true after WithVips(missing), want false")
	}
}

// TestRunVips_respectsContextDeadline confirms a wedged vips is killed once the
// caller's context deadline passes, so it cannot block a worker goroutine
// forever. runVips just execs bin with args, so `sleep` stands in for a
// long-running vips process; the derived vipsTimeout never masks the earlier
// caller deadline.
func TestRunVips_respectsContextDeadline(t *testing.T) {
	t.Parallel()
	sleepBin, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = runVips(ctx, sleepBin, []string{"30"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("runVips returned nil for a deadline-exceeded process, want an error")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("runVips took %v; the deadline did not kill the process", elapsed)
	}
}

// writeFakeVips writes an executable shell script at a temp path and returns it,
// so the vips shell-out path can be exercised without libvips installed.
func writeFakeVips(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vipsthumbnail")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake vips: %v", err)
	}
	return path
}

// fakeVipsCopy is a stand-in for vipsthumbnail that copies the source image
// verbatim to the -o target (stripping the [opts] suffix). Because it does not
// resize, a thumbnail it produced keeps the SOURCE dimensions — a clean signal
// in tests that the vips path, not the pure-Go resize, ran.
const fakeVipsCopy = `#!/bin/sh
src="$1"
out=""
prev=""
for a in "$@"; do
  if [ "$prev" = "-o" ]; then out="$a"; fi
  prev="$a"
done
out="${out%%[*}"
cp "$src" "$out"
`

// fakeVipsFail always exits non-zero, simulating a vips invocation failure.
const fakeVipsFail = `#!/bin/sh
echo "boom" >&2
exit 1
`

// newVipsThumbnailer builds a Thumbnailer whose vips engine points at the given
// fake binary, over an isolated originals store and cache root.
func newVipsThumbnailer(t *testing.T, fakeBin string) (*Thumbnailer, *storage.FS) {
	t.Helper()
	root := t.TempDir()
	store, err := storage.NewFS(filepath.Join(root, "originals"))
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	th := New(store, filepath.Join(root, "cache"), WithVips(fakeBin), WithConcurrency(2))
	if !th.usesVips() {
		t.Fatalf("usesVips() = false, want true with fake binary %q", fakeBin)
	}
	return th, store
}

// TestGenerate_vipsEngineUsedForJPEG confirms a JPEG original routes through the
// vips engine: the fake binary copies the source unchanged, so the produced
// thumbnail keeps the source dimensions rather than the pure-Go resized ones.
func TestGenerate_vipsEngineUsedForJPEG(t *testing.T) {
	t.Parallel()
	th, store := newVipsThumbnailer(t, writeFakeVips(t, fakeVipsCopy))
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.FileMime = "image/jpeg"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 1200 || h != 900 {
		t.Errorf("vips fit_720 = %dx%d, want 1200x900 (source copied by fake vips)", w, h)
	}
}

// TestGenerate_vipsFailureFallsBackToGo confirms a failing vips invocation falls
// back to the pure-Go engine: Generate still succeeds and produces a correctly
// resized thumbnail.
func TestGenerate_vipsFailureFallsBackToGo(t *testing.T) {
	t.Parallel()
	th, store := newVipsThumbnailer(t, writeFakeVips(t, fakeVipsFail))
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.FileMime = "image/jpeg"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 720 || h != 540 {
		t.Errorf("fallback fit_720 = %dx%d, want 720x540 (pure-Go resize)", w, h)
	}
}

// TestGenerate_vipsSkippedForUnsupportedMime confirms a source the vips engine
// does not handle (here a non-raster MIME) bypasses vips and uses the pure-Go
// resize, which produces the bounded dimensions.
func TestGenerate_vipsSkippedForUnsupportedMime(t *testing.T) {
	t.Parallel()
	th, store := newVipsThumbnailer(t, writeFakeVips(t, fakeVipsCopy))
	// The bytes are JPEG (so pure-Go can decode), but the catalogued MIME is one
	// the vips engine does not claim, so it must not shell out.
	photo := storeJPEG(t, store, 1200, 900, 0)
	photo.FileMime = "image/tiff"

	got, err := th.Generate(context.Background(), photo, "fit_720")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	w, h := jpegBounds(t, got["fit_720"])
	if w != 720 || h != 540 {
		t.Errorf("unsupported-mime fit_720 = %dx%d, want 720x540 (pure-Go resize)", w, h)
	}
}
