package hls

import (
	"path/filepath"
	"strconv"
)

// DefaultSegmentSeconds is the length one media segment is cut to unless the
// instance configures another. Six seconds is the usual VOD compromise: short
// enough that a player starts playing quickly and can switch rendition without a
// long commitment, long enough that a two-hour video does not turn into thousands
// of objects in the store. It is also the interval keyframes are forced onto, so
// a segment always starts with one.
const DefaultSegmentSeconds = 6

// EncodePlaylistName is the media playlist ffmpeg writes next to the segments it
// produces. It is a scratch file: the encoder reads it, rewrites its URIs and
// throws it away — nothing under this name is ever uploaded, which is why
// ValidateName refuses it.
const EncodePlaylistName = "index.m3u8"

// AudioBitrate is the constant audio bitrate of every rendition, in bits per
// second. Audio is not what a rendition varies (a viewer who drops to 720p still
// wants intelligible sound), so it is a package constant rather than a field of
// Rendition, and it is part of the bandwidth a rendition advertises.
const AudioBitrate = 128_000

// The encoding parameters, fixed for every rendition. They are a quality
// decision, not a tuning knob: H.264 high profile in yuv420p is what every
// browser and iOS device can decode; preset medium buys noticeably better
// compression than the veryfast used for the throwaway on-the-fly transcode,
// which is worth it for output that is encoded once and served many times; CRF 21
// is visually transparent enough for family video while staying well under the
// bitrate ceiling on ordinary footage; and AAC-LC stereo is the audio codec HLS
// players are guaranteed to have.
const (
	videoCodec     = "libx264"
	videoProfile   = "high"
	videoPreset    = "medium"
	videoCRF       = "21"
	pixelFormat    = "yuv420p"
	audioCodec     = "aac"
	audioChannels  = "2"
	segmentPattern = "%05d.m4s"
	// bufsizeFactor sizes the rate-control buffer as a multiple of the bitrate
	// ceiling. Two seconds' worth is x264's own convention: it lets a hard scene
	// overshoot briefly instead of being crushed, while still holding the average
	// at the ceiling.
	bufsizeFactor = 2
)

// Rendition describes one quality level a video is encoded into: the name that
// becomes a path segment in the object layout, the square box its picture is
// fitted into, the video bitrate ceiling, and the RFC 6381 codecs string the
// master playlist advertises for it.
//
// MaxDimension is deliberately one number rather than a width and a height. A
// family archive holds as many portrait phone clips as landscape ones, and a
// 1920x1080 box would cap a portrait video at 1080x608 — a quarter of the pixels
// the same rendition gives a landscape one. Fitting inside a square treats both
// orientations alike: 1920 caps the long edge, whichever edge that is.
type Rendition struct {
	// Name is the rendition's name, e.g. 1080p. It appears verbatim in object
	// keys, so it must satisfy ValidateRendition.
	Name string
	// MaxDimension is the side of the square box the picture is fitted inside, in
	// pixels. Neither output dimension exceeds it and the source is never scaled
	// up.
	MaxDimension int
	// VideoBitrate is the ceiling for the video stream, in bits per second. It is
	// a ceiling, not a target: the encode is quality-driven (CRF) and only ever
	// reaches it on demanding footage.
	VideoBitrate int
	// Codecs is the RFC 6381 codecs string for this rendition's streams, as the
	// master playlist must advertise it so a player knows before fetching a
	// segment whether it can decode it.
	Codecs string
}

// renditions is the ordered list of renditions every video is encoded into,
// highest quality first. There is one today; the list is the point, because
// adding 720p must be an entry here plus the data it produces, never a rewrite of
// the code that reads it.
var renditions = []Rendition{
	{
		Name:         Rendition1080p,
		MaxDimension: 1920,
		VideoBitrate: 6_000_000,
		// H.264 high profile at level 4.0 (avc1.6400 28, 0x28 = 40 = level 4.0),
		// which covers 1920x1080 at 30 fps, plus AAC-LC (mp4a.40.2).
		Codecs: "avc1.640028,mp4a.40.2",
	},
}

// All returns the renditions a video is encoded into, highest quality first. The
// slice is a copy, so a caller cannot reshape the encoder's plan by writing into
// it.
func All() []Rendition {
	out := make([]Rendition, len(renditions))
	copy(out, renditions)
	return out
}

// ByName returns the rendition with the given name and reports whether it exists.
// It is how a request's path segment becomes a rendition: an unknown name is not
// an encoding this instance produces, whatever it looks like.
func ByName(name string) (Rendition, bool) {
	for _, r := range renditions {
		if r.Name == name {
			return r, true
		}
	}
	return Rendition{}, false
}

// Bandwidth returns the peak bandwidth this rendition can demand, in bits per
// second: the video ceiling plus the audio bitrate. It is what the master
// playlist's BANDWIDTH attribute must carry — the attribute is defined as the
// peak, not the average, because a player uses it to decide whether a variant
// will play without stalling.
func (r Rendition) Bandwidth() int {
	return r.VideoBitrate + AudioBitrate
}

// ScaleFilter returns the ffmpeg scale filter that fits the source inside this
// rendition's square box.
//
// The box is min(MaxDimension, iw) x min(MaxDimension, ih), so a source smaller
// than the box scales to itself: force_original_aspect_ratio=decrease would
// otherwise enlarge a small video until it touched the box, spending bitrate on
// pixels that were never filmed. Inside that box the aspect ratio is preserved
// and force_divisible_by=2 keeps both dimensions even, which yuv420p chroma
// subsampling requires. The commas are escaped because the string is a filtergraph,
// where an unescaped comma would start a second filter.
func (r Rendition) ScaleFilter() string {
	box := strconv.Itoa(r.MaxDimension)
	return "scale=w=min(" + box + `\,iw):h=min(` + box + `\,ih)` +
		":force_original_aspect_ratio=decrease:force_divisible_by=2"
}

// EncodeArgs returns the ffmpeg argument list that encodes the video at src into
// this rendition's HLS output inside outDir: the media playlist
// EncodePlaylistName, the initialisation segment InitName and the media segments
// 00000.m4s, 00001.m4s, … src is either a local path or a URL — ffmpeg opens
// both — and outDir must exist.
//
//	args := hls.EncodeArgs(src, dir, rendition, hls.DefaultSegmentSeconds)
//	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
//
// segmentSeconds is the length segments are cut to; a non-positive value means
// DefaultSegmentSeconds, so a caller reading it out of an unset configuration
// still asks for a playable encode rather than for segments of length zero.
//
// The function is pure: it creates no directory, probes nothing, reads no clock
// and never runs ffmpeg, so the whole plan is testable on a machine that has no
// ffmpeg installed. Audio is mapped optionally (0:a?) so a silent clip still
// encodes, and keyframes are forced onto multiples of the segment length so the
// segmenter can cut where it planned to instead of waiting for the next keyframe
// the encoder happened to emit.
func EncodeArgs(src, outDir string, r Rendition, segmentSeconds int) []string {
	segmentSeconds = SegmentLength(segmentSeconds)
	return []string{
		"-nostdin",
		"-y",
		"-i", src,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-vf", r.ScaleFilter(),
		"-c:v", videoCodec,
		"-profile:v", videoProfile,
		"-preset", videoPreset,
		"-crf", videoCRF,
		"-maxrate", kbits(r.VideoBitrate),
		"-bufsize", kbits(r.VideoBitrate * bufsizeFactor),
		"-pix_fmt", pixelFormat,
		"-force_key_frames", keyframeExpr(segmentSeconds),
		"-c:a", audioCodec,
		"-ac", audioChannels,
		"-b:a", kbits(AudioBitrate),
		"-f", "hls",
		"-hls_time", strconv.Itoa(segmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_segment_type", "fmp4",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments",
		"-hls_fmp4_init_filename", InitName,
		"-hls_segment_filename", filepath.Join(outDir, segmentPattern),
		filepath.Join(outDir, EncodePlaylistName),
	}
}

// keyframeExpr returns ffmpeg's -force_key_frames expression placing a keyframe
// at every whole multiple of segmentSeconds.
func keyframeExpr(segmentSeconds int) string {
	return "expr:gte(t,n_forced*" + strconv.Itoa(segmentSeconds) + ")"
}

// SegmentLength returns the segment length an encode should use given what the
// configuration asked for: the value itself when it is positive, and
// DefaultSegmentSeconds otherwise. It exists so "unset means the default" is
// decided once, here, rather than at every call site that reads a config field.
func SegmentLength(configured int) int {
	if configured <= 0 {
		return DefaultSegmentSeconds
	}
	return configured
}

// kbits renders a bitrate in bits per second the way ffmpeg's rate options are
// spelled, e.g. 6000000 becomes "6000k". Kukátko's bitrates are whole thousands,
// so the truncation is exact; a rate that is not is rounded down rather than
// silently becoming a different unit.
func kbits(bitsPerSecond int) string {
	return strconv.Itoa(bitsPerSecond/1000) + "k"
}

// Fit returns the picture dimensions this rendition encodes a srcWidth x
// srcHeight source into — the arithmetic ScaleFilter asks ffmpeg to perform,
// carried out in Go so the plan can be reasoned about, and tested, on a machine
// with no ffmpeg on it. A source with a non-positive dimension yields 0, 0.
//
// It reproduces ffmpeg's own rounding: the aspect-preserving dimension is rounded
// to the nearest multiple of two (that is what force_divisible_by does inside the
// fit) and the result is then floored to an even number. The values were measured
// against ffmpeg 6.1.1 — 3840x2160 → 1920x1080, 2160x3840 → 1080x1920,
// 1279x721 → 1278x720, 1921x1000 → 1920x1000 — and the tests hold this function
// to them.
//
// The encoder should still read the real dimensions off what it produced rather
// than advertise these: ffmpeg applies a source's rotation metadata before the
// filter runs, so a portrait clip stored as a rotated landscape one reaches the
// filter already upright and Fit, given the stored dimensions, would describe a
// picture nobody encoded.
func (r Rendition) Fit(srcWidth, srcHeight int) (width, height int) {
	if srcWidth <= 0 || srcHeight <= 0 {
		return 0, 0
	}
	srcW, srcH := int64(srcWidth), int64(srcHeight)
	boxW := int64(min(r.MaxDimension, srcWidth))
	boxH := int64(min(r.MaxDimension, srcHeight))
	// The width the box's height implies, and vice versa, each rounded to the
	// nearest even number; the smaller of box and implication wins, which is what
	// force_original_aspect_ratio=decrease means.
	fromHeight := 2 * ((boxH*srcW + srcH) / (2 * srcH))
	fromWidth := 2 * ((boxW*srcH + srcW) / (2 * srcW))
	return evenDown(min(boxW, fromHeight)), evenDown(min(boxH, fromWidth))
}

// evenDown returns value floored to an even number, never below two: yuv420p
// halves both dimensions for its chroma planes, so an odd — or zero — side is not
// a picture libx264 can encode.
func evenDown(value int64) int {
	if value < 2 {
		return 2
	}
	return int(value &^ 1)
}
