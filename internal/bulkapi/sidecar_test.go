package bulkapi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/bulk"
)

// recordingEnqueuer records the uids it was asked to schedule.
type recordingEnqueuer struct {
	uids []string
	err  error
}

// EnqueueSidecar records uid and reports the configured error.
func (r *recordingEnqueuer) EnqueueSidecar(_ context.Context, uid string) error {
	r.uids = append(r.uids, uid)
	return r.err
}

// resultOf builds a bulk.Result from (uid, status) pairs, standing in for what
// bulk.Service.Apply returns.
func resultOf(pairs ...[2]string) bulk.Result {
	var result bulk.Result
	for _, p := range pairs {
		result.Results = append(result.Results, bulk.PhotoResult{PhotoUID: p[0], Status: p[1]})
	}
	return result
}

// TestEnqueueSidecars_onePerUpdatedPhoto is the spec's bulk requirement: a batch
// over N photos enqueues N cheap jobs rather than writing N files inside the
// request. Each enqueue is one small insert; the worker writes the files, and the
// queue's per-photo dedup collapses repeats.
func TestEnqueueSidecars_onePerUpdatedPhoto(t *testing.T) {
	t.Parallel()

	const batchSize = 500
	pairs := make([][2]string, 0, batchSize)
	for i := range batchSize {
		pairs = append(pairs, [2]string{fmt.Sprintf("pht%04d", i), bulk.StatusUpdated})
	}
	enq := &recordingEnqueuer{}
	api := NewAPI(Config{Service: nil, Sidecar: enq, RequireWrite: passthrough})

	api.enqueueSidecars(context.Background(), resultOf(pairs...))

	if len(enq.uids) != batchSize {
		t.Errorf("enqueued %d sidecar jobs for a %d-photo batch, want %d",
			len(enq.uids), batchSize, batchSize)
	}
}

// TestEnqueueSidecars_skipsUnchangedPhotos asserts a photo the batch did not
// change is not scheduled: its sidecar is still current, so rewriting it would be
// pure I/O for nothing.
func TestEnqueueSidecars_skipsUnchangedPhotos(t *testing.T) {
	t.Parallel()

	enq := &recordingEnqueuer{}
	api := NewAPI(Config{Sidecar: enq, RequireWrite: passthrough})

	api.enqueueSidecars(context.Background(), resultOf(
		[2]string{"pht1", bulk.StatusUpdated},
		[2]string{"pht2", bulk.StatusSkipped},
		[2]string{"pht3", bulk.StatusError},
		[2]string{"pht4", bulk.StatusUpdated},
	))

	want := []string{"pht1", "pht4"}
	if len(enq.uids) != len(want) {
		t.Fatalf("enqueued %v, want only the updated photos %v", enq.uids, want)
	}
	for i, uid := range want {
		if enq.uids[i] != uid {
			t.Errorf("enqueued[%d] = %q, want %q", i, enq.uids[i], uid)
		}
	}
}

// TestEnqueueSidecars_failureIsSwallowed asserts a queue failure costs a stale
// sidecar, not the user's edit. The batch is committed either way; the enqueue is
// best-effort by design, and a lost one is recovered by the backfill.
func TestEnqueueSidecars_failureIsSwallowed(t *testing.T) {
	t.Parallel()

	enq := &recordingEnqueuer{err: errors.New("queue down")}
	api := NewAPI(Config{Sidecar: enq, RequireWrite: passthrough})

	// The call must not panic and must keep going past the first failure, so a
	// transient queue error does not silently truncate the rest of the batch.
	api.enqueueSidecars(context.Background(), resultOf(
		[2]string{"pht1", bulk.StatusUpdated},
		[2]string{"pht2", bulk.StatusUpdated},
	))

	if len(enq.uids) != 2 {
		t.Errorf("attempted %d enqueues, want 2 — a failure must not abort the rest", len(enq.uids))
	}
}

// TestEnqueueSidecars_withoutEnqueuer asserts the batch path is inert when the
// export is switched off.
func TestEnqueueSidecars_withoutEnqueuer(t *testing.T) {
	t.Parallel()

	api := NewAPI(Config{Sidecar: nil, RequireWrite: passthrough})
	// Must not panic on a nil enqueuer.
	api.enqueueSidecars(context.Background(), resultOf([2]string{"pht1", bulk.StatusUpdated}))
}
