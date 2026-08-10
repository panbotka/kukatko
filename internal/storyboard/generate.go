package storyboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/video"
)

const (
	// spriteQuality is ffmpeg's -q:v for the sprite JPEG (2 best … 31 worst). A
	// 160 px preview tile tolerates more compression than a thumbnail, and the
	// sprite is fetched whole before the first preview shows.
	spriteQuality = 5
	// generateTimeout caps one ffmpeg run. The filter chain decodes the clip
	// end to end, so the ceiling is generous — but finite, so a wedged subprocess
	// fails the job instead of holding a worker slot forever.
	generateTimeout = 15 * time.Minute
	// dirPerm and filePerm match the storage and thumbnail layers' owner-only
	// permissions.
	dirPerm  = 0o750
	filePerm = 0o640
)

// Generator produces and reads storyboard sprites. It is safe for concurrent use:
// every sprite is written atomically to a path derived from its own file hash, so
// two workers racing on the same video converge on identical bytes.
type Generator struct {
	// originals materializes a stored video as a local file, because ffmpeg takes
	// a filename.
	originals storage.Storage
	// cacheDir is the configured cache root (storage.cache_path).
	cacheDir string
}

// New returns a Generator that reads originals through store and writes sprites
// under cacheDir (the configured storage.cache_path).
func New(store storage.Storage, cacheDir string) *Generator {
	return &Generator{originals: store, cacheDir: cacheDir}
}

// Path returns the absolute filesystem path of the sprite for the given file
// hash, whether or not it exists yet, or ErrInvalidHash for a malformed hash.
func (g *Generator) Path(hash string) (string, error) {
	rel, err := RelPath(hash)
	if err != nil {
		return "", err
	}
	return filepath.Join(g.cacheDir, filepath.FromSlash(rel)), nil
}

// Exists reports whether the sprite for hash has already been generated. It
// returns ErrInvalidHash for a malformed hash and a wrapped error for an I/O
// failure that is not "absent".
func (g *Generator) Exists(hash string) (bool, error) {
	abs, err := g.Path(hash)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("storyboard: stat %s: %w", abs, err)
	}
	return info.Mode().IsRegular() && info.Size() > 0, nil
}

// Open returns a reader over the generated sprite for hash. The caller owns the
// reader and must close it. It returns ErrNotGenerated when no sprite is cached —
// the ordinary answer for a video whose job has not run yet — or ErrInvalidHash
// for a malformed hash.
func (g *Generator) Open(hash string) (io.ReadCloser, error) {
	abs, err := g.Path(hash)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(abs) //nolint:gosec // G304: abs is built from a validated hex hash under the cache root.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrNotGenerated, hash)
		}
		return nil, fmt.Errorf("storyboard: opening sprite %s: %w", hash, err)
	}
	return file, nil
}

// Remove deletes the cached sprite for hash, leaving no derived media behind when
// its source video is purged. It is idempotent: a hash that never had a sprite is
// not an error. It returns ErrInvalidHash for a malformed hash, or the I/O error
// when the deletion itself fails.
func (g *Generator) Remove(hash string) error {
	abs, err := g.Path(hash)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("storyboard: removing sprite %s: %w", hash, err)
	}
	return nil
}

// Generate produces the sprite for the video stored at srcRelPath, keyed by its
// content hash and laid out by spec, and returns without doing anything when it
// is already cached — the idempotence a retried or re-enqueued job relies on.
//
// It materializes the original (ffmpeg needs a real file), runs one ffmpeg pass
// and writes the result atomically, so an interrupted run leaves no half-written
// sprite at the final path. It returns a wrapped video.ErrFFmpegMissing when
// ffmpeg is not installed, ErrGenerateFailed when ffmpeg ran but produced nothing
// usable, or ErrInvalidHash for a malformed hash.
func (g *Generator) Generate(ctx context.Context, hash, srcRelPath string, spec Spec) error {
	abs, err := g.Path(hash)
	if err != nil {
		return err
	}
	cached, err := g.Exists(hash)
	if err != nil {
		return err
	}
	if cached {
		return nil
	}
	if _, err := exec.LookPath(ffmpegBinary); err != nil {
		return fmt.Errorf("%w: %w", video.ErrFFmpegMissing, err)
	}
	srcPath, cleanup, err := g.originals.Materialize(ctx, srcRelPath)
	defer cleanup()
	if err != nil {
		return fmt.Errorf("storyboard: materializing %s: %w", srcRelPath, err)
	}
	return renderSprite(ctx, srcPath, abs, spec)
}

// ffmpegBinary is the tool that decodes the clip and tiles its frames. It is the
// same binary internal/video shells out to for posters and transcodes.
const ffmpegBinary = "ffmpeg"

// renderSprite runs ffmpeg over srcPath and installs the resulting sprite at
// dstPath atomically. The sprite is rendered to a temporary file first and only
// then renamed, so a concurrent reader never observes a partial JPEG and a failed
// run leaves the previous state untouched.
func renderSprite(ctx context.Context, srcPath, dstPath string, spec Spec) error {
	dir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("storyboard: creating cache dir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".sb-*.jpg")
	if err != nil {
		return fmt.Errorf("storyboard: creating temp sprite in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("storyboard: closing temp sprite: %w", err)
	}
	if err := runFFmpeg(ctx, srcPath, tmpPath, spec); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, filePerm); err != nil {
		return fmt.Errorf("storyboard: setting sprite permissions: %w", err)
	}
	if err := os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("storyboard: installing sprite %s: %w", dstPath, err)
	}
	return nil
}

// FFmpegArgs builds the ffmpeg argument list that renders the sprite for src into
// dst according to spec. It is exported (and pure) so the command construction is
// unit-tested without executing ffmpeg.
//
// The filter chain samples one frame every spec.IntervalMs (expressed as the
// rational fps 1000/interval so no float formatting is involved), scales each to
// the tile size and packs them row-major into one image. `-frames:v 1` keeps only
// the first full tile, which discards the frames a rounded interval may yield
// beyond the grid.
func FFmpegArgs(src, dst string, spec Spec) []string {
	filter := fmt.Sprintf(
		"fps=1000/%d,scale=%d:%d,tile=%dx%d",
		spec.IntervalMs, spec.TileWidth, spec.TileHeight, spec.Columns, spec.Rows,
	)
	return []string{
		"-nostdin",
		"-y",
		"-an",
		"-sn",
		"-i", src,
		"-vf", filter,
		"-frames:v", "1",
		"-q:v", strconv.Itoa(spriteQuality),
		dst,
	}
}

// runFFmpeg executes the sprite render and reports ErrGenerateFailed when the
// command succeeded but wrote nothing usable (an unreadable or zero-length clip),
// so the caller can tell "this video will never have a storyboard" from a
// transient failure worth retrying.
func runFFmpeg(ctx context.Context, srcPath, dstPath string, spec Spec) error {
	cctx, cancel := context.WithTimeout(ctx, generateTimeout)
	defer cancel()

	var stderr bytes.Buffer
	// #nosec G204 -- srcPath is a file this application stored or materialized,
	// dstPath is our own temp file, and the remaining args are constant flags and
	// integers from a computed Spec.
	cmd := exec.CommandContext(cctx, ffmpegBinary, FFmpegArgs(srcPath, dstPath, spec)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("storyboard: ffmpeg %s: %w (stderr: %s)",
			filepath.Base(srcPath), err, stderr.String())
	}
	info, err := os.Stat(dstPath)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("%w: %s", ErrGenerateFailed, filepath.Base(srcPath))
	}
	return nil
}
