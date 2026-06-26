package imgconvert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// exiftoolBinary extracts the embedded JPEG preview from a RAW file.
	exiftoolBinary = "exiftool"
	// rawTimeout caps a single exiftool invocation. Reading an embedded preview
	// is cheap (no demosaic); 60s is a generous backstop on a slow device.
	rawTimeout = 60 * time.Second
)

// rawPreviewTags lists the exiftool binary tags that may hold a full-size
// embedded JPEG, in order of preference. Different vendors use different tags:
// most store PreviewImage, while some (e.g. certain Nikon/Sony bodies) only
// populate JpgFromRaw. ThumbnailImage is a small last resort that still beats a
// full demosaic.
var rawPreviewTags = []string{"-PreviewImage", "-JpgFromRaw", "-ThumbnailImage"}

// convertRAW extracts the largest available embedded JPEG preview from the RAW
// file at srcPath using exiftool, writes it to a temporary JPEG, and returns
// the path plus a once-only cleanup function. It deliberately avoids a full
// demosaic. If exiftool is not on PATH the returned error wraps
// ErrConverterMissing; if no preview tag yields data it wraps
// ErrNoEmbeddedPreview.
func convertRAW(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath(exiftoolBinary); err != nil {
		return "", nil, fmt.Errorf("%w: %s lookup: %w", ErrConverterMissing, exiftoolBinary, err)
	}

	tmpPath, cleanup, err := createTempJPEG("kukatko-raw-*.jpg")
	if err != nil {
		return "", nil, err
	}

	if err := extractPreview(ctx, srcPath, tmpPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmpPath, cleanup, nil
}

// extractPreview tries each tag in rawPreviewTags in turn, writing the first
// non-empty result to dstPath. It returns ErrNoEmbeddedPreview (wrapped) when
// every tag yields nothing.
func extractPreview(ctx context.Context, srcPath, dstPath string) error {
	for _, tag := range rawPreviewTags {
		n, err := runExiftoolToFile(ctx, srcPath, tag, dstPath)
		if err == nil && n > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNoEmbeddedPreview, filepath.Base(srcPath))
}

// runExiftoolToFile runs `exiftool -b <tag> <src>` with stdout streamed
// directly to dstPath (truncating it first), so an arbitrarily large preview is
// never buffered in memory. It returns the number of bytes written. A non-nil
// error or a zero byte count signals the caller to try the next tag.
func runExiftoolToFile(ctx context.Context, srcPath, tag, dstPath string) (int64, error) {
	cctx, cancel := context.WithTimeout(ctx, rawTimeout)
	defer cancel()

	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // G304: dstPath is our own temp file.
	if err != nil {
		return 0, fmt.Errorf("imgconvert: open preview temp: %w", err)
	}
	defer func() { _ = out.Close() }()

	var stderr bytes.Buffer
	// #nosec G204 -- srcPath is the caller-supplied path EnsureDecodable stat'ed
	// before dispatch; tag is from the constant rawPreviewTags whitelist.
	cmd := exec.CommandContext(cctx, exiftoolBinary, "-b", tag, srcPath)
	cmd.Stdout = out
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return 0, fmt.Errorf("imgconvert: %s %s %s: %w (stderr: %s)",
			exiftoolBinary, tag, filepath.Base(srcPath), runErr, stderr.String())
	}

	info, statErr := out.Stat()
	if statErr != nil {
		return 0, fmt.Errorf("imgconvert: stat preview temp: %w", statErr)
	}
	return info.Size(), nil
}
