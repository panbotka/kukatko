package video

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// posterTimeout caps a single ffmpeg poster extraction. Decoding one frame is
	// fast even from a large clip; this guards against a wedged subprocess.
	posterTimeout = 60 * time.Second
	// posterSeekSeconds is where the representative frame is taken from: a second
	// in skips black/fade-in intros while staying within even very short clips.
	posterSeekSeconds = 1
	// posterQuality is ffmpeg's -q:v for the JPEG (2 best … 31 worst); 3 keeps the
	// poster visually faithful for the thumbnail tiers derived from it.
	posterQuality = 3
)

// ExtractPoster decodes a representative frame of the video at srcPath to a
// temporary JPEG and returns its path plus a once-only cleanup function the
// caller MUST defer. The frame is taken ~1s in, falling back to the very first
// frame for clips shorter than that.
//
// If ffmpeg is not on PATH the returned error wraps ErrFFmpegMissing; if ffmpeg
// runs but yields no frame the error wraps ErrPosterFailed. On error the
// returned cleanup is nil; on success it is non-nil.
func ExtractPoster(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath(ffmpegBinary); err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrFFmpegMissing, err)
	}
	tmpPath, cleanup, err := createTempJPEG("kukatko-poster-*.jpg")
	if err != nil {
		return "", nil, err
	}
	if err := extractFrame(ctx, srcPath, tmpPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmpPath, cleanup, nil
}

// extractFrame writes one decoded frame of srcPath to dstPath, trying the
// poster offset first and falling back to the first frame when the clip is too
// short to seek that far. It returns ErrPosterFailed (wrapped) when no attempt
// produces a non-empty file.
func extractFrame(ctx context.Context, srcPath, dstPath string) error {
	if err := runFFmpegFrame(ctx, srcPath, dstPath, posterSeekSeconds); err == nil && nonEmptyFile(dstPath) {
		return nil
	}
	if err := runFFmpegFrame(ctx, srcPath, dstPath, 0); err != nil {
		return err
	}
	if !nonEmptyFile(dstPath) {
		return fmt.Errorf("%w: %s", ErrPosterFailed, filepath.Base(srcPath))
	}
	return nil
}

// posterArgs builds the ffmpeg argument list that writes a single JPEG frame of
// src, seeked seekSeconds in, to dst. It is standalone so the command
// construction can be unit-tested without executing ffmpeg. The input seek
// (-ss before -i) is the fast, keyframe-accurate form.
func posterArgs(src, dst string, seekSeconds int) []string {
	return []string{
		"-nostdin",
		"-y",
		"-ss", strconv.Itoa(seekSeconds),
		"-i", src,
		"-frames:v", "1",
		"-q:v", strconv.Itoa(posterQuality),
		dst,
	}
}

// runFFmpegFrame runs ffmpeg to extract one frame of srcPath at seekSeconds into
// dstPath (truncating it). A non-nil error or an empty output signals the caller
// to try another offset.
func runFFmpegFrame(ctx context.Context, srcPath, dstPath string, seekSeconds int) error {
	cctx, cancel := context.WithTimeout(ctx, posterTimeout)
	defer cancel()

	var stderr bytes.Buffer
	// #nosec G204 -- srcPath is the caller-supplied file the ingest layer staged;
	// dstPath is our own temp file and the remaining args are constant flags.
	cmd := exec.CommandContext(cctx, ffmpegBinary, posterArgs(srcPath, dstPath, seekSeconds)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("video: ffmpeg poster %s: %w (stderr: %s)",
			filepath.Base(srcPath), err, stderr.String())
	}
	return nil
}

// nonEmptyFile reports whether the file at path exists and has a non-zero size.
func nonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// createTempJPEG creates an empty temporary file matching pattern under
// os.TempDir() and closes it so ffmpeg can write to it. It returns the absolute
// path plus a once-only cleanup function.
func createTempJPEG(pattern string) (string, func(), error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("video: create temp jpeg: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := onceRemove(tmpPath)
	if closeErr := tmp.Close(); closeErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("video: close temp jpeg: %w", closeErr)
	}
	return tmpPath, cleanup, nil
}

// onceRemove returns a cleanup function that os.Removes path on its first call
// and is a no-op thereafter, satisfying the "safe to call multiple times"
// cleanup contract.
func onceRemove(path string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { _ = os.Remove(path) })
	}
}
