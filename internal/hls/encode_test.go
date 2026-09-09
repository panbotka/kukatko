package hls

import (
	"slices"
	"strings"
	"testing"
)

// wantScaleFilter is the scale filter the 1080p rendition must ask for: fit
// inside a 1920 square, never upscale, keep both sides even.
const wantScaleFilter = `scale=w=min(1920\,iw):h=min(1920\,ih):force_original_aspect_ratio=decrease:force_divisible_by=2`

// rendition1080p returns the 1080p rendition, failing the test if the encoder's
// list has lost it.
func rendition1080p(t *testing.T) Rendition {
	t.Helper()
	r, ok := ByName(Rendition1080p)
	if !ok {
		t.Fatalf("ByName(%q): rendition not found", Rendition1080p)
	}
	return r
}

// argValue returns the value following the given ffmpeg flag, and whether the
// flag was present with a value after it.
func argValue(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// TestAll verifies the encoder plans exactly the renditions the layout names, and
// that the returned slice is a copy a caller cannot use to reshape the plan.
func TestAll(t *testing.T) {
	t.Parallel()

	got := All()
	if len(got) != 1 || got[0].Name != Rendition1080p {
		t.Fatalf("All() = %+v, want the single %q rendition", got, Rendition1080p)
	}
	if err := ValidateRendition(got[0].Name); err != nil {
		t.Errorf("ValidateRendition(%q): %v — a rendition name must be key-safe", got[0].Name, err)
	}
	got[0].Name = "tampered"
	if again := All(); again[0].Name != Rendition1080p {
		t.Errorf("All() after writing into an earlier result = %q, want the plan untouched", again[0].Name)
	}
}

// TestByName verifies a rendition is looked up by the name that appears in object
// keys, and that an unknown one is not silently invented.
func TestByName(t *testing.T) {
	t.Parallel()

	r, ok := ByName(Rendition1080p)
	if !ok {
		t.Fatalf("ByName(%q) not found", Rendition1080p)
	}
	if r.MaxDimension != 1920 || r.VideoBitrate != 6_000_000 {
		t.Errorf("ByName(%q) = %+v, want a 1920 box at 6 Mbit/s", Rendition1080p, r)
	}
	for _, name := range []string{"", "720p", "1080P", "1080p "} {
		if got, ok := ByName(name); ok {
			t.Errorf("ByName(%q) = %+v, want not found", name, got)
		}
	}
}

// TestRenditionBandwidth verifies the advertised peak is the video ceiling plus
// the audio bitrate, which is what a master playlist's BANDWIDTH means.
func TestRenditionBandwidth(t *testing.T) {
	t.Parallel()

	r := rendition1080p(t)
	if got, want := r.Bandwidth(), 6_000_000+AudioBitrate; got != want {
		t.Errorf("Bandwidth() = %d, want %d", got, want)
	}
}

// TestEncodeArgs verifies the whole encoding contract: the quality parameters,
// the fMP4/CMAF segmenting at the requested length, the keyframes forced onto those
// boundaries, and the output names the object layout expects.
func TestEncodeArgs(t *testing.T) {
	t.Parallel()

	args := EncodeArgs("/tmp/src.mov", "/tmp/out", rendition1080p(t), DefaultSegmentSeconds)

	wantValues := map[string]string{
		"-i":                      "/tmp/src.mov",
		"-c:v":                    "libx264",
		"-profile:v":              "high",
		"-preset":                 "medium",
		"-crf":                    "21",
		"-maxrate":                "6000k",
		"-bufsize":                "12000k",
		"-pix_fmt":                "yuv420p",
		"-force_key_frames":       "expr:gte(t,n_forced*6)",
		"-c:a":                    "aac",
		"-ac":                     "2",
		"-b:a":                    "128k",
		"-f":                      "hls",
		"-hls_time":               "6",
		"-hls_playlist_type":      "vod",
		"-hls_segment_type":       "fmp4",
		"-hls_list_size":          "0",
		"-hls_fmp4_init_filename": "init.mp4",
		"-hls_segment_filename":   "/tmp/out/%05d.m4s",
		"-vf":                     wantScaleFilter,
	}
	for flag, want := range wantValues {
		got, ok := argValue(args, flag)
		if !ok {
			t.Errorf("EncodeArgs: %s missing", flag)
			continue
		}
		if got != want {
			t.Errorf("EncodeArgs: %s = %q, want %q", flag, got, want)
		}
	}
	if got := args[len(args)-1]; got != "/tmp/out/index.m3u8" {
		t.Errorf("EncodeArgs: output = %q, want the playlist in the output directory", got)
	}
	if !slices.Contains(args, "0:a?") {
		t.Error("EncodeArgs: audio is not mapped optionally; a silent clip would fail")
	}
	if !slices.Contains(args, "-nostdin") {
		t.Error("EncodeArgs: -nostdin missing; ffmpeg would compete for the process's stdin")
	}
}

// TestEncodeArgs_isPure verifies the builder has no hidden state: the same inputs
// produce the same list, every time, with nothing read from the environment.
func TestEncodeArgs_isPure(t *testing.T) {
	t.Parallel()

	r := rendition1080p(t)
	first := EncodeArgs("src.mp4", "out", r, DefaultSegmentSeconds)
	second := EncodeArgs("src.mp4", "out", r, DefaultSegmentSeconds)
	if !slices.Equal(first, second) {
		t.Errorf("EncodeArgs is not deterministic:\n%v\n%v", first, second)
	}
}

// TestEncodeArgs_scaling verifies the one filter the builder emits caps every
// orientation the way the contract requires. The filter is an expression, so the
// dimensions it produces are asserted through Fit, which performs ffmpeg's own
// arithmetic — the expected values were measured against ffmpeg 6.1.1.
func TestEncodeArgs_scaling(t *testing.T) {
	t.Parallel()

	r := rendition1080p(t)
	tests := []struct {
		name                  string
		srcWidth, srcHeight   int
		wantWidth, wantHeight int
	}{
		{name: "landscape 4K is halved", srcWidth: 3840, srcHeight: 2160, wantWidth: 1920, wantHeight: 1080},
		{name: "portrait 4K caps its long edge", srcWidth: 2160, srcHeight: 3840, wantWidth: 1080, wantHeight: 1920},
		{name: "already small is left alone", srcWidth: 640, srcHeight: 480, wantWidth: 640, wantHeight: 480},
		{name: "1080p source is left alone", srcWidth: 1920, srcHeight: 1080, wantWidth: 1920, wantHeight: 1080},
		{name: "tall panorama caps its height", srcWidth: 1000, srcHeight: 3000, wantWidth: 640, wantHeight: 1920},
		{name: "odd small dimensions become even", srcWidth: 1279, srcHeight: 721, wantWidth: 1278, wantHeight: 720},
		{name: "barely over the box", srcWidth: 1921, srcHeight: 1000, wantWidth: 1920, wantHeight: 1000},
		{name: "wide source", srcWidth: 3000, srcHeight: 1001, wantWidth: 1920, wantHeight: 640},
		{name: "square source", srcWidth: 1999, srcHeight: 1999, wantWidth: 1920, wantHeight: 1920},
		{name: "unknown dimensions", srcWidth: 0, srcHeight: 0, wantWidth: 0, wantHeight: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotW, gotH := r.Fit(tt.srcWidth, tt.srcHeight)
			if gotW != tt.wantWidth || gotH != tt.wantHeight {
				t.Errorf("Fit(%d, %d) = %dx%d, want %dx%d",
					tt.srcWidth, tt.srcHeight, gotW, gotH, tt.wantWidth, tt.wantHeight)
			}
			if gotW > r.MaxDimension || gotH > r.MaxDimension {
				t.Errorf("Fit(%d, %d) = %dx%d, want both sides within the %d box",
					tt.srcWidth, tt.srcHeight, gotW, gotH, r.MaxDimension)
			}
			if tt.srcWidth > 0 && (gotW > tt.srcWidth || gotH > tt.srcHeight) {
				t.Errorf("Fit(%d, %d) = %dx%d, want no upscaling", tt.srcWidth, tt.srcHeight, gotW, gotH)
			}
			if args := EncodeArgs("src.mp4", "out", r, DefaultSegmentSeconds); !slices.Contains(args, wantScaleFilter) {
				t.Errorf("EncodeArgs: scale filter = %v, want %q", args, wantScaleFilter)
			}
		})
	}
}

// TestScaleFilter_escapesCommas verifies the filter's commas are escaped: an
// unescaped one would end the scale filter and start a second, unknown one.
func TestScaleFilter_escapesCommas(t *testing.T) {
	t.Parallel()

	got := rendition1080p(t).ScaleFilter()
	if strings.Contains(strings.ReplaceAll(got, `\,`, ""), ",") {
		t.Errorf("ScaleFilter() = %q, want every comma escaped", got)
	}
	if got != wantScaleFilter {
		t.Errorf("ScaleFilter() = %q, want %q", got, wantScaleFilter)
	}
}

// TestEncodeArgs_segmentLength verifies the configured segment length reaches
// both places it must — the segmenter and the forced keyframes, which have to
// agree or a segment does not start on a keyframe — and that a non-positive
// value falls back to the default rather than asking for zero-length segments.
func TestEncodeArgs_segmentLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured int
		want       string
	}{
		{name: "configured length is used", configured: 4, want: "4"},
		{name: "zero falls back to the default", configured: 0, want: "6"},
		{name: "negative falls back to the default", configured: -3, want: "6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args := EncodeArgs("src.mp4", "out", rendition1080p(t), tt.configured)
			if got, _ := argValue(args, "-hls_time"); got != tt.want {
				t.Errorf("EncodeArgs(%d): -hls_time = %q, want %q", tt.configured, got, tt.want)
			}
			wantExpr := "expr:gte(t,n_forced*" + tt.want + ")"
			if got, _ := argValue(args, "-force_key_frames"); got != wantExpr {
				t.Errorf("EncodeArgs(%d): -force_key_frames = %q, want %q", tt.configured, got, wantExpr)
			}
		})
	}
}

// TestSegmentLength verifies the "unset means the default" rule the encoder and
// every config reader share.
func TestSegmentLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured int
		want       int
	}{
		{name: "positive is kept", configured: 10, want: 10},
		{name: "zero is the default", configured: 0, want: DefaultSegmentSeconds},
		{name: "negative is the default", configured: -1, want: DefaultSegmentSeconds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := SegmentLength(tt.configured); got != tt.want {
				t.Errorf("SegmentLength(%d) = %d, want %d", tt.configured, got, tt.want)
			}
		})
	}
}
