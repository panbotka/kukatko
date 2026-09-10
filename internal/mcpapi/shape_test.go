package mcpapi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/processing"
)

// fakeEncodeReporter answers a fixed processing report, and records which photo
// was asked about so a test can prove a still costs no query at all.
type fakeEncodeReporter struct {
	report []processing.Status
	err    error
	asked  []string
}

// Report records the uid and returns the configured answer.
func (f *fakeEncodeReporter) Report(_ context.Context, photoUID string) ([]processing.Status, error) {
	f.asked = append(f.asked, photoUID)
	return f.report, f.err
}

// shapeAPI builds an API carrying only what the payload projections need: the URL
// builder they decorate through, and the encode reporter under test.
func shapeAPI(reporter EncodeReporter) *API {
	return &API{media: mediaurl.NewBuilder(nil), processing: reporter}
}

// stillPhoto, loudVideo and silentVideo are the three catalogue rows the payload
// shapes have to tell apart.
func stillPhoto() photos.Photo {
	return photos.Photo{UID: "pht-still", Title: "Lake", MediaType: photos.MediaImage}
}

func loudVideo() photos.Photo {
	duration, fps := 125_000, 29.97
	return photos.Photo{
		UID: "pht-loud", Title: "Party", MediaType: photos.MediaVideo,
		DurationMs: &duration, FPS: &fps,
		VideoCodec: "h264", AudioCodec: "aac", HasAudio: true,
	}
}

func silentVideo() photos.Photo {
	duration, fps := 4_000, 60.0
	return photos.Photo{
		UID: "pht-silent", Title: "Timelapse", MediaType: photos.MediaVideo,
		DurationMs: &duration, FPS: &fps,
		VideoCodec: "hevc", HasAudio: false,
	}
}

// jsonFields marshals a payload and reads it back as a bare map, which is what
// the agent actually sees — including which keys were left out entirely.
func jsonFields(t *testing.T, payload any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshalling the payload: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshalling the payload: %v", err)
	}
	return fields
}

// TestSummarize_carriesDuration verifies a list row says how long a clip is, so a
// listing of videos is useful without a get_photo per item, and that a still does
// not carry the field at all — a duration of zero on a photograph is a claim
// nobody made.
func TestSummarize_carriesDuration(t *testing.T) {
	t.Parallel()

	list := shapeAPI(nil).summarize([]photos.Photo{stillPhoto(), loudVideo(), silentVideo()})
	if len(list) != 3 {
		t.Fatalf("summaries = %d, want 3", len(list))
	}
	if list[0].DurationMs != nil {
		t.Errorf("a still carries duration_ms = %v, want none", *list[0].DurationMs)
	}
	if list[1].DurationMs == nil || *list[1].DurationMs != 125_000 {
		t.Errorf("video duration = %v, want 125000", list[1].DurationMs)
	}

	fields := jsonFields(t, list[0])
	if _, ok := fields["duration_ms"]; ok {
		t.Errorf("a still's summary carries duration_ms: %v", fields)
	}
}

// TestPhotoDetail_videoFacts verifies the detail answers the questions an agent
// asks about a clip — how fast, in what codecs, and whether it has sound — and
// that a still is asked none of them: every video-only key is absent there rather
// than present and empty.
func TestPhotoDetail_videoFacts(t *testing.T) {
	t.Parallel()

	videoKeys := []string{"duration_ms", "fps", "video_codec", "has_audio"}

	t.Run("a still answers none of it", func(t *testing.T) {
		t.Parallel()
		fields := jsonFields(t, toPhotoDetail(stillPhoto()))
		for _, key := range append(videoKeys, "audio_codec", "encode_state") {
			if _, ok := fields[key]; ok {
				t.Errorf("a still carries %q: %v", key, fields[key])
			}
		}
	})

	t.Run("a video with sound", func(t *testing.T) {
		t.Parallel()
		detail := toPhotoDetail(loudVideo())
		if detail.FPS == nil || *detail.FPS != 29.97 {
			t.Errorf("fps = %v, want 29.97", detail.FPS)
		}
		if detail.VideoCodec != "h264" || detail.AudioCodec != "aac" {
			t.Errorf("codecs = %q/%q, want h264/aac", detail.VideoCodec, detail.AudioCodec)
		}
		if detail.HasAudio == nil || !*detail.HasAudio {
			t.Errorf("has_audio = %v, want true", detail.HasAudio)
		}
		fields := jsonFields(t, detail)
		for _, key := range videoKeys {
			if _, ok := fields[key]; !ok {
				t.Errorf("a video is missing %q: %v", key, fields)
			}
		}
	})

	t.Run("a video without sound says so", func(t *testing.T) {
		t.Parallel()
		detail := toPhotoDetail(silentVideo())
		if detail.HasAudio == nil {
			t.Fatalf("a silent video carries no has_audio at all")
		}
		if *detail.HasAudio {
			t.Errorf("has_audio = true, want false")
		}
		fields := jsonFields(t, detail)
		// The distinction that matters: silence is stated, not inferred from a
		// missing audio codec.
		if got, ok := fields["has_audio"]; !ok || got != false {
			t.Errorf("has_audio = %v (present %v), want an explicit false", got, ok)
		}
		if _, ok := fields["audio_codec"]; ok {
			t.Errorf("a silent video names an audio codec: %v", fields["audio_codec"])
		}
	})
}

// TestEncodeState verifies the streaming-encode state reaches the detail for a
// video, is not asked about anything else, and degrades to an absent field rather
// than losing the whole record when the report cannot be read.
func TestEncodeState(t *testing.T) {
	t.Parallel()

	report := []processing.Status{
		{Step: processing.StepThumbnail, State: processing.StateDone},
		{Step: processing.StepHLS, State: processing.StateQueued},
	}

	t.Run("a video carries the state", func(t *testing.T) {
		t.Parallel()
		reporter := &fakeEncodeReporter{report: report}
		got := shapeAPI(reporter).encodeState(context.Background(), loudVideo())
		if got != string(processing.StateQueued) {
			t.Errorf("encode state = %q, want queued", got)
		}
		if len(reporter.asked) != 1 || reporter.asked[0] != "pht-loud" {
			t.Errorf("asked = %v, want one report for the video", reporter.asked)
		}
	})

	t.Run("a still is never asked", func(t *testing.T) {
		t.Parallel()
		reporter := &fakeEncodeReporter{report: report}
		if got := shapeAPI(reporter).encodeState(context.Background(), stillPhoto()); got != "" {
			t.Errorf("encode state of a still = %q, want none", got)
		}
		if len(reporter.asked) != 0 {
			t.Errorf("a still cost %d processing reports, want 0", len(reporter.asked))
		}
	})

	t.Run("an unwired instance answers nothing", func(t *testing.T) {
		t.Parallel()
		if got := shapeAPI(nil).encodeState(context.Background(), loudVideo()); got != "" {
			t.Errorf("encode state without a reporter = %q, want none", got)
		}
	})

	t.Run("a failed report costs the field, not the photo", func(t *testing.T) {
		t.Parallel()
		reporter := &fakeEncodeReporter{err: context.DeadlineExceeded}
		if got := shapeAPI(reporter).encodeState(context.Background(), loudVideo()); got != "" {
			t.Errorf("encode state after a failed report = %q, want none", got)
		}
	})

	t.Run("a report without the step answers nothing", func(t *testing.T) {
		t.Parallel()
		reporter := &fakeEncodeReporter{report: report[:1]}
		if got := shapeAPI(reporter).encodeState(context.Background(), loudVideo()); got != "" {
			t.Errorf("encode state = %q, want none when the report omits the step", got)
		}
	})
}
