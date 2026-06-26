// Package video handles Kukátko's video originals without CGO: it probes
// container metadata (duration, codecs, dimensions, frame rate, creation time,
// GPS) and extracts a representative poster frame by shelling out to the FFmpeg
// suite (`ffprobe` and `ffmpeg`).
//
// The poster frame is a plain JPEG fed straight back into the existing image
// pipeline — the thumbnailer and perceptual hasher decode it like any photo —
// so a video shows a real poster in the library grid and participates in
// semantic/face search through that frame. Metadata probing prefers ffprobe and
// falls back to exiftool (reusing the exif package) when ffprobe is absent.
//
// Both tools are optional at build time but required at runtime for video:
// callers detect their absence via FFmpegAvailable / the wrapped ErrFFmpegMissing
// sentinel and surface a clear, actionable error rather than storing a broken
// video entry.
package video

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// ffprobeBinary reads container/stream metadata on the primary probe path.
	ffprobeBinary = "ffprobe"
	// ffmpegBinary extracts the poster frame.
	ffmpegBinary = "ffmpeg"
	// exiftoolBinary is the metadata fallback when ffprobe is unavailable.
	exiftoolBinary = "exiftool"
)

// Sentinel errors so callers can branch with errors.Is — most importantly to
// distinguish "the external tool is not installed" (operator action required)
// from a transient processing failure.
var (
	// ErrFFmpegMissing is returned (wrapped) when ffmpeg, required to extract a
	// poster frame, is not installed on PATH. There is no fallback for poster
	// extraction, so this is fatal for a video upload.
	ErrFFmpegMissing = errors.New("video: ffmpeg not installed")
	// ErrFFprobeMissing is returned (wrapped) when ffprobe is not installed and
	// no metadata fallback (exiftool) is available either.
	ErrFFprobeMissing = errors.New("video: ffprobe not installed")
	// ErrNoMetadataTool is returned by Probe when neither ffprobe nor exiftool is
	// available to read the container's metadata.
	ErrNoMetadataTool = errors.New("video: no metadata tool available (need ffprobe or exiftool)")
	// ErrPosterFailed is returned when ffmpeg ran but produced no usable poster
	// frame (e.g. an unreadable or zero-length clip).
	ErrPosterFailed = errors.New("video: poster frame extraction failed")
)

// videoExts is the set of lowercased file extensions (with leading dot) that
// Kukátko treats as video. It covers the common containers PhotoPrism stores —
// mp4/mov/m4v and the live-photo clips — plus the widely seen prosumer formats.
var videoExts = map[string]struct{}{
	".mp4":  {},
	".m4v":  {},
	".mov":  {},
	".avi":  {},
	".mkv":  {},
	".webm": {},
	".hevc": {},
	".h264": {},
	".3gp":  {},
	".m2ts": {},
	".mts":  {},
	".mpg":  {},
	".mpeg": {},
	".wmv":  {},
	".flv":  {},
}

// Metadata is the normalised result of probing one video. Optional scalar values
// use pointers so a genuinely missing reading (nil) is distinguishable from a
// zero one; the shared fields (TakenAt, GPS, dimensions, Mime) map onto the same
// photos.Photo columns the image pipeline fills.
type Metadata struct {
	// TakenAt is the container creation time, nil when absent or unparseable.
	TakenAt *time.Time

	// Lat, Lng and Altitude are decimal GPS coordinates (degrees, metres), nil
	// when the container carries no usable location.
	Lat      *float64
	Lng      *float64
	Altitude *float64

	// Width and Height are the video's pixel dimensions, 0 when unknown.
	Width  int
	Height int

	// DurationMs is the clip length in milliseconds, nil when unknown.
	DurationMs *int
	// VideoCodec / AudioCodec name the primary streams' codecs (e.g. "h264",
	// "aac"); empty when a stream is absent or its codec is unknown.
	VideoCodec string
	AudioCodec string
	// HasAudio reports whether an audio stream is present.
	HasAudio bool
	// FPS is the average frame rate, nil when unknown.
	FPS *float64

	// Mime is the detected media type, e.g. "video/mp4"; may be empty (the
	// storage layer's content sniff fills it in then).
	Mime string

	// Raw is the full, JSON-able probe document (the ffprobe output, or the
	// exiftool tag object on the fallback path), nil when nothing was probed. It
	// is stored verbatim in photos.exif.
	Raw map[string]any
}

// IsVideoExt reports whether ext names a video format Kukátko ingests. The
// extension may include or omit the leading dot and is case-insensitive.
func IsVideoExt(ext string) bool {
	if ext == "" {
		return false
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	_, ok := videoExts[strings.ToLower(ext)]
	return ok
}

// IsVideoPath reports whether name's extension identifies it as a video.
func IsVideoPath(name string) bool {
	return IsVideoExt(filepath.Ext(name))
}

// FFmpegAvailable reports whether the ffmpeg binary is on PATH.
func FFmpegAvailable() bool {
	_, err := exec.LookPath(ffmpegBinary)
	return err == nil
}

// FFprobeAvailable reports whether the ffprobe binary is on PATH.
func FFprobeAvailable() bool {
	_, err := exec.LookPath(ffprobeBinary)
	return err == nil
}

// exiftoolAvailable reports whether the exiftool binary is on PATH.
func exiftoolAvailable() bool {
	_, err := exec.LookPath(exiftoolBinary)
	return err == nil
}
