package video

import "testing"

// TestApplyExiftoolVideoFields verifies the video-only fields are pulled from a
// numeric exiftool tag object.
func TestApplyExiftoolVideoFields(t *testing.T) {
	t.Parallel()

	obj := map[string]any{
		"Duration":       5.312,
		"VideoFrameRate": 29.97,
		"CompressorID":   "avc1",
		"AudioFormat":    "mp4a",
	}
	var meta Metadata
	applyExiftoolVideoFields(&meta, obj)

	if meta.DurationMs == nil || *meta.DurationMs != 5312 {
		t.Errorf("DurationMs = %v, want 5312", meta.DurationMs)
	}
	if meta.FPS == nil || *meta.FPS != 29.97 {
		t.Errorf("FPS = %v, want 29.97", meta.FPS)
	}
	if meta.VideoCodec != "avc1" {
		t.Errorf("VideoCodec = %q, want avc1", meta.VideoCodec)
	}
	if meta.AudioCodec != "mp4a" || !meta.HasAudio {
		t.Errorf("audio fields mismatch: codec=%q has=%v", meta.AudioCodec, meta.HasAudio)
	}
}

// TestApplyExiftoolVideoFields_audioByChannels verifies HasAudio is inferred
// from a channel count when no audio-format tag is present.
func TestApplyExiftoolVideoFields_audioByChannels(t *testing.T) {
	t.Parallel()
	var meta Metadata
	applyExiftoolVideoFields(&meta, map[string]any{"AudioChannels": 2.0})
	if !meta.HasAudio {
		t.Error("HasAudio = false, want true from AudioChannels")
	}
}

// TestApplyExiftoolVideoFields_nil verifies a nil tag object is a safe no-op.
func TestApplyExiftoolVideoFields_nil(t *testing.T) {
	t.Parallel()
	var meta Metadata
	applyExiftoolVideoFields(&meta, nil)
	if meta.DurationMs != nil || meta.HasAudio || meta.VideoCodec != "" {
		t.Errorf("nil object mutated metadata: %+v", meta)
	}
}

// TestTagString returns the first non-empty string and skips non-strings.
func TestTagString(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"A": "", "B": 3.0, "C": "  found  ", "D": "later"}
	if got := tagString(obj, "A", "B", "C", "D"); got != "found" {
		t.Errorf("tagString = %q, want found", got)
	}
	if got := tagString(obj, "missing"); got != "" {
		t.Errorf("tagString(missing) = %q, want empty", got)
	}
}

// TestTagFloat accepts JSON numbers and numeric strings, rejecting others.
func TestTagFloat(t *testing.T) {
	t.Parallel()
	obj := map[string]any{"num": 5.5, "str": "12.25", "bad": "x"}
	if got, ok := tagFloat(obj, "num"); !ok || got != 5.5 {
		t.Errorf("tagFloat(num) = %v, %v; want 5.5, true", got, ok)
	}
	if got, ok := tagFloat(obj, "str"); !ok || got != 12.25 {
		t.Errorf("tagFloat(str) = %v, %v; want 12.25, true", got, ok)
	}
	if _, ok := tagFloat(obj, "bad"); ok {
		t.Error("tagFloat(bad) ok = true, want false")
	}
	if _, ok := tagFloat(obj, "missing"); ok {
		t.Error("tagFloat(missing) ok = true, want false")
	}
}
