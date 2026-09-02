package bulkapi

import (
	"context"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/bulk"
)

// recordingPlaces records the uids it was asked to re-geocode.
type recordingPlaces struct {
	uids []string
	err  error
}

// EnqueuePlaces records uid and reports the configured error.
func (r *recordingPlaces) EnqueuePlaces(_ context.Context, uid string) error {
	r.uids = append(r.uids, uid)
	return r.err
}

// TestEnqueueGeocodes_onlyMovedPhotos asserts the batch schedules a reverse
// geocode exactly for the photos whose coordinates changed. The list the service
// reports is the whole input: a photo the batch touched without moving costs a
// metered mapy.com credit for nothing.
func TestEnqueueGeocodes_onlyMovedPhotos(t *testing.T) {
	t.Parallel()

	enq := &recordingPlaces{}
	api := NewAPI(Config{Places: enq, RequireWrite: passthrough})

	result := resultOf(
		[2]string{"pht1", bulk.StatusUpdated},
		[2]string{"pht2", bulk.StatusUpdated},
		[2]string{"pht3", bulk.StatusSkipped},
	)
	result.LocationChanged = []string{"pht1"}
	api.enqueueGeocodes(context.Background(), result)

	if len(enq.uids) != 1 || enq.uids[0] != "pht1" {
		t.Errorf("enqueued %v, want only the photo that moved [pht1]", enq.uids)
	}
}

// TestEnqueueGeocodes_failureIsSwallowed asserts a queue failure costs a stale
// cached place, not the user's edit: the coordinates are committed either way and
// the rest of the batch is still attempted.
func TestEnqueueGeocodes_failureIsSwallowed(t *testing.T) {
	t.Parallel()

	enq := &recordingPlaces{err: errors.New("queue down")}
	api := NewAPI(Config{Places: enq, RequireWrite: passthrough})

	result := resultOf([2]string{"pht1", bulk.StatusUpdated}, [2]string{"pht2", bulk.StatusUpdated})
	result.LocationChanged = []string{"pht1", "pht2"}
	api.enqueueGeocodes(context.Background(), result)

	if len(enq.uids) != 2 {
		t.Errorf("attempted %d enqueues, want 2 — a failure must not abort the rest", len(enq.uids))
	}
}

// TestEnqueueGeocodes_withoutEnqueuer asserts the path is inert when no queue is
// wired, the way a CLI-built API or a test harness runs it.
func TestEnqueueGeocodes_withoutEnqueuer(t *testing.T) {
	t.Parallel()

	api := NewAPI(Config{Places: nil, RequireWrite: passthrough})
	result := resultOf([2]string{"pht1", bulk.StatusUpdated})
	result.LocationChanged = []string{"pht1"}
	// Must not panic on a nil enqueuer.
	api.enqueueGeocodes(context.Background(), result)
}
