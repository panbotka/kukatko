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
	recovered int
}

// newFakeQueue returns a fakeQueue seeded with the given pending jobs.
func newFakeQueue(pending ...jobs.Job) *fakeQueue {
	return &fakeQueue{pending: pending, failed: make(map[int64]error)}
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

// Complete records id as completed.
func (q *fakeQueue) Complete(_ context.Context, id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completed = append(q.completed, id)
	return nil
}

// Fail records the failure cause for id and returns a placeholder job.
func (q *fakeQueue) Fail(_ context.Context, id int64, cause error) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failed[id] = cause
	return jobs.Job{ID: id}, nil
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
