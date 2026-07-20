package worker

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
)

// fakeQueue is an in-memory Queue for unit-testing the worker's dispatch and
// lifecycle without a database. It is safe for concurrent use.
type fakeQueue struct {
	mu        sync.Mutex
	pending   []jobs.Job
	completed []int64
	failed    map[int64]error
	deferred  map[int64]time.Duration
	recovered int
	// beats counts Heartbeat calls per job id.
	beats map[int64]int
	// owner records, per job id, the worker id of the last lifecycle write, so
	// tests can assert the outcome was attributed to the right worker.
	owner map[int64]string
	// lockLost, when set, makes every ownership-guarded call for that job id
	// report jobs.ErrLockLost, simulating a job reclaimed by another worker.
	lockLost map[int64]bool
}

// newFakeQueue returns a fakeQueue seeded with the given pending jobs.
func newFakeQueue(pending ...jobs.Job) *fakeQueue {
	return &fakeQueue{
		pending:  pending,
		failed:   make(map[int64]error),
		deferred: make(map[int64]time.Duration),
		beats:    make(map[int64]int),
		owner:    make(map[int64]string),
		lockLost: make(map[int64]bool),
	}
}

// Claim pops and returns the next pending job, or jobs.ErrNoJobs when empty.
func (q *fakeQueue) Claim(_ context.Context, _ string, _ ...string) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) == 0 {
		return jobs.Job{}, jobs.ErrNoJobs
	}
	job := q.pending[0]
	q.pending = q.pending[1:]
	return job, nil
}

// Complete records id as completed under workerID, or reports jobs.ErrLockLost
// when the job has been marked as reclaimed.
func (q *fakeQueue) Complete(_ context.Context, id int64, workerID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lockLost[id] {
		return jobs.ErrLockLost
	}
	q.completed = append(q.completed, id)
	q.owner[id] = workerID
	return nil
}

// Fail records the failure cause for id under workerID and returns a placeholder
// job, or reports jobs.ErrLockLost when the job has been marked as reclaimed.
func (q *fakeQueue) Fail(_ context.Context, id int64, workerID string, cause error) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lockLost[id] {
		return jobs.Job{}, jobs.ErrLockLost
	}
	q.failed[id] = cause
	q.owner[id] = workerID
	return jobs.Job{ID: id}, nil
}

// Defer records the deferral delay for id under workerID and returns a
// placeholder job, or reports jobs.ErrLockLost when the job has been marked as
// reclaimed.
func (q *fakeQueue) Defer(
	_ context.Context, id int64, workerID string, delay time.Duration,
) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lockLost[id] {
		return jobs.Job{}, jobs.ErrLockLost
	}
	q.deferred[id] = delay
	q.owner[id] = workerID
	return jobs.Job{ID: id}, nil
}

// Heartbeat counts a lock refresh for id, or reports jobs.ErrLockLost when the
// job has been marked as reclaimed.
func (q *fakeQueue) Heartbeat(_ context.Context, id int64, _ string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.lockLost[id] {
		return jobs.ErrLockLost
	}
	q.beats[id]++
	return nil
}

// beatCount returns how many heartbeats were recorded for id.
func (q *fakeQueue) beatCount(id int64) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.beats[id]
}

// loseLock marks id as reclaimed by another worker, so every subsequent
// ownership-guarded call for it fails with jobs.ErrLockLost.
func (q *fakeQueue) loseLock(id int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lockLost[id] = true
}

// ownerOf returns the worker id credited with id's last lifecycle write.
func (q *fakeQueue) ownerOf(id int64) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.owner[id]
}

// RecoverStaleLocks counts the call and recovers nothing.
func (q *fakeQueue) RecoverStaleLocks(_ context.Context, _ time.Duration) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.recovered++
	return 0, nil
}

// snapshot returns copies of the recorded completions and failures.
func (q *fakeQueue) snapshot() ([]int64, map[int64]error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	done := append([]int64(nil), q.completed...)
	failed := make(map[int64]error, len(q.failed))
	maps.Copy(failed, q.failed)
	return done, failed
}

// deferredSnapshot returns a copy of the recorded deferrals.
func (q *fakeQueue) deferredSnapshot() map[int64]time.Duration {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make(map[int64]time.Duration, len(q.deferred))
	maps.Copy(out, q.deferred)
	return out
}

// newTestWorker builds a Worker over q with reg and fast intervals for tests.
func newTestWorker(q Queue, reg *Registry) *Worker {
	return New(Config{
		Queue:             q,
		Registry:          reg,
		Concurrency:       1,
		PollInterval:      time.Millisecond,
		StaleScanInterval: time.Millisecond,
		IDPrefix:          "test",
	})
}

// TestProcess_completesOnSuccess verifies a successful handler completes the job.
func TestProcess_completesOnSuccess(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("ok", func(context.Context, jobs.Job) error { return nil })
	w := newTestWorker(q, reg)

	w.process(context.Background(), "test-0", jobs.Job{ID: 7, Type: "ok"})

	done, failed := q.snapshot()
	if len(done) != 1 || done[0] != 7 {
		t.Errorf("completed = %v, want [7]", done)
	}
	if len(failed) != 0 {
		t.Errorf("failed = %v, want empty", failed)
	}
}

// TestProcess_failsOnHandlerError verifies a handler error fails the job with
// that error.
func TestProcess_failsOnHandlerError(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("bad", func(context.Context, jobs.Job) error { return boom })
	w := newTestWorker(q, reg)

	w.process(context.Background(), "test-0", jobs.Job{ID: 3, Type: "bad"})

	_, failed := q.snapshot()
	if !errors.Is(failed[3], boom) {
		t.Errorf("failed[3] = %v, want boom", failed[3])
	}
}

// TestProcess_defersOnRetryAfter verifies a handler returning a RetryAfterError
// defers the job (no attempt burned) instead of failing it.
func TestProcess_defersOnRetryAfter(t *testing.T) {
	t.Parallel()

	cause := errors.New("box offline")
	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("transient", func(context.Context, jobs.Job) error {
		return RetryAfter(90*time.Second, cause)
	})
	w := newTestWorker(q, reg)

	w.process(context.Background(), "test-0", jobs.Job{ID: 4, Type: "transient"})

	done, failed := q.snapshot()
	if len(done) != 0 || len(failed) != 0 {
		t.Errorf("retry-after job recorded as done/failed: completed=%v failed=%v", done, failed)
	}
	deferred := q.deferredSnapshot()
	if got := deferred[4]; got != 90*time.Second {
		t.Errorf("deferred[4] = %v, want 90s", got)
	}
}

// TestProcess_failsOnMissingHandler verifies a job with no registered handler is
// failed with ErrNoHandler.
func TestProcess_failsOnMissingHandler(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	w := newTestWorker(q, NewRegistry())

	w.process(context.Background(), "test-0", jobs.Job{ID: 5, Type: "unknown"})

	_, failed := q.snapshot()
	if !errors.Is(failed[5], ErrNoHandler) {
		t.Errorf("failed[5] = %v, want ErrNoHandler", failed[5])
	}
}

// TestProcess_recoversHandlerPanic verifies a panicking handler is turned into a
// job failure tagged ErrHandlerPanic rather than crashing the worker.
func TestProcess_recoversHandlerPanic(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("panic", func(context.Context, jobs.Job) error { panic("kaboom") })
	w := newTestWorker(q, reg)

	w.process(context.Background(), "test-0", jobs.Job{ID: 9, Type: "panic"})

	_, failed := q.snapshot()
	if !errors.Is(failed[9], ErrHandlerPanic) {
		t.Errorf("failed[9] = %v, want ErrHandlerPanic", failed[9])
	}
}

// TestProcess_abandonsOnShutdown verifies that when the context is already
// cancelled and the handler returns an error, the job is neither completed nor
// failed — it is abandoned for the queue's stale-lock recovery to requeue.
func TestProcess_abandonsOnShutdown(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("slow", func(ctx context.Context, _ jobs.Job) error { return ctx.Err() })
	w := newTestWorker(q, reg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.process(ctx, "test-0", jobs.Job{ID: 11, Type: "slow"})

	done, failed := q.snapshot()
	if len(done) != 0 || len(failed) != 0 {
		t.Errorf("abandoned job recorded: completed=%v failed=%v", done, failed)
	}
}

// TestProcess_defersOnShutdown verifies that a deferral coinciding with shutdown
// is still written: a RetryAfterError must never burn a retry attempt, so unlike
// a genuine handler error it may not be abandoned to stale-lock recovery (which
// would increment attempts and eventually dead-letter a job that is merely
// waiting for the embeddings box).
func TestProcess_defersOnShutdown(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("transient", func(context.Context, jobs.Job) error {
		return RetryAfter(2*time.Minute, errors.New("box offline"))
	})
	w := newTestWorker(q, reg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.process(ctx, "test-0", jobs.Job{ID: 12, Type: "transient"})

	done, failed := q.snapshot()
	if len(done) != 0 || len(failed) != 0 {
		t.Errorf("deferral at shutdown recorded as done/failed: completed=%v failed=%v", done, failed)
	}
	if got := q.deferredSnapshot()[12]; got != 2*time.Minute {
		t.Errorf("deferred[12] = %v, want 2m", got)
	}
}

// TestProcess_heartbeatsWhileHandlerRuns verifies a long-running handler has its
// lock refreshed, which is what keeps stale-lock recovery from reclaiming a job
// that is still being worked.
func TestProcess_heartbeatsWhileHandlerRuns(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("slow", func(ctx context.Context, job jobs.Job) error {
		// Outlive at least one heartbeat tick, then return.
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if q.beatCount(job.ID) > 0 {
				return nil
			}
			time.Sleep(time.Millisecond)
		}
		return errors.New("no heartbeat within 2s")
	})
	// A tiny StaleAfter floors the heartbeat at minHeartbeatInterval.
	w := New(Config{
		Queue: q, Registry: reg, Concurrency: 1,
		PollInterval: time.Millisecond, StaleAfter: time.Millisecond, IDPrefix: "test",
	})

	w.process(context.Background(), "test-0", jobs.Job{ID: 21, Type: "slow"})

	if n := q.beatCount(21); n == 0 {
		t.Error("job 21 was never heartbeated")
	}
	done, failed := q.snapshot()
	if len(done) != 1 || done[0] != 21 {
		t.Errorf("completed = %v failed = %v, want [21] and no failures", done, failed)
	}
}

// TestProcess_dropsResultWhenLockLost verifies that a job reclaimed while its
// former owner was still working has that owner's late result dropped rather
// than written over the new owner's run.
func TestProcess_dropsResultWhenLockLost(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	q.loseLock(31)
	reg := NewRegistry()
	reg.Register("ok", func(context.Context, jobs.Job) error { return nil })
	w := newTestWorker(q, reg)

	w.process(context.Background(), "test-0", jobs.Job{ID: 31, Type: "ok"})

	done, failed := q.snapshot()
	if len(done) != 0 || len(failed) != 0 {
		t.Errorf("late result written: completed=%v failed=%v", done, failed)
	}
}

// TestProcess_attributesOutcomeToWorker verifies the claiming worker's id is
// threaded through to the outcome write, which is what lets the queue reject a
// stale worker's late Complete/Fail/Defer.
func TestProcess_attributesOutcomeToWorker(t *testing.T) {
	t.Parallel()

	q := newFakeQueue()
	reg := NewRegistry()
	reg.Register("ok", func(context.Context, jobs.Job) error { return nil })
	w := newTestWorker(q, reg)

	w.process(context.Background(), "test-7", jobs.Job{ID: 41, Type: "ok"})

	if got := q.ownerOf(41); got != "test-7" {
		t.Errorf("owner of job 41 = %q, want %q", got, "test-7")
	}
}

// TestHeartbeatInterval verifies the heartbeat fires several times per stale
// window and is floored so a short StaleAfter cannot cause a busy loop.
func TestHeartbeatInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		staleAfter time.Duration
		want       time.Duration
	}{
		{"default stale window divides by three", 5 * time.Minute, 100 * time.Second},
		{"short window is floored", time.Millisecond, minHeartbeatInterval},
		{"exactly at the floor", 3 * minHeartbeatInterval, minHeartbeatInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := New(Config{
				Queue: newFakeQueue(), Registry: NewRegistry(), StaleAfter: tt.staleAfter,
			})
			if got := w.heartbeatInterval(); got != tt.want {
				t.Errorf("heartbeatInterval() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRun_drainsAndStops verifies Run claims and completes queued jobs, then
// returns promptly once its context is cancelled.
func TestRun_drainsAndStops(t *testing.T) {
	t.Parallel()

	q := newFakeQueue(
		jobs.Job{ID: 1, Type: TypeNoop},
		jobs.Job{ID: 2, Type: TypeNoop},
		jobs.Job{ID: 3, Type: TypeNoop},
	)
	reg := NewRegistry()
	RegisterBuiltins(reg)
	w := newTestWorker(q, reg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	waitFor(t, func() bool {
		completed, _ := q.snapshot()
		return len(completed) == 3
	})

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancellation")
	}
}

// TestNew_panicsWithoutDeps verifies New rejects a nil Queue or Registry.
func TestNew_panicsWithoutDeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{"nil queue", Config{Registry: NewRegistry()}},
		{"nil registry", Config{Queue: newFakeQueue()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("New did not panic")
				}
			}()
			New(tt.cfg)
		})
	}
}

// waitFor polls cond up to two seconds, failing the test if it never holds.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}
