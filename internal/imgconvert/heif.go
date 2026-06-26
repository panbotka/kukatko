package imgconvert

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

const (
	// heifBinary is the external HEIC/HEIF decoder (from libheif) shelled out to.
	heifBinary = "heif-convert"
	// heifTimeout caps a single heif-convert invocation. The largest HEIC
	// originals decode in a few seconds; 30s is a generous upper bound that
	// still prevents a runaway subprocess from blocking indefinitely.
	heifTimeout = 30 * time.Second
	// heifQuality is the JPEG quality (1-100) passed to heif-convert. 92 keeps
	// the intermediate visually lossless relative to the top thumbnail tier.
	heifQuality = 92
)

// convertHEIC runs heif-convert against srcPath and returns the path of a
// freshly written temporary JPEG plus a once-only cleanup function. If
// heif-convert is not on PATH the returned error wraps ErrConverterMissing.
func convertHEIC(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath(heifBinary); err != nil {
		return "", nil, fmt.Errorf("%w: %s lookup: %w", ErrConverterMissing, heifBinary, err)
	}

	tmpPath, cleanup, err := createTempJPEG("kukatko-heic-*.jpg")
	if err != nil {
		return "", nil, err
	}

	cctx, cancel := context.WithTimeout(ctx, heifTimeout)
	defer cancel()

	// #nosec G204 -- srcPath is the caller-supplied path EnsureDecodable stat'ed
	// before dispatch; the remaining args are constant literals.
	cmd := exec.CommandContext(cctx, heifBinary,
		"-q", strconv.Itoa(heifQuality),
		srcPath, tmpPath,
	)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("imgconvert: %s %s: %w (output: %s)",
			heifBinary, filepath.Base(srcPath), runErr, string(out))
	}

	if err := requireNonEmpty(tmpPath, heifBinary); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmpPath, cleanup, nil
}

// requireNonEmpty returns an error if the file at path is missing or empty,
// labelling it with the producing tool's name for diagnostics.
func requireNonEmpty(path, tool string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("imgconvert: stat %s output: %w", tool, err)
	}
	if info.Size() == 0 {
		return fmt.Errorf("imgconvert: %s produced empty output", tool)
	}
	return nil
}
