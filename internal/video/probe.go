package video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// probeTimeout caps a single ffprobe/exiftool invocation. Reading container
// metadata is cheap; this is a generous backstop against a wedged process.
const probeTimeout = 30 * time.Second

// creationTimeLayouts lists the timestamp formats ffprobe may emit for a
// container's creation_time tag, tried in order.
var creationTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
}

// iso6709NumberRe matches the signed decimal components of an ISO 6709 location
// string such as "+37.3318-122.0312+010.000/" (latitude, longitude, altitude).
var iso6709NumberRe = regexp.MustCompile(`[+-]\d+(?:\.\d+)?`)

// Probe reads the metadata of the video at path. It prefers ffprobe and falls
// back to exiftool when ffprobe is unavailable, returning ErrNoMetadataTool when
// neither tool is installed. A readable video with sparse metadata yields a
// Metadata with zero values for the missing fields and a nil error.
func Probe(ctx context.Context, path string) (Metadata, error) {
	if path == "" {
		return Metadata{}, errors.New("video: path must not be empty")
	}
	if FFprobeAvailable() {
		if meta, err := probeWithFFprobe(ctx, path); err == nil {
			return meta, nil
		}
	}
	if exiftoolAvailable() {
		return probeWithExiftool(ctx, path)
	}
	return Metadata{}, ErrFFprobeMissing
}

// ffprobeArgs builds the ffprobe argument list that prints the format and stream
// metadata of path as a single JSON document. It is a standalone function so the
// command construction can be unit-tested without executing ffprobe.
func ffprobeArgs(path string) []string {
	return []string{
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"--", path,
	}
}

// probeWithFFprobe runs ffprobe against path and parses its JSON output into
// Metadata. It returns an error if the process fails or its output is not the
// expected JSON.
func probeWithFFprobe(ctx context.Context, path string) (Metadata, error) {
	cctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	// #nosec G204 -- path is the caller-supplied file the ingest layer staged;
	// the remaining arguments are constant flags.
	cmd := exec.CommandContext(cctx, ffprobeBinary, ffprobeArgs(path)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Metadata{}, fmt.Errorf("video: run ffprobe: %w (stderr: %s)", err, stderr.String())
	}
	return parseFFprobe(stdout.Bytes())
}

// ffprobeOutput is the subset of ffprobe's `-print_format json` document the
// probe consumes.
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

// ffprobeStream is one entry of the ffprobe "streams" array.
type ffprobeStream struct {
	CodecType    string `json:"codec_type"`
	CodecName    string `json:"codec_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	AvgFrameRate string `json:"avg_frame_rate"`
	RFrameRate   string `json:"r_frame_rate"`
}

// ffprobeFormat is the ffprobe "format" object.
type ffprobeFormat struct {
	Duration string            `json:"duration"`
	Tags     map[string]string `json:"tags"`
}

// parseFFprobe maps an ffprobe JSON document onto Metadata, retaining the whole
// document verbatim in Metadata.Raw. It returns an error only when the bytes are
// not valid JSON.
func parseFFprobe(data []byte) (Metadata, error) {
	var out ffprobeOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return Metadata{}, fmt.Errorf("video: parse ffprobe json: %w", err)
	}
	meta := Metadata{Raw: rawDocument(data)}
	applyFFprobeStreams(&meta, out.Streams)
	applyFFprobeFormat(&meta, out.Format)
	return meta, nil
}

// rawDocument decodes data into a generic map for storage in photos.exif,
// returning nil when it cannot be decoded that way.
func rawDocument(data []byte) map[string]any {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	return doc
}

// applyFFprobeStreams fills the codec, dimension, frame-rate and audio fields
// from the first video and first audio stream.
func applyFFprobeStreams(meta *Metadata, streams []ffprobeStream) {
	for _, s := range streams {
		switch s.CodecType {
		case "video":
			if meta.VideoCodec == "" {
				applyVideoStream(meta, s)
			}
		case "audio":
			if !meta.HasAudio {
				meta.HasAudio = true
				meta.AudioCodec = s.CodecName
			}
		}
	}
}

// applyVideoStream copies the codec, pixel dimensions and frame rate of a video
// stream into meta.
func applyVideoStream(meta *Metadata, s ffprobeStream) {
	meta.VideoCodec = s.CodecName
	meta.Width = s.Width
	meta.Height = s.Height
	if fps, ok := parseRational(s.AvgFrameRate); ok {
		meta.FPS = &fps
	} else if fps, ok := parseRational(s.RFrameRate); ok {
		meta.FPS = &fps
	}
}

// applyFFprobeFormat fills the duration, creation time and GPS location from the
// container's format object.
func applyFFprobeFormat(meta *Metadata, format ffprobeFormat) {
	if ms, ok := durationToMs(format.Duration); ok {
		meta.DurationMs = &ms
	}
	if when, ok := parseCreationTime(format.Tags["creation_time"]); ok {
		meta.TakenAt = &when
	}
	applyLocation(meta, locationTag(format.Tags))
}

// locationTag returns the first non-empty ISO 6709 location value among the tag
// keys cameras and phones use for it.
func locationTag(tags map[string]string) string {
	for _, key := range []string{"location", "location-eng", "com.apple.quicktime.location.ISO6709"} {
		if v := strings.TrimSpace(tags[key]); v != "" {
			return v
		}
	}
	return ""
}

// applyLocation parses an ISO 6709 location string ("+lat+lng+alt/") into the
// decimal latitude, longitude and (optional) altitude fields of meta.
func applyLocation(meta *Metadata, iso string) {
	if iso == "" {
		return
	}
	nums := iso6709NumberRe.FindAllString(iso, 3)
	if len(nums) < 2 {
		return
	}
	if lat, err := strconv.ParseFloat(nums[0], 64); err == nil {
		meta.Lat = &lat
	}
	if lng, err := strconv.ParseFloat(nums[1], 64); err == nil {
		meta.Lng = &lng
	}
	if len(nums) >= 3 {
		if alt, err := strconv.ParseFloat(nums[2], 64); err == nil {
			meta.Altitude = &alt
		}
	}
}

// parseRational parses an ffprobe rational string "num/den" into its float
// value, returning false for a malformed string or a zero denominator (ffprobe's
// "0/0" sentinel for an unknown rate).
func parseRational(s string) (float64, bool) {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		return 0, false
	}
	numerator, err1 := strconv.ParseFloat(num, 64)
	denominator, err2 := strconv.ParseFloat(den, 64)
	if err1 != nil || err2 != nil || denominator == 0 {
		return 0, false
	}
	return numerator / denominator, true
}

// durationToMs converts an ffprobe duration string in seconds ("5.312000") into
// rounded milliseconds, returning false for an empty or non-positive value.
func durationToMs(s string) (int, bool) {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || seconds <= 0 {
		return 0, false
	}
	return int(math.Round(seconds * 1000)), true
}

// parseCreationTime parses a container creation_time tag against the known
// layouts, returning the time and true on the first match.
func parseCreationTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range creationTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
