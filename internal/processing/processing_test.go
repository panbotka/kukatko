package processing

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestParseStep covers the untrusted-input boundary: only the reported steps are
// accepted, and the job types that exist but are not per-photo steps are not.
func TestParseStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Step
		wanOK bool
	}{
		{name: "metadata", input: "metadata", want: StepMetadata, wanOK: true},
		{name: "sidecar", input: "sidecar", want: StepSidecar, wanOK: true},
		{name: "storyboard is not a reported step", input: "storyboard"},
		{name: "backup is not a per-photo step", input: "backup"},
		{name: "empty", input: ""},
		{name: "nonsense", input: "drop table photos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseStep(tt.input)
			if ok != tt.wanOK || got != tt.want {
				t.Errorf("ParseStep(%q) = (%q, %v), want (%q, %v)", tt.input, got, ok, tt.want, tt.wanOK)
			}
		})
	}
}

// TestSteps_coverEveryStepConstant guards the fixed report order against a step
// constant being added without being reported (or reported twice).
func TestSteps_coverEveryStepConstant(t *testing.T) {
	t.Parallel()

	seen := map[Step]bool{}
	for _, step := range Steps {
		if seen[step] {
			t.Errorf("step %q appears twice in Steps", step)
		}
		seen[step] = true
	}
	for _, step := range []Step{
		StepMetadata, StepThumbnail, StepImageEmbed, StepFaceDetect, StepOCR, StepPlaces, StepSidecar,
	} {
		if !seen[step] {
			t.Errorf("step %q is missing from Steps", step)
		}
	}
	if len(Steps) != 7 {
		t.Errorf("len(Steps) = %d, want 7 (storyboard is deliberately not reported)", len(Steps))
	}
}

// TestEvidence_applies covers the two inapplicability rules: no coordinate means
// no place, and a video is outside face detection and text recognition — while a
// live photo, being a still that carries a clip, is not.
func TestEvidence_applies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		evidence Evidence
		step     Step
		want     bool
	}{
		{name: "places with GPS", evidence: Evidence{HasGPS: true}, step: StepPlaces, want: true},
		{name: "places without GPS", evidence: Evidence{}, step: StepPlaces},
		{
			name:     "faces on an image",
			evidence: Evidence{MediaType: photos.MediaImage},
			step:     StepFaceDetect, want: true,
		},
		{name: "faces on a video", evidence: Evidence{MediaType: photos.MediaVideo}, step: StepFaceDetect},
		{name: "ocr on a video", evidence: Evidence{MediaType: photos.MediaVideo}, step: StepOCR},
		{
			name:     "ocr on a live photo",
			evidence: Evidence{MediaType: photos.MediaLive},
			step:     StepOCR, want: true,
		},
		{
			name:     "thumbnail on a video",
			evidence: Evidence{MediaType: photos.MediaVideo},
			step:     StepThumbnail, want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.evidence.applies(tt.step); got != tt.want {
				t.Errorf("applies(%q) = %v, want %v", tt.step, got, tt.want)
			}
		})
	}
}

// TestJobState maps every unfinished queue row onto the state it reports. The
// interesting rows are the two that mean "failed" without saying so: a
// dead-lettered job, and one back in the queue carrying the error of its last
// attempt.
func TestJobState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		job       jobs.Job
		wantState State
		wantErr   string
	}{
		{name: "queued", job: jobs.Job{State: jobs.StateQueued}, wantState: StateQueued},
		{name: "running", job: jobs.Job{State: jobs.StateRunning}, wantState: StateRunning},
		{
			name:      "dead carries its error",
			job:       jobs.Job{State: jobs.StateDead, LastError: "box offline"},
			wantState: StateFailed, wantErr: "box offline",
		},
		{
			name:      "queued retry after an error",
			job:       jobs.Job{State: jobs.StateQueued, Attempts: 1, LastError: "decode failed"},
			wantState: StateFailed, wantErr: "decode failed",
		},
		{
			name:      "running wins over its previous error",
			job:       jobs.Job{State: jobs.StateRunning, Attempts: 1, LastError: "decode failed"},
			wantState: StateRunning,
		},
		{
			name:      "terminally failed",
			job:       jobs.Job{State: jobs.StateFailed, LastError: "unsupported"},
			wantState: StateFailed, wantErr: "unsupported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, msg := jobState(tt.job)
			if state != tt.wantState || msg != tt.wantErr {
				t.Errorf("jobState(%+v) = (%q, %q), want (%q, %q)",
					tt.job, state, msg, tt.wantState, tt.wantErr)
			}
		})
	}
}

// TestEvidence_status covers the precedence between the three sources: landed
// evidence beats the queue, the queue beats "will not run", and an absence with
// nothing behind it is pending.
func TestEvidence_status(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		evidence Evidence
		step     Step
		job      *jobs.Job
		skip     bool
		want     State
		wantAt   bool
	}{
		{
			name:     "evidence beats a queued job",
			evidence: Evidence{MetadataAt: &at},
			step:     StepMetadata, job: &jobs.Job{State: jobs.StateQueued},
			want: StateDone, wantAt: true,
		},
		{
			name:     "a geocoded place beats being skipped",
			evidence: Evidence{PlaceAt: &at},
			step:     StepPlaces, skip: true,
			want: StateDone, wantAt: true,
		},
		{
			name:     "a queued job beats being skipped",
			evidence: Evidence{MediaType: photos.MediaVideo},
			step:     StepOCR, job: &jobs.Job{State: jobs.StateQueued}, skip: true,
			want: StateQueued,
		},
		{name: "skipped", step: StepPlaces, skip: true, want: StateSkipped},
		{name: "pending", step: StepImageEmbed, want: StatePending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.evidence.status(tt.step, tt.job, tt.skip)
			if got.State != tt.want {
				t.Errorf("status(%q).State = %q, want %q", tt.step, got.State, tt.want)
			}
			if (got.At != nil) != tt.wantAt {
				t.Errorf("status(%q).At = %v, want present=%v", tt.step, got.At, tt.wantAt)
			}
		})
	}
}

// TestEvidence_status_describesResult pins the two results that must not read as
// a gap: a photo looked at and holding no face, and OCR that found no text.
func TestEvidence_status_describesResult(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)

	faces := Evidence{FaceAt: &at, FaceCount: 0}.status(StepFaceDetect, nil, false)
	if faces.FaceCount == nil || *faces.FaceCount != 0 {
		t.Errorf("face_count = %v, want a present 0", faces.FaceCount)
	}
	if faces.State != StateDone {
		t.Errorf("state = %q, want %q", faces.State, StateDone)
	}

	empty := Evidence{OCRAt: &at, OCRTextFound: false}.status(StepOCR, nil, false)
	if empty.TextFound == nil || *empty.TextFound {
		t.Errorf("text_found = %v, want a present false", empty.TextFound)
	}
	found := Evidence{OCRAt: &at, OCRTextFound: true}.status(StepOCR, nil, false)
	if found.TextFound == nil || !*found.TextFound {
		t.Errorf("text_found = %v, want a present true", found.TextFound)
	}

	// The step-specific fields belong to their own step and to a landed result.
	if pending := (Evidence{}).status(StepFaceDetect, nil, false); pending.FaceCount != nil {
		t.Errorf("a pending face step reported face_count = %v, want none", pending.FaceCount)
	}
	if meta := (Evidence{MetadataAt: &at}).status(StepMetadata, nil, false); meta.TextFound != nil {
		t.Errorf("the metadata step reported text_found = %v, want none", meta.TextFound)
	}
}

// TestEvidence_report checks the whole array: one entry per step, in the fixed
// order, each resolved against its own job type.
func TestEvidence_report(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	ev := Evidence{
		MediaType: photos.MediaImage, HasGPS: false,
		MetadataAt: &at, ThumbnailAt: &at,
	}
	byType := map[string]jobs.Job{
		jobs.TypeImageEmbed: {Type: jobs.TypeImageEmbed, State: jobs.StateRunning},
		jobs.TypeFaceDetect: {Type: jobs.TypeFaceDetect, State: jobs.StateDead, LastError: "boom"},
	}
	report := ev.report(byType, func(step Step) bool { return step == StepPlaces })

	if len(report) != len(Steps) {
		t.Fatalf("len(report) = %d, want %d", len(report), len(Steps))
	}
	want := []State{
		StateDone,    // metadata
		StateDone,    // thumbnail
		StateRunning, // image_embed
		StateFailed,  // face_detect
		StatePending, // ocr
		StateSkipped, // places
		StatePending, // sidecar
	}
	for i, entry := range report {
		if entry.Step != Steps[i] {
			t.Errorf("report[%d].Step = %q, want %q", i, entry.Step, Steps[i])
		}
		if entry.State != want[i] {
			t.Errorf("report[%d] (%q).State = %q, want %q", i, entry.Step, entry.State, want[i])
		}
	}
	if report[3].Error != "boom" {
		t.Errorf("face_detect error = %q, want %q", report[3].Error, "boom")
	}
}
