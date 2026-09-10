package hlsjob

import (
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/hls"
	"github.com/panbotka/kukatko/internal/photos"
)

// renditionCall is one ObserveRenditionEncode call, kept whole so a test can
// assert the label values and the numbers together.
type renditionCall struct {
	rendition string
	outcome   string
	duration  time.Duration
	written   int64
}

// fakeObserver records what an encode reported. The encode is sequential within
// one job, so it needs no locking.
type fakeObserver struct {
	renditions []renditionCall
	source     []time.Duration
}

// ObserveRenditionEncode records one rendition's report.
func (o *fakeObserver) ObserveRenditionEncode(rendition, outcome string, d time.Duration, written int64) {
	o.renditions = append(o.renditions, renditionCall{
		rendition: rendition, outcome: outcome, duration: d, written: written,
	})
}

// ObserveEncodedSource records one video's length.
func (o *fakeObserver) ObserveEncodedSource(d time.Duration) {
	o.source = append(o.source, d)
}

// steppingClock returns a clock that advances by step on every reading, so a
// timed section measures exactly one step whatever the machine is doing.
func steppingClock(step time.Duration) func() time.Time {
	base := time.Date(2026, time.September, 10, 12, 0, 0, 0, time.UTC)
	var reads int
	return func() time.Time {
		now := base.Add(time.Duration(reads) * step)
		reads++
		return now
	}
}

// TestOutcomeFor verifies the two outcome labels a rendition can be reported
// under, since they are the label values a dashboard is written against.
func TestOutcomeFor(t *testing.T) {
	t.Parallel()

	if got := outcomeFor(nil); got != OutcomeSuccess {
		t.Errorf("outcomeFor(nil) = %q, want %q", got, OutcomeSuccess)
	}
	if got := outcomeFor(errors.New("ffmpeg died")); got != OutcomeError {
		t.Errorf("outcomeFor(err) = %q, want %q", got, OutcomeError)
	}
}

// TestClipLength verifies the footage counter's input: a known length becomes a
// duration, and a length the catalogue never recorded becomes zero rather than a
// made-up one.
func TestClipLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		durationMs *int
		want       time.Duration
	}{
		{name: "unknown length", durationMs: nil, want: 0},
		{name: "nonsense length", durationMs: new(int), want: 0},
		{name: "negative length", durationMs: new(-5), want: 0},
		{name: "ninety seconds", durationMs: new(90_000), want: 90 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := clipLength(tt.durationMs); got != tt.want {
				t.Errorf("clipLength = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEncodeOne_observesFailure verifies a rendition that could not be encoded is
// still reported — labelled by its name and as an error, timed, and carrying the
// bytes it published (none, since it never got that far). An encode that fails
// after hours is the case the timing exists for, so it must not be the case that
// records nothing.
func TestEncodeOne_observesFailure(t *testing.T) {
	t.Parallel()

	photo := videoPhoto("ph-video")
	obs := &fakeObserver{}
	svc := newService(t, &fakePhotos{rows: map[string]photos.Photo{photo.UID: photo}},
		newFakeObjects(), &fakeRenditions{})
	svc.observer = obs
	svc.now = steppingClock(2 * time.Second)

	rendition, _ := hls.ByName(hls.Rendition1080p)
	if err := svc.encodeOne(t.Context(), photo, "/nonexistent/clip.mp4", rendition); err == nil {
		t.Fatal("encodeOne succeeded on a source that is not there")
	}
	if len(obs.renditions) != 1 {
		t.Fatalf("observer got %d rendition reports, want 1", len(obs.renditions))
	}
	got := obs.renditions[0]
	want := renditionCall{
		rendition: hls.Rendition1080p, outcome: OutcomeError, duration: 2 * time.Second, written: 0,
	}
	if got != want {
		t.Errorf("rendition report = %+v, want %+v", got, want)
	}
	if len(obs.source) != 0 {
		t.Errorf("observer got %d footage reports, want none from a failed encode", len(obs.source))
	}
}

// TestTranscode_observesNoFootageWhenNothingEncoded verifies the footage counter
// stays put when the encode never finished: the retry will put the same minutes
// through again, and counting them twice would understate what a minute costs.
func TestTranscode_observesNoFootageWhenNothingEncoded(t *testing.T) {
	t.Parallel()

	photo := videoPhoto("ph-video")
	photo.DurationMs = new(30_000)
	obs := &fakeObserver{}
	objects := newFakeObjects()
	objects.materializeErr = errors.New("bucket unreachable")
	svc := New(Config{
		Photos:          &fakePhotos{rows: map[string]photos.Photo{photo.UID: photo}},
		Objects:         objects,
		Renditions:      &fakeRenditions{},
		Plan:            hls.All(),
		FFmpegAvailable: func() bool { return true },
		Metrics:         obs,
	})

	if err := svc.Transcode(t.Context(), photo.UID); err == nil {
		t.Fatal("Transcode succeeded with an unreachable original")
	}
	if len(obs.source) != 0 || len(obs.renditions) != 0 {
		t.Errorf("observer got %d footage and %d rendition reports, want none",
			len(obs.source), len(obs.renditions))
	}
}

// TestNew_withoutObserverEncodesTheSame verifies the no-observer path: a Service
// built with no Metrics records nothing and behaves exactly like one that does —
// same error, same objects published, same rows written — so instrumentation can
// never change what the encode does.
func TestNew_withoutObserverEncodesTheSame(t *testing.T) {
	t.Parallel()

	photo := videoPhoto("ph-video")
	photo.DurationMs = new(30_000)
	cat := &fakePhotos{rows: map[string]photos.Photo{photo.UID: photo}}

	// outcome is what one run left behind, reduced to what the two runs must
	// agree on.
	type outcome struct {
		failed    bool
		published int
		rows      int
	}
	run := func(obs Observer) outcome {
		objects, rend := newFakeObjects(), &fakeRenditions{}
		svc := New(Config{
			Photos: cat, Objects: objects, Renditions: rend, Plan: hls.All(),
			FFmpegAvailable: func() bool { return true }, Metrics: obs,
		})
		err := svc.Transcode(t.Context(), photo.UID)
		return outcome{failed: err != nil, published: len(objects.put), rows: len(rend.saved)}
	}

	obs := &fakeObserver{}
	observed, silent := run(obs), run(nil)
	if observed != silent {
		t.Errorf("observed run = %+v, unobserved run = %+v; the two paths must agree", observed, silent)
	}
	if len(obs.renditions) == 0 {
		t.Error("the observed run reported nothing, so the comparison proves nothing")
	}
}
