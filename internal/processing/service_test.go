package processing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeEvidence stands in for the database read.
type fakeEvidence struct {
	evidence Evidence
	err      error
	calls    int
}

// Evidence returns the pinned evidence (or error) and counts the call, so a test
// can assert the report costs exactly one read.
func (f *fakeEvidence) Evidence(_ context.Context, _ string) (Evidence, error) {
	f.calls++
	return f.evidence, f.err
}

// fakeJobs stands in for the queue read.
type fakeJobs struct {
	list  []jobs.Job
	err   error
	calls int
}

// UnfinishedForPhoto returns the pinned rows (or error) and counts the call.
func (f *fakeJobs) UnfinishedForPhoto(_ context.Context, _ string) ([]jobs.Job, error) {
	f.calls++
	return f.list, f.err
}

// fakeEnqueuer records which step was scheduled for which photo.
type fakeEnqueuer struct {
	scheduled []string
	err       error
}

// record appends a scheduled step and returns the pinned error.
func (f *fakeEnqueuer) record(step string) error {
	f.scheduled = append(f.scheduled, step)
	return f.err
}

func (f *fakeEnqueuer) EnqueueMetadata(_ context.Context, _ string) error {
	return f.record(jobs.TypeMetadata)
}
func (f *fakeEnqueuer) EnqueueThumbnail(_ context.Context, _ string) error {
	return f.record(jobs.TypeThumbnail)
}
func (f *fakeEnqueuer) EnqueueImageEmbed(_ context.Context, _ string) error {
	return f.record(jobs.TypeImageEmbed)
}
func (f *fakeEnqueuer) EnqueueFaceDetect(_ context.Context, _ string) error {
	return f.record(jobs.TypeFaceDetect)
}
func (f *fakeEnqueuer) EnqueueOCR(_ context.Context, _ string) error {
	return f.record(jobs.TypeOCR)
}
func (f *fakeEnqueuer) EnqueuePlaces(_ context.Context, _ string) error {
	return f.record(jobs.TypePlaces)
}
func (f *fakeEnqueuer) EnqueueSidecar(_ context.Context, _ string) error {
	return f.record(jobs.TypeSidecar)
}

// newTestService wires a Service over the three fakes.
func newTestService(
	t *testing.T, ev *fakeEvidence, jb *fakeJobs, enq *fakeEnqueuer, disabled ...Step,
) *Service {
	t.Helper()
	return New(Config{Evidence: ev, Jobs: jb, Enqueuer: enq, Disabled: disabled})
}

// TestNew_requiresCollaborators pins the panic: a Service missing a collaborator
// could only report or schedule half the truth.
func TestNew_requiresCollaborators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{name: "no evidence", cfg: Config{Jobs: &fakeJobs{}, Enqueuer: &fakeEnqueuer{}}},
		{name: "no jobs", cfg: Config{Evidence: &fakeEvidence{}, Enqueuer: &fakeEnqueuer{}}},
		{name: "no enqueuer", cfg: Config{Evidence: &fakeEvidence{}, Jobs: &fakeJobs{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic on a missing collaborator")
				}
			}()
			New(tt.cfg)
		})
	}
}

// TestService_Report_isTwoRoundTrips is the N+1 guard: however many steps there
// are, the report reads the evidence once and the queue once.
func TestService_Report_isTwoRoundTrips(t *testing.T) {
	t.Parallel()

	ev := &fakeEvidence{evidence: Evidence{MediaType: photos.MediaImage}}
	jb := &fakeJobs{}
	svc := newTestService(t, ev, jb, &fakeEnqueuer{})

	report, err := svc.Report(t.Context(), "p1")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if len(report) != len(Steps) {
		t.Errorf("len(report) = %d, want %d", len(report), len(Steps))
	}
	if ev.calls != 1 || jb.calls != 1 {
		t.Errorf("round trips = (evidence %d, jobs %d), want (1, 1)", ev.calls, jb.calls)
	}
}

// TestService_Report_disabledStepIsSkipped covers the instance switch: with the
// feature off no worker handler is registered, so the step reads as skipped
// rather than as a gap waiting to be filled.
func TestService_Report_disabledStepIsSkipped(t *testing.T) {
	t.Parallel()

	svc := newTestService(t,
		&fakeEvidence{evidence: Evidence{MediaType: photos.MediaImage}},
		&fakeJobs{}, &fakeEnqueuer{}, StepOCR)

	report, err := svc.Report(t.Context(), "p1")
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	for _, entry := range report {
		want := StatePending
		if entry.Step == StepOCR {
			want = StateSkipped
		}
		if entry.Step == StepPlaces {
			want = StateSkipped // no coordinate on this photo either
		}
		if entry.State != want {
			t.Errorf("%q = %q, want %q", entry.Step, entry.State, want)
		}
	}
}

// TestService_Report_propagatesErrors checks both reads fail the report rather
// than reporting a half-truth.
func TestService_Report_propagatesErrors(t *testing.T) {
	t.Parallel()

	missing := &fakeEvidence{err: photos.ErrPhotoNotFound}
	if _, err := newTestService(t, missing, &fakeJobs{}, &fakeEnqueuer{}).
		Report(t.Context(), "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Report error = %v, want photos.ErrPhotoNotFound", err)
	}

	boom := errors.New("queue down")
	if _, err := newTestService(t, &fakeEvidence{}, &fakeJobs{err: boom}, &fakeEnqueuer{}).
		Report(t.Context(), "p1"); !errors.Is(err, boom) {
		t.Errorf("Report error = %v, want %v", err, boom)
	}
}

// TestService_Run_schedulesTheStep checks the happy path: the right enqueuer
// method is called and the answer is the step's new state.
func TestService_Run_schedulesTheStep(t *testing.T) {
	t.Parallel()

	enq := &fakeEnqueuer{}
	jb := &fakeJobs{}
	svc := newTestService(t, &fakeEvidence{evidence: Evidence{MediaType: photos.MediaImage}}, jb, enq)

	// The queue read after the enqueue sees the freshly queued row.
	jb.list = []jobs.Job{{Type: jobs.TypeImageEmbed, State: jobs.StateQueued}}

	status, err := svc.Run(t.Context(), "p1", StepImageEmbed)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(enq.scheduled) != 1 || enq.scheduled[0] != jobs.TypeImageEmbed {
		t.Errorf("scheduled = %v, want [%s]", enq.scheduled, jobs.TypeImageEmbed)
	}
	if status.Step != StepImageEmbed || status.State != StateQueued {
		t.Errorf("status = %+v, want a queued image_embed", status)
	}
}

// TestService_Run_everyStepReachesItsEnqueuer guards the dispatch table: a new
// step must not silently fall through to nothing.
func TestService_Run_everyStepReachesItsEnqueuer(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	for _, step := range Steps {
		t.Run(string(step), func(t *testing.T) {
			t.Parallel()
			enq := &fakeEnqueuer{}
			// GPS and a still, so no step is inapplicable; a landed metadata read
			// proves an already-done step can still be re-run on demand.
			ev := &fakeEvidence{evidence: Evidence{
				MediaType: photos.MediaImage, HasGPS: true, MetadataAt: &at,
			}}
			if _, err := newTestService(t, ev, &fakeJobs{}, enq).Run(t.Context(), "p1", step); err != nil {
				t.Fatalf("Run(%q): %v", step, err)
			}
			if len(enq.scheduled) != 1 || enq.scheduled[0] != string(step) {
				t.Errorf("Run(%q) scheduled %v", step, enq.scheduled)
			}
		})
	}
}

// TestService_Run_refusals covers the three requests the service turns away
// before touching the queue.
func TestService_Run_refusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		evidence Evidence
		evErr    error
		disabled []Step
		step     Step
		wantErr  error
	}{
		{name: "unknown step", step: Step("storyboard"), wantErr: ErrUnknownStep},
		{name: "missing photo", step: StepMetadata, evErr: photos.ErrPhotoNotFound, wantErr: photos.ErrPhotoNotFound},
		{name: "places without GPS", step: StepPlaces, wantErr: ErrStepNotApplicable},
		{
			name:     "ocr on a video",
			evidence: Evidence{MediaType: photos.MediaVideo},
			step:     StepOCR, wantErr: ErrStepNotApplicable,
		},
		{
			name:     "a disabled feature",
			evidence: Evidence{MediaType: photos.MediaImage},
			disabled: []Step{StepSidecar}, step: StepSidecar, wantErr: ErrStepNotApplicable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enq := &fakeEnqueuer{}
			svc := newTestService(t,
				&fakeEvidence{evidence: tt.evidence, err: tt.evErr}, &fakeJobs{}, enq, tt.disabled...)
			if _, err := svc.Run(t.Context(), "p1", tt.step); !errors.Is(err, tt.wantErr) {
				t.Errorf("Run(%q) error = %v, want %v", tt.step, err, tt.wantErr)
			}
			if len(enq.scheduled) != 0 {
				t.Errorf("Run(%q) scheduled %v, want nothing", tt.step, enq.scheduled)
			}
		})
	}
}

// TestService_Run_enqueueFailurePropagates checks a queue failure is reported
// rather than answered with an invented state.
func TestService_Run_enqueueFailurePropagates(t *testing.T) {
	t.Parallel()

	boom := errors.New("queue down")
	svc := newTestService(t,
		&fakeEvidence{evidence: Evidence{MediaType: photos.MediaImage}},
		&fakeJobs{}, &fakeEnqueuer{err: boom})
	if _, err := svc.Run(t.Context(), "p1", StepThumbnail); !errors.Is(err, boom) {
		t.Errorf("Run error = %v, want %v", err, boom)
	}
}
