package video

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"
)

const (
	// posterTimeout caps a single ffmpeg poster extraction. Decoding one frame is
	// fast even from a large clip; this guards against a wedged subprocess.
	posterTimeout = 60 * time.Second
	// posterQuality is ffmpeg's -q:v for the JPEG (2 best … 31 worst); 3 keeps the
	// poster visually faithful for the thumbnail tiers derived from it.
	posterQuality = 3
	// posterSampleQuality is ffmpeg's -q:v for a candidate frame. A candidate is
	// only ever measured, never shown, so it is encoded coarsely.
	posterSampleQuality = 5
)

// ExtractPoster decodes a representative frame of the video at srcPath to a
// temporary JPEG and returns its path plus a once-only cleanup function the
// caller MUST defer.
//
// The frame is chosen rather than fixed: a handful of candidates spread across
// the clip are decoded small and ranked (see posterOffsets and betterFrame), so
// a clip that opens on darkness — a night scene, a fade-in, a lens cap — does
// not end up as a black tile in the library. A clip that is dark or uniform all
// the way through still gets a poster, the best of its candidates; the rule
// never answers "no thumbnail".
//
// If ffmpeg is not on PATH the returned error wraps ErrFFmpegMissing; if ffmpeg
// runs but yields no frame at any offset the error wraps ErrPosterFailed. On
// error the returned cleanup is nil; on success it is non-nil.
func ExtractPoster(ctx context.Context, srcPath string) (string, func(), error) {
	if _, err := exec.LookPath(ffmpegBinary); err != nil {
		return "", nil, fmt.Errorf("%w: %w", ErrFFmpegMissing, err)
	}
	offsets := rankedPosterOffsets(ctx, srcPath)
	tmpPath, cleanup, err := createTempJPEG("kukatko-poster-*.jpg")
	if err != nil {
		return "", nil, err
	}
	if err := extractFrame(ctx, srcPath, tmpPath, offsets); err != nil {
		cleanup()
		return "", nil, err
	}
	return tmpPath, cleanup, nil
}

// scoredOffset pairs a sample offset with what the frame there looked like.
type scoredOffset struct {
	at    float64
	score frameScore
}

// rankedPosterOffsets samples the clip's candidate frames and returns the
// offsets to extract the poster from, best first. Sampling stops early at the
// first candidate that is plainly a picture (frameScore.goodEnough), so an
// ordinary clip costs one sample and a dark-opening one costs as many as it
// takes to get past the darkness.
//
// Candidates that yielded no frame keep their place at the end of the list, and
// offset 0 is always last, so a clip whose sampling produced nothing at all — no
// ffprobe, an exotic codec, an ffmpeg that failed on every seek — still falls
// back to the very first frame, which is what this function's predecessor did
// unconditionally.
func rankedPosterOffsets(ctx context.Context, srcPath string) []float64 {
	candidates := posterOffsets(probeDuration(ctx, srcPath))

	scored := make([]scoredOffset, 0, len(candidates))
	for _, at := range candidates {
		score, ok := sampleFrame(ctx, srcPath, at)
		if !ok {
			continue
		}
		scored = append(scored, scoredOffset{at: at, score: score})
		if score.goodEnough() {
			// The candidates are ordered by where they sit in the clip, so the first
			// one that is plainly a picture is the earliest such frame — and decoding
			// the rest could only replace it with an equally good one.
			break
		}
	}
	// Stable, so equally good candidates keep clip order and the choice is
	// reproducible — every path that re-derives a video's poster (ingest, a
	// thumbnail rebuild, face detection) must land on the same frame.
	sort.SliceStable(scored, func(i, j int) bool {
		return betterFrame(scored[i].score, scored[j].score)
	})

	ranked := make([]float64, 0, len(scored)+len(candidates)+1)
	for _, s := range scored {
		ranked = append(ranked, s.at)
	}
	ranked = append(ranked, candidates...)
	return dedupOffsets(append(ranked, 0))
}

// probeDuration returns the clip's duration, or 0 when it cannot be read —
// neither ffprobe nor exiftool installed, or a container that admits to no
// duration. The offsets degrade to fixed seconds then, so a missing prober costs
// a worse-spread sample, never a failure.
func probeDuration(ctx context.Context, srcPath string) time.Duration {
	meta, err := Probe(ctx, srcPath)
	if err != nil || meta.DurationMs == nil || *meta.DurationMs <= 0 {
		return 0
	}
	return time.Duration(*meta.DurationMs) * time.Millisecond
}

// sampleFrame decodes the candidate frame at the given offset into a small JPEG
// and scores it. ok is false when no frame could be read there — a seek past the
// end of the clip, or an ffmpeg that could not decode it — which the caller
// treats as "this candidate does not exist" rather than as an error.
func sampleFrame(ctx context.Context, srcPath string, at float64) (frameScore, bool) {
	tmpPath, cleanup, err := createTempJPEG("kukatko-poster-sample-*.jpg")
	if err != nil {
		return frameScore{}, false
	}
	defer cleanup()

	if err := runFFmpegFrame(ctx, srcPath, sampleArgs(srcPath, tmpPath, at)); err != nil {
		return frameScore{}, false
	}
	if !nonEmptyFile(tmpPath) {
		return frameScore{}, false
	}
	img, err := decodeJPEG(tmpPath)
	if err != nil {
		return frameScore{}, false
	}
	return scoreFrame(img), true
}

// decodeJPEG decodes the JPEG at path. Candidates are written by ffmpeg's own
// JPEG encoder, so the stdlib decoder always suffices and no format sniffing is
// needed.
func decodeJPEG(path string) (image.Image, error) {
	file, err := os.Open(path) //nolint:gosec // G304: path is our own temp file.
	if err != nil {
		return nil, fmt.Errorf("video: open poster sample: %w", err)
	}
	defer func() { _ = file.Close() }()

	img, err := jpeg.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("video: decode poster sample: %w", err)
	}
	return img, nil
}

// extractFrame writes one decoded frame of srcPath to dstPath, trying the given
// offsets in order until one produces a non-empty file. It returns
// ErrPosterFailed (wrapped) when none of them does.
func extractFrame(ctx context.Context, srcPath, dstPath string, offsets []float64) error {
	var lastErr error
	for _, at := range offsets {
		if err := runFFmpegFrame(ctx, srcPath, posterArgs(srcPath, dstPath, at)); err != nil {
			lastErr = err
			continue
		}
		if nonEmptyFile(dstPath) {
			return nil
		}
	}
	if lastErr != nil {
		return fmt.Errorf("%w: %s: %w", ErrPosterFailed, filepath.Base(srcPath), lastErr)
	}
	return fmt.Errorf("%w: %s", ErrPosterFailed, filepath.Base(srcPath))
}

// posterArgs builds the ffmpeg argument list that writes a single JPEG frame of
// src, seeked to at seconds, to dst. It is standalone so the command
// construction can be unit-tested without executing ffmpeg. The input seek
// (-ss before -i) is the fast, keyframe-accurate form.
func posterArgs(src, dst string, at float64) []string {
	return []string{
		"-nostdin",
		"-y",
		"-ss", formatSeek(at),
		"-i", src,
		"-frames:v", "1",
		"-q:v", strconv.Itoa(posterQuality),
		dst,
	}
}

// sampleArgs builds the ffmpeg argument list for a candidate frame: the same
// single-frame seek as posterArgs, but scaled down to posterSampleWidth (never
// up — a clip narrower than that is left alone) and coarsely encoded, because
// the frame is only ever measured.
func sampleArgs(src, dst string, at float64) []string {
	return []string{
		"-nostdin",
		"-y",
		"-ss", formatSeek(at),
		"-i", src,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", posterSampleWidth),
		"-q:v", strconv.Itoa(posterSampleQuality),
		dst,
	}
}

// formatSeek renders a seek offset for ffmpeg's -ss in whole milliseconds, the
// resolution the offsets are computed at, and without exponent notation.
func formatSeek(at float64) string {
	return strconv.FormatFloat(at, 'f', 3, 64)
}

// runFFmpegFrame runs ffmpeg with args (built by posterArgs or sampleArgs). A
// non-nil error or an empty output signals the caller to try another offset.
func runFFmpegFrame(ctx context.Context, srcPath string, args []string) error {
	cctx, cancel := context.WithTimeout(ctx, posterTimeout)
	defer cancel()

	var stderr bytes.Buffer
	// #nosec G204 -- the input path is the caller-supplied file the ingest layer
	// staged; the output is our own temp file and the remaining args are constant
	// flags and computed numbers.
	cmd := exec.CommandContext(cctx, ffmpegBinary, args...)
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
