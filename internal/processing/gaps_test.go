package processing

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/photos"
)

// fakeLibrary stands in for the library-wide reads of the gap scan.
type fakeLibrary struct {
	evidence      map[string]Evidence
	unfinished    map[string]map[Step]bool
	evidenceErr   error
	unfinishedErr error
}

// EachLiveEvidence walks the pinned evidence in uid order.
func (f *fakeLibrary) EachLiveEvidence(_ context.Context, fn func(string, Evidence) error) error {
	if f.evidenceErr != nil {
		return f.evidenceErr
	}
	uids := make([]string, 0, len(f.evidence))
	for uid := range f.evidence {
		uids = append(uids, uid)
	}
	slices.Sort(uids)
	for _, uid := range uids {
		if err := fn(uid, f.evidence[uid]); err != nil {
			return err
		}
	}
	return nil
}

// UnfinishedSteps returns the pinned unfinished job types.
func (f *fakeLibrary) UnfinishedSteps(context.Context) (map[string]map[Step]bool, error) {
	return f.unfinished, f.unfinishedErr
}

// doneStill is the evidence of a still every applicable step has landed on,
// except for the steps listed in missing.
func doneStill(missing ...Step) Evidence {
	at := time.Date(2026, 10, 5, 19, 2, 0, 0, time.UTC)
	ev := Evidence{
		MediaType: photos.MediaImage, MetadataAt: &at, ThumbnailAt: &at, EmbeddingAt: &at,
		FaceAt: &at, OCRAt: &at, SidecarAt: &at,
	}
	for _, step := range missing {
		switch step {
		case StepMetadata:
			ev.MetadataAt = nil
		case StepThumbnail:
			ev.ThumbnailAt = nil
		case StepImageEmbed:
			ev.EmbeddingAt = nil
		case StepFaceDetect:
			ev.FaceAt = nil
		case StepOCR:
			ev.OCRAt = nil
		case StepSidecar:
			ev.SidecarAt = nil
		case StepHLS, StepPlaces:
		}
	}
	return ev
}

// TestEvidence_pending checks the gap predicate against the report it must agree
// with: done, queued and skipped steps are not gaps, everything else is.
func TestEvidence_pending(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		ev         Evidence
		unfinished map[Step]bool
		disabled   map[Step]bool
		want       []Step
	}{
		{name: "fully processed still", ev: doneStill()},
		{
			name: "interrupted upload owes thumbnail and jobs",
			ev:   doneStill(StepThumbnail, StepImageEmbed, StepFaceDetect, StepOCR, StepSidecar),
			want: []Step{StepThumbnail, StepImageEmbed, StepFaceDetect, StepOCR, StepSidecar},
		},
		{
			name:       "a queued job is no gap",
			ev:         doneStill(StepImageEmbed, StepFaceDetect),
			unfinished: map[Step]bool{StepImageEmbed: true},
			want:       []Step{StepFaceDetect},
		},
		{
			name:     "a disabled step is no gap",
			ev:       doneStill(StepOCR, StepSidecar),
			disabled: map[Step]bool{StepOCR: true},
			want:     []Step{StepSidecar},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			skip := func(step Step) bool { return tt.disabled[step] || !tt.ev.applies(step) }
			got := tt.ev.pending(tt.unfinished, skip)
			if !slices.Equal(got, tt.want) {
				t.Errorf("pending = %v, want %v", got, tt.want)
			}
			// Agreement with the report, step by step.
			byType := map[string]jobs.Job{}
			for step := range tt.unfinished {
				byType[string(step)] = jobs.Job{Type: string(step), State: jobs.StateQueued}
			}
			var fromReport []Step
			for _, st := range tt.ev.report(byType, skip) {
				if st.State == StatePending {
					fromReport = append(fromReport, st.Step)
				}
			}
			if !slices.Equal(got, fromReport) {
				t.Errorf("pending = %v, but the report calls %v pending", got, fromReport)
			}
		})
	}
}

// TestService_SchedulePending checks that only pending steps are enqueued — a
// done, queued or skipped step is left alone — and that the scheduled steps are
// returned in report order.
func TestService_SchedulePending(t *testing.T) {
	t.Parallel()
	ev := &fakeEvidence{evidence: doneStill(StepThumbnail, StepImageEmbed, StepFaceDetect, StepOCR)}
	jb := &fakeJobs{list: []jobs.Job{{Type: jobs.TypeFaceDetect, State: jobs.StateQueued}}}
	enq := &fakeEnqueuer{}
	svc := newTestService(t, ev, jb, enq, StepOCR)

	got, err := svc.SchedulePending(t.Context(), "p1")
	if err != nil {
		t.Fatalf("SchedulePending: %v", err)
	}
	want := []Step{StepThumbnail, StepImageEmbed}
	if !slices.Equal(got, want) {
		t.Errorf("scheduled = %v, want %v", got, want)
	}
	if !slices.Equal(enq.scheduled, []string{jobs.TypeThumbnail, jobs.TypeImageEmbed}) {
		t.Errorf("enqueued = %v, want thumbnail and image_embed only", enq.scheduled)
	}
}

// TestService_SchedulePending_nothingOwed checks a fully processed photo
// enqueues nothing.
func TestService_SchedulePending_nothingOwed(t *testing.T) {
	t.Parallel()
	enq := &fakeEnqueuer{}
	svc := newTestService(t, &fakeEvidence{evidence: doneStill()}, &fakeJobs{}, enq)

	got, err := svc.SchedulePending(t.Context(), "p1")
	if err != nil || len(got) != 0 || len(enq.scheduled) != 0 {
		t.Errorf("SchedulePending = (%v, %v), enqueued %v; want nothing", got, err, enq.scheduled)
	}
}

// TestService_SchedulePending_errors covers both failure paths: an unknown photo
// is ErrPhotoNotFound, and a failed enqueue is reported without stopping the
// others.
func TestService_SchedulePending_errors(t *testing.T) {
	t.Parallel()
	missing := newTestService(t, &fakeEvidence{err: photos.ErrPhotoNotFound}, &fakeJobs{}, &fakeEnqueuer{})
	if _, err := missing.SchedulePending(t.Context(), "nobody"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("unknown photo: err = %v, want ErrPhotoNotFound", err)
	}

	boom := errors.New("queue down")
	enq := &fakeEnqueuer{err: boom}
	svc := newTestService(t, &fakeEvidence{evidence: doneStill(StepImageEmbed, StepSidecar)}, &fakeJobs{}, enq)
	got, err := svc.SchedulePending(t.Context(), "p1")
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the enqueue failure", err)
	}
	if len(got) != 0 || len(enq.scheduled) != 2 {
		t.Errorf("scheduled = %v after %v attempts, want none reported and both tried", got, enq.scheduled)
	}
}

// TestService_Unscheduled checks the library-wide scan: it reports each owed step
// per photo in uid order, leaves out the queued and the disabled ones, and costs
// one read of each half however many photos there are.
func TestService_Unscheduled(t *testing.T) {
	t.Parallel()
	lib := &fakeLibrary{
		evidence: map[string]Evidence{
			"pb": doneStill(StepThumbnail, StepImageEmbed, StepOCR),
			"pa": doneStill(StepSidecar),
			"pc": doneStill(),
		},
		unfinished: map[string]map[Step]bool{"pb": {StepImageEmbed: true}},
	}
	svc := New(Config{
		Evidence: &fakeEvidence{}, Jobs: &fakeJobs{}, Enqueuer: &fakeEnqueuer{},
		Library: lib, Disabled: []Step{StepOCR},
	})

	got, err := svc.Unscheduled(t.Context())
	if err != nil {
		t.Fatalf("Unscheduled: %v", err)
	}
	want := []Gap{{PhotoUID: "pa", Step: StepSidecar}, {PhotoUID: "pb", Step: StepThumbnail}}
	if !slices.Equal(got, want) {
		t.Errorf("gaps = %v, want %v", got, want)
	}
}

// TestService_Unscheduled_errors covers the unwired service and both failing
// reads.
func TestService_Unscheduled_errors(t *testing.T) {
	t.Parallel()
	bare := newTestService(t, &fakeEvidence{}, &fakeJobs{}, &fakeEnqueuer{})
	if _, err := bare.Unscheduled(t.Context()); !errors.Is(err, ErrLibraryUnavailable) {
		t.Errorf("unwired: err = %v, want ErrLibraryUnavailable", err)
	}
	boom := errors.New("db down")
	for name, lib := range map[string]*fakeLibrary{
		"evidence":   {evidenceErr: boom},
		"unfinished": {unfinishedErr: boom},
	} {
		svc := New(Config{Evidence: &fakeEvidence{}, Jobs: &fakeJobs{}, Enqueuer: &fakeEnqueuer{}, Library: lib})
		if _, err := svc.Unscheduled(t.Context()); !errors.Is(err, boom) {
			t.Errorf("%s read failing: err = %v, want it propagated", name, err)
		}
	}
}
