package video

import (
	"reflect"
	"testing"
	"time"
)

// TestFFprobeArgs verifies the ffprobe command is constructed with the JSON
// format/stream flags and the path terminated after "--".
func TestFFprobeArgs(t *testing.T) {
	t.Parallel()
	got := ffprobeArgs("/tmp/clip.mp4")
	want := []string{
		"-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", "--", "/tmp/clip.mp4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ffprobeArgs = %v, want %v", got, want)
	}
}

// TestParseFFprobe maps a representative ffprobe document onto Metadata: codecs,
// dimensions, frame rate, duration, creation time and GPS.
func TestParseFFprobe(t *testing.T) {
	t.Parallel()
	const doc = `{
		"streams": [
			{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"avg_frame_rate":"30000/1001"},
			{"codec_type":"audio","codec_name":"aac"}
		],
		"format": {
			"duration":"5.312000",
			"tags":{"creation_time":"2023-01-15T14:30:52.000000Z","location":"+37.3318-122.0312/"}
		}
	}`
	meta, err := parseFFprobe([]byte(doc))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if meta.VideoCodec != "h264" || meta.Width != 1280 || meta.Height != 720 {
		t.Errorf("video stream mismatch: %+v", meta)
	}
	if !meta.HasAudio || meta.AudioCodec != "aac" {
		t.Errorf("audio stream mismatch: %+v", meta)
	}
	if meta.FPS == nil || *meta.FPS < 29.9 || *meta.FPS > 30.0 {
		t.Errorf("FPS = %v, want ~29.97", meta.FPS)
	}
	if meta.DurationMs == nil || *meta.DurationMs != 5312 {
		t.Errorf("DurationMs = %v, want 5312", meta.DurationMs)
	}
	want := time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC)
	if meta.TakenAt == nil || !meta.TakenAt.Equal(want) {
		t.Errorf("TakenAt = %v, want %v", meta.TakenAt, want)
	}
	if meta.Lat == nil || *meta.Lat != 37.3318 || meta.Lng == nil || *meta.Lng != -122.0312 {
		t.Errorf("GPS mismatch: lat=%v lng=%v", meta.Lat, meta.Lng)
	}
	if meta.Raw == nil {
		t.Error("Raw document was not retained")
	}
}

// TestParseFFprobe_sparse verifies a probe with no streams/tags yields zero
// values without error.
func TestParseFFprobe_sparse(t *testing.T) {
	t.Parallel()
	meta, err := parseFFprobe([]byte(`{"streams":[],"format":{}}`))
	if err != nil {
		t.Fatalf("parseFFprobe: %v", err)
	}
	if meta.VideoCodec != "" || meta.HasAudio || meta.DurationMs != nil || meta.TakenAt != nil {
		t.Errorf("expected zero-value metadata, got %+v", meta)
	}
}

// TestParseFFprobe_invalidJSON verifies malformed output is an error.
func TestParseFFprobe_invalidJSON(t *testing.T) {
	t.Parallel()
	if _, err := parseFFprobe([]byte("not json")); err == nil {
		t.Error("parseFFprobe(invalid) = nil error, want failure")
	}
}

// TestParseRational covers valid rationals, the "0/0" unknown sentinel, and
// malformed input.
func TestParseRational(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"30/1", 30, true},
		{"30000/1001", 29.97002997002997, true},
		{"0/0", 0, false},
		{"25", 0, false},
		{"abc/1", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseRational(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("parseRational(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestDurationToMs covers second-to-millisecond conversion and rejection of
// empty or non-positive durations.
func TestDurationToMs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want int
		ok   bool
	}{
		{"5.312000", 5312, true},
		{"0.5", 500, true},
		{"10", 10000, true},
		{"0", 0, false},
		{"", 0, false},
		{"-3", 0, false},
	}
	for _, tt := range tests {
		got, ok := durationToMs(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("durationToMs(%q) = %v, %v; want %v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestParseCreationTime covers the timestamp layouts ffprobe may emit and the
// empty case.
func TestParseCreationTime(t *testing.T) {
	t.Parallel()
	want := time.Date(2023, 1, 15, 14, 30, 52, 0, time.UTC)
	for _, in := range []string{
		"2023-01-15T14:30:52.000000Z",
		"2023-01-15T14:30:52Z",
		"2023-01-15 14:30:52",
	} {
		got, ok := parseCreationTime(in)
		if !ok || !got.Equal(want) {
			t.Errorf("parseCreationTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	if _, ok := parseCreationTime(""); ok {
		t.Error("parseCreationTime(empty) ok = true, want false")
	}
}

// TestApplyLocation parses ISO 6709 location strings with and without altitude
// and ignores malformed input.
func TestApplyLocation(t *testing.T) {
	t.Parallel()

	var withAlt Metadata
	applyLocation(&withAlt, "+37.3318-122.0312+010.500/")
	if withAlt.Lat == nil || *withAlt.Lat != 37.3318 ||
		withAlt.Lng == nil || *withAlt.Lng != -122.0312 ||
		withAlt.Altitude == nil || *withAlt.Altitude != 10.5 {
		t.Errorf("with altitude mismatch: %+v", withAlt)
	}

	var noAlt Metadata
	applyLocation(&noAlt, "+37.3318-122.0312/")
	if noAlt.Lat == nil || noAlt.Lng == nil || noAlt.Altitude != nil {
		t.Errorf("no-altitude mismatch: %+v", noAlt)
	}

	var bad Metadata
	applyLocation(&bad, "garbage")
	if bad.Lat != nil || bad.Lng != nil {
		t.Errorf("malformed location set coordinates: %+v", bad)
	}
}
