package clusterjob

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/cluster"
	"github.com/panbotka/kukatko/internal/jobs"
)

// errQueue stands in for a queue that refused the job.
var errQueue = errors.New("queue unavailable")

// errState stands in for a library whose grouping state could not be read.
var errState = errors.New("state unavailable")

// fakeClusterer records what the handler asked of the cluster service and
// returns canned outcomes.
type fakeClusterer struct {
	reclusters int
	created    int
	reErr      error

	summaryCalls  []int
	runs          []cluster.SummaryRun
	summaryErr    error
	summaryCursor int

	state      cluster.GroupingState
	stateErr   error
	stateCalls int
}

// GroupingState records the call and returns the canned state.
func (f *fakeClusterer) GroupingState(context.Context) (cluster.GroupingState, error) {
	f.stateCalls++
	return f.state, f.stateErr
}

// Recluster records the call and returns the canned count.
func (f *fakeClusterer) Recluster(context.Context) (int, error) {
	f.reclusters++
	return f.created, f.reErr
}

// BuildSummaries records the batch size it was given and returns the next canned
// run (the last one repeats, so a test needs to script only what it asserts on).
func (f *fakeClusterer) BuildSummaries(_ context.Context, limit int) (cluster.SummaryRun, error) {
	f.summaryCalls = append(f.summaryCalls, limit)
	if f.summaryErr != nil {
		return cluster.SummaryRun{}, f.summaryErr
	}
	if len(f.runs) == 0 {
		return cluster.SummaryRun{}, nil
	}
	run := f.runs[min(f.summaryCursor, len(f.runs)-1)]
	f.summaryCursor++
	return run, nil
}

// fakeQueue is an in-memory stand-in for the job store.
type fakeQueue struct {
	enqueued []json.RawMessage
	pending  int
	err      error
}

// Enqueue records the payload or fails with the canned error.
func (q *fakeQueue) Enqueue(
	_ context.Context, jobType string, payload json.RawMessage, _ jobs.EnqueueOptions,
) (jobs.Job, error) {
	if q.err != nil {
		return jobs.Job{}, q.err
	}
	q.enqueued = append(q.enqueued, payload)
	return jobs.Job{ID: int64(len(q.enqueued)), Type: jobType}, nil
}

// CountPending returns the canned pending count.
func (q *fakeQueue) CountPending(context.Context, ...string) (int, error) {
	return q.pending, q.err
}

// decode reads back an enqueued payload.
func decode(t *testing.T, raw json.RawMessage) payload {
	t.Helper()
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decoding the enqueued payload: %v", err)
	}
	return p
}

// TestScheduleRecluster_queuesAPass queues a regrouping pass when nothing is
// waiting, and says it did.
func TestScheduleRecluster_queuesAPass(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{}
	svc := New(&fakeClusterer{}, queue, 0, nil)

	scheduled, err := svc.ScheduleRecluster(t.Context())
	if err != nil {
		t.Fatalf("ScheduleRecluster: %v", err)
	}
	if !scheduled {
		t.Error("scheduled = false, want true")
	}
	if len(queue.enqueued) != 1 || !decode(t, queue.enqueued[0]).Recluster {
		t.Errorf("enqueued = %s, want one regrouping pass", queue.enqueued)
	}
}

// TestScheduleRecluster_collapsesIntoAPendingPass queues nothing when a pass is
// already waiting or running: a second one would only repeat its work.
func TestScheduleRecluster_collapsesIntoAPendingPass(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{pending: 1}
	svc := New(&fakeClusterer{}, queue, 0, nil)

	scheduled, err := svc.ScheduleRecluster(t.Context())
	if err != nil {
		t.Fatalf("ScheduleRecluster: %v", err)
	}
	if scheduled {
		t.Error("scheduled = true, want false")
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued = %s, want nothing", queue.enqueued)
	}
}

// TestEnsureGrouping_decides queues the one pass the library's state asks for:
// the grouping itself when there are faces and no groups (nothing else in the
// app ever starts it), the summaries when groups are waiting to be listed, and
// nothing at all for a library that is either empty or already done.
func TestEnsureGrouping_decides(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		state         cluster.GroupingState
		wantGrouping  bool
		wantPasses    int
		wantRecluster bool
	}{
		{
			name:          "faces but no groups are grouped",
			state:         cluster.GroupingState{Groupable: true},
			wantGrouping:  true,
			wantPasses:    1,
			wantRecluster: true,
		},
		{
			name:         "unprepared groups are prepared, never regrouped",
			state:        cluster.GroupingState{Ready: 2, Unprepared: 5},
			wantGrouping: true,
			wantPasses:   1,
		},
		{
			name:  "a library with no faces stays calm",
			state: cluster.GroupingState{},
		},
		{
			name:  "a grouped and prepared library queues nothing",
			state: cluster.GroupingState{Ready: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			queue := &fakeQueue{}
			svc := New(&fakeClusterer{state: tt.state}, queue, 0, nil)

			grouping, err := svc.EnsureGrouping(t.Context())
			if err != nil {
				t.Fatalf("EnsureGrouping: %v", err)
			}
			if grouping != tt.wantGrouping {
				t.Errorf("grouping = %t, want %t", grouping, tt.wantGrouping)
			}
			if len(queue.enqueued) != tt.wantPasses {
				t.Fatalf("enqueued = %s, want %d pass(es)", queue.enqueued, tt.wantPasses)
			}
			if tt.wantPasses > 0 && decode(t, queue.enqueued[0]).Recluster != tt.wantRecluster {
				t.Errorf("enqueued = %s, want recluster = %t", queue.enqueued, tt.wantRecluster)
			}
		})
	}
}

// TestEnsureGrouping_collapsesIntoAPendingPass queues nothing while a pass is
// already waiting or running — a reader reloading the page must not stack up a
// queue of identical passes — and still reports that grouping is under way.
func TestEnsureGrouping_collapsesIntoAPendingPass(t *testing.T) {
	t.Parallel()

	clusters := &fakeClusterer{state: cluster.GroupingState{Groupable: true}}
	queue := &fakeQueue{pending: 1}
	svc := New(clusters, queue, 0, nil)

	grouping, err := svc.EnsureGrouping(t.Context())
	if err != nil {
		t.Fatalf("EnsureGrouping: %v", err)
	}
	if !grouping {
		t.Error("grouping = false, want true: a pass is already running")
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued = %s, want nothing", queue.enqueued)
	}
	if clusters.stateCalls != 0 {
		t.Errorf("state reads = %d, want none: the pending pass settles it", clusters.stateCalls)
	}
}

// TestEnsureGrouping_queueFailure reports a queue that refused the job instead
// of pretending a pass was scheduled.
func TestEnsureGrouping_queueFailure(t *testing.T) {
	t.Parallel()

	svc := New(&fakeClusterer{}, &fakeQueue{err: errQueue}, 0, nil)

	if _, err := svc.EnsureGrouping(t.Context()); !errors.Is(err, errQueue) {
		t.Errorf("error = %v, want the queue's", err)
	}
}

// TestEnsureGrouping_stateFailure reports a library whose state could not be
// read rather than guessing at a pass.
func TestEnsureGrouping_stateFailure(t *testing.T) {
	t.Parallel()

	queue := &fakeQueue{}
	svc := New(&fakeClusterer{stateErr: errState}, queue, 0, nil)

	grouping, err := svc.EnsureGrouping(t.Context())
	if !errors.Is(err, errState) {
		t.Errorf("error = %v, want the clusterer's", err)
	}
	if grouping || len(queue.enqueued) != 0 {
		t.Errorf("grouping = %t / enqueued = %s, want false / nothing", grouping, queue.enqueued)
	}
}

// TestHandle_reclustersThenPrepares runs both halves for the admin trigger, in
// that order, and asks for the configured batch size.
func TestHandle_reclustersThenPrepares(t *testing.T) {
	t.Parallel()

	clusters := &fakeClusterer{created: 3}
	queue := &fakeQueue{}
	svc := New(clusters, queue, 50, nil)

	if err := svc.Handle(t.Context(), jobs.Job{ID: 1, Payload: []byte(`{"recluster":true}`)}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if clusters.reclusters != 1 {
		t.Errorf("recluster calls = %d, want 1", clusters.reclusters)
	}
	if len(clusters.summaryCalls) != 1 || clusters.summaryCalls[0] != 50 {
		t.Errorf("summary calls = %v, want one batch of 50", clusters.summaryCalls)
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued = %s, want no successor with nothing left", queue.enqueued)
	}
}

// TestHandle_preparationOnly leaves the grouping alone for a page-scheduled
// pass, which is what keeps browsing read-only.
func TestHandle_preparationOnly(t *testing.T) {
	t.Parallel()

	clusters := &fakeClusterer{}
	svc := New(clusters, &fakeQueue{}, 0, nil)

	if err := svc.Handle(t.Context(), jobs.Job{ID: 2, Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if clusters.reclusters != 0 {
		t.Errorf("recluster calls = %d, want none", clusters.reclusters)
	}
	if len(clusters.summaryCalls) != 1 {
		t.Errorf("summary calls = %v, want one", clusters.summaryCalls)
	}
}

// TestHandle_emptyPayload treats a job with no payload as a preparation pass
// rather than failing on the decode.
func TestHandle_emptyPayload(t *testing.T) {
	t.Parallel()

	clusters := &fakeClusterer{}
	svc := New(clusters, &fakeQueue{}, 0, nil)

	if err := svc.Handle(t.Context(), jobs.Job{ID: 3}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if clusters.reclusters != 0 || len(clusters.summaryCalls) != 1 {
		t.Errorf("run = %d regroupings / %v batches, want none / one", clusters.reclusters, clusters.summaryCalls)
	}
}

// TestHandle_enqueuesItsSuccessor hands the rest of a large backlog to another
// pass, so one run never holds a worker for the whole library — and the
// successor prepares only, even when this run regrouped.
func TestHandle_enqueuesItsSuccessor(t *testing.T) {
	t.Parallel()

	clusters := &fakeClusterer{runs: []cluster.SummaryRun{{Built: 200, Remaining: 340}}}
	queue := &fakeQueue{}
	svc := New(clusters, queue, 200, nil)

	if err := svc.Handle(t.Context(), jobs.Job{ID: 4, Payload: []byte(`{"recluster":true}`)}); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %s, want one successor", queue.enqueued)
	}
	if decode(t, queue.enqueued[0]).Recluster {
		t.Error("the successor asks to regroup again, want preparation only")
	}
}

// TestHandle_reclusterFailure fails the job so the queue retries it, rather than
// reporting a pass that never grouped anything as done.
func TestHandle_reclusterFailure(t *testing.T) {
	t.Parallel()

	svc := New(&fakeClusterer{reErr: errQueue}, &fakeQueue{}, 0, nil)

	err := svc.Handle(t.Context(), jobs.Job{ID: 5, Payload: []byte(`{"recluster":true}`)})
	if !errors.Is(err, errQueue) {
		t.Errorf("error = %v, want the clusterer's", err)
	}
}

// TestHandle_badPayload fails the job with a clear message rather than silently
// doing the wrong half of the work.
func TestHandle_badPayload(t *testing.T) {
	t.Parallel()

	svc := New(&fakeClusterer{}, &fakeQueue{}, 0, nil)

	if err := svc.Handle(t.Context(), jobs.Job{ID: 6, Payload: []byte("{")}); err == nil {
		t.Error("Handle accepted a malformed payload")
	}
}
