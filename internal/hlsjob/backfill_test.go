package hlsjob

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// fakeLister is a PhotoLister answering with fixed uid lists, so a backfill test
// pins which of the two queries the flag chose without a database.
type fakeLister struct {
	missing []string
	active  []string
	err     error
}

// ListVideosMissingHLS returns the pinned "never encoded" list.
func (f *fakeLister) ListVideosMissingHLS(_ context.Context, _ int) ([]string, error) {
	return f.missing, f.err
}

// ListActiveVideoUIDs returns the pinned "every video" list.
func (f *fakeLister) ListActiveVideoUIDs(_ context.Context) ([]string, error) {
	return f.active, f.err
}

// fakeBackfillEnqueuer records the uids scheduled, and can fail on one of them.
type fakeBackfillEnqueuer struct {
	scheduled []string
	failOn    string
}

// EnqueueHLSTranscode records photoUID, failing when it is the pinned one.
func (f *fakeBackfillEnqueuer) EnqueueHLSTranscode(_ context.Context, photoUID string) error {
	if photoUID == f.failOn {
		return errQueueDown
	}
	f.scheduled = append(f.scheduled, photoUID)
	return nil
}

// errQueueDown stands in for a queue that refuses a job mid-backfill.
var errQueueDown = errors.New("queue down")

// backfillService wires a Service with only the backfill collaborators: the
// encode itself is never reached, so the handler's dependencies would only be
// noise here.
func backfillService(lister PhotoLister, enq Enqueuer) *Service {
	return New(Config{
		Photos: &fakePhotos{}, Objects: &fakeObjects{}, Renditions: &fakeRenditions{},
		Lister: lister, Enqueuer: enq,
	})
}

// TestBackfillHLS_picksTheListForTheFlag pins which query each mode runs: the
// default schedules only videos with no rendition, while ?all=true schedules
// every non-archived video so a newly enabled quality level is picked up.
func TestBackfillHLS_picksTheListForTheFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		all  bool
		want []string
	}{
		{name: "missing only", all: false, want: []string{"p1", "p2"}},
		{name: "forced full re-run", all: true, want: []string{"p1", "p2", "p3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			enq := &fakeBackfillEnqueuer{}
			lister := &fakeLister{missing: []string{"p1", "p2"}, active: []string{"p1", "p2", "p3"}}

			count, err := backfillService(lister, enq).BackfillHLS(t.Context(), tt.all)
			if err != nil {
				t.Fatalf("BackfillHLS(all=%v): %v", tt.all, err)
			}
			if count != len(tt.want) {
				t.Errorf("enqueued = %d, want %d", count, len(tt.want))
			}
			if !slices.Equal(enq.scheduled, tt.want) {
				t.Errorf("scheduled = %v, want %v", enq.scheduled, tt.want)
			}
		})
	}
}

// TestBackfillHLS_withoutCollaborators pins the refusal: a Service built for the
// worker handler alone cannot backfill, and says so rather than reporting zero
// scheduled work as a success.
func TestBackfillHLS_withoutCollaborators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		lister PhotoLister
		enq    Enqueuer
	}{
		{name: "neither"},
		{name: "no enqueuer", lister: &fakeLister{}},
		{name: "no lister", enq: &fakeBackfillEnqueuer{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := backfillService(tt.lister, tt.enq).BackfillHLS(t.Context(), false)
			if !errors.Is(err, ErrBackfillUnavailable) {
				t.Errorf("BackfillHLS error = %v, want ErrBackfillUnavailable", err)
			}
		})
	}
}

// TestBackfillHLS_listFailure checks a failed listing aborts before anything is
// scheduled: half a backfill over an unknown set is worse than none.
func TestBackfillHLS_listFailure(t *testing.T) {
	t.Parallel()

	boom := errors.New("catalogue down")
	enq := &fakeBackfillEnqueuer{}
	count, err := backfillService(&fakeLister{err: boom}, enq).BackfillHLS(t.Context(), false)
	if !errors.Is(err, boom) {
		t.Errorf("BackfillHLS error = %v, want %v", err, boom)
	}
	if count != 0 || len(enq.scheduled) != 0 {
		t.Errorf("scheduled %d/%v after a failed listing, want none", count, enq.scheduled)
	}
}

// TestBackfillHLS_enqueueFailureReportsProgress checks the count reports the
// jobs that really reached the queue: they are already there, and a caller told
// "zero" would re-run the whole backfill over them.
func TestBackfillHLS_enqueueFailureReportsProgress(t *testing.T) {
	t.Parallel()

	enq := &fakeBackfillEnqueuer{failOn: "p3"}
	lister := &fakeLister{missing: []string{"p1", "p2", "p3", "p4"}}

	count, err := backfillService(lister, enq).BackfillHLS(t.Context(), false)
	if !errors.Is(err, errQueueDown) {
		t.Fatalf("BackfillHLS error = %v, want %v", err, errQueueDown)
	}
	if count != 2 {
		t.Errorf("enqueued = %d, want 2 (the ones that landed before the failure)", count)
	}
}
