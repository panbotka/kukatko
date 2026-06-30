package thumb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/panbotka/kukatko/internal/photos"
)

// defaultVipsBinary is the vipsthumbnail executable resolved on PATH when no
// override is configured.
const defaultVipsBinary = "vipsthumbnail"

// mimeJPEG is the canonical JPEG MIME type, named to keep the supported-format
// check and tests in sync.
const mimeJPEG = "image/jpeg"

// VipsAvailable reports whether the vipsthumbnail binary named by bin (or the
// default "vipsthumbnail" when bin is empty) is resolvable on PATH. The cmd
// layer calls it once at startup so it can log whether the requested vips engine
// is actually usable; a Thumbnailer built with WithVips degrades to the pure-Go
// engine on its own when the binary is missing.
func VipsAvailable(bin string) bool {
	if bin == "" {
		bin = defaultVipsBinary
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// WithVips enables the vipsthumbnail shell-out engine using the binary named by
// bin (resolved on PATH; the default "vipsthumbnail" is used when bin is empty).
// When the binary cannot be found the option is a no-op and the pure-Go engine
// stays in place, so requesting vips on a host without libvips degrades to
// pure-Go rather than failing.
//
// The vips engine is used only for JPEG/PNG/WebP originals, where it is markedly
// faster and far lower-memory than the pure-Go decode+resize on large images.
// Every other source (HEIC/RAW/video, which need the imgconvert pre-decode) and
// any vips invocation failure fall back to the pure-Go path per photo, so the
// rendered output and the public contract are unchanged — only speed differs.
func WithVips(bin string) Option {
	return func(t *Thumbnailer) {
		if bin == "" {
			bin = defaultVipsBinary
		}
		if resolved, err := exec.LookPath(bin); err == nil {
			t.vipsBin = resolved
		}
	}
}

// usesVips reports whether the vips engine is active (a binary was resolved).
func (t *Thumbnailer) usesVips() bool {
	return t.vipsBin != ""
}

// vipsSupportsMime reports whether the vips engine handles a source of the given
// MIME type directly. Only the pure-Go-decodable raster formats are accepted;
// everything else (HEIC/RAW/video) is left to the pure-Go path, which routes it
// through imgconvert first.
func vipsSupportsMime(mime string) bool {
	switch mime {
	case mimeJPEG, "image/jpg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

// tryVips renders every size in needed for photo through the vips engine,
// returning true when it produced them all. It returns false — leaving the
// caller to fall back to the pure-Go engine — when the engine is disabled, the
// source is not a vips-handled format, or any vips invocation fails. The
// original is read directly by vips; orientation is applied by vipsthumbnail's
// built-in EXIF autorotation (the same orientation Kukátko stored at import), so
// output matches the pure-Go engine.
func (t *Thumbnailer) tryVips(
	ctx context.Context, photo photos.Photo, needed []string, result map[string]string,
) bool {
	if !t.usesVips() || !vipsSupportsMime(photo.FileMime) {
		return false
	}
	src := t.originals.AbsPath(photo.FilePath)
	group, gctx := errgroup.WithContext(ctx)
	group.SetLimit(t.workers)
	for _, name := range needed {
		group.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			return t.vipsWriteSize(gctx, src, name, result[name])
		})
	}
	return group.Wait() == nil
}

// vipsWriteSize renders one size of src into absPath via vipsthumbnail, writing
// to a sibling temp file and renaming it into place so no half-written thumbnail
// is ever observed (matching the pure-Go atomic write).
func (t *Thumbnailer) vipsWriteSize(ctx context.Context, src, name, absPath string) error {
	start := time.Now()
	tmpPath, cleanup, err := reserveVipsTemp(absPath)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := runVips(ctx, t.vipsBin, vipsArgs(src, tmpPath, sizes[name])); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, filePerm); err != nil {
		return fmt.Errorf("thumb: chmod vips temp: %w", err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		return fmt.Errorf("thumb: rename vips temp: %w", err)
	}
	t.observer.ObserveThumbnail(time.Since(start))
	return nil
}

// reserveVipsTemp creates the destination directory and a unique sibling temp
// path ending in .jpg (so vipsthumbnail selects the JPEG encoder from the
// extension). The empty placeholder file is removed before vips runs so vips
// writes a fresh file; the returned cleanup removes the temp if it survives an
// error. It returns the temp path and a once-only cleanup.
func reserveVipsTemp(absPath string) (tmpPath string, cleanup func(), err error) {
	dir := filepath.Dir(absPath)
	if mkErr := os.MkdirAll(dir, dirPerm); mkErr != nil {
		return "", nil, fmt.Errorf("thumb: create cache dir %s: %w", dir, mkErr)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(absPath)+".vips-*.jpg")
	if err != nil {
		return "", nil, fmt.Errorf("thumb: create vips temp: %w", err)
	}
	tmpPath = tmp.Name()
	_ = tmp.Close()
	// Remove the placeholder so vipsthumbnail writes a clean file at this path.
	if rmErr := os.Remove(tmpPath); rmErr != nil {
		return "", nil, fmt.Errorf("thumb: clear vips temp: %w", rmErr)
	}
	return tmpPath, func() { _ = os.Remove(tmpPath) }, nil
}

// runVips executes vipsthumbnail with args, capturing stderr for diagnostics.
func runVips(ctx context.Context, bin string, args []string) error {
	var stderr bytes.Buffer
	// #nosec G204 -- bin is an operator-configured executable resolved on PATH at
	// construction; args are validated registry sizes and trusted storage paths.
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("thumb: vipsthumbnail: %w (stderr: %s)", err, stderr.String())
	}
	return nil
}

// vipsArgs builds the vipsthumbnail argument list that renders src into dst per
// spec. It is a pure function so the command construction is unit-testable
// without the binary. The output carries the JPEG quality and strips metadata
// ([Q=…,strip]); fit sizes use the "WxH>" geometry to only shrink (never
// upscale), while crop-square sizes use --smartcrop centre to fill an exact box.
func vipsArgs(src, dst string, spec sizeSpec) []string {
	out := dst + "[Q=" + strconv.Itoa(spec.Quality) + ",strip]"
	args := []string{src, "--size", vipsSizeToken(spec)}
	if spec.Mode == modeCropSquare {
		args = append(args, "--smartcrop", "centre")
	}
	return append(args, "-o", out)
}

// vipsSizeToken returns the vipsthumbnail --size geometry for spec: a
// shrink-only "WxH>" box for fit mode (matching the pure-Go no-upscale rule) or
// an exact "WxH" box for crop-square mode (smartcrop fills it, upscaling small
// sources just as the pure-Go centre-crop renders side×side).
func vipsSizeToken(spec sizeSpec) string {
	s := strconv.Itoa(spec.Max)
	box := s + "x" + s
	if spec.Mode == modeFit {
		return box + ">"
	}
	return box
}
