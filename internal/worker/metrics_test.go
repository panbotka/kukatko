package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
)

// fakeObserver records the job lifecycle calls the worker makes so tests can
// assert metrics are emitted. It is safe for concurrent use.
type fakeObserver struct {
	mu       sync.Mutex
	started  []string
	finished []finishedCall
}

// finishedCall captures one JobFinished invocation.
type finishedCall struct {
	jobType string
	outcome string
}

// JobStarted records a started job type.
func (o *fakeObserver) JobStarted(jobType string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.started = append(o.started, jobType)
}

// JobFinished records a finished job's type and outcome.
func (o *fakeObserver) JobFinished(jobType, outcome string, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.finished = append(o.finished, finishedCall{jobType: jobType, outcome: outcome})
}

// snapshot returns copies of the recorded calls.
func (o *fakeObserver) snapshot() ([]string, []finishedCall) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.started...), append([]finishedCall(nil), o.finished...)
}

// newObservedWorker builds a Worker over q and reg with obs as its metrics hook.
func newObservedWorker(q Queue, reg *Registry, obs Observer) *Worker {
	return New(Config{Queue: q, Registry: reg, Concurrency: 1, IDPrefix: "test", Metrics: obs})
}

// TestProcess_recordsJobMetrics verifies a job run emits a started signal and a
// finished signal whose outcome reflects the handler result.
func TestProcess_recordsJobMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		handler     HandlerFunc
		wantOutcome string
	}{
		{
			name:        "success",
			handler:     func(context.Context, jobs.Job) error { return nil },
			wantOutcome: outcomeSuccess,
		},
		{
			name:        "error",
			handler:     func(context.Context, jobs.Job) error { return errors.New("boom") },
			wantOutcome: outcomeError,
		},
		{
			name:        "deferred",
			handler:     func(context.Context, jobs.Job) error { return RetryAfter(time.Minute, errors.New("offline")) },
			wantOutcome: outcomeDeferred,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reg := NewRegistry()
			reg.Register("image_embed", tt.handler)
			obs := &fakeObserver{}
			w := newObservedWorker(newFakeQueue(), reg, obs)

			w.process(context.Background(), "test-0", jobs.Job{ID: 1, Type: "image_embed"})

			started, finished := obs.snapshot()
			if len(started) != 1 || started[0] != "image_embed" {
				t.Errorf("started = %v, want [image_embed]", started)
			}
			if len(finished) != 1 {
				t.Fatalf("finished = %v, want one entry", finished)
			}
			if finished[0].outcome != tt.wantOutcome {
				t.Errorf("outcome = %q, want %q", finished[0].outcome, tt.wantOutcome)
			}
		})
	}
}

// TestProcess_noHandlerSkipsStartMetric verifies a job with no registered
// handler is not counted as started or finished (no handler ran).
func TestProcess_noHandlerSkipsStartMetric(t *testing.T) {
	t.Parallel()

	obs := &fakeObserver{}
	w := newObservedWorker(newFakeQueue(), NewRegistry(), obs)

	w.process(context.Background(), "test-0", jobs.Job{ID: 9, Type: "unknown"})

	started, finished := obs.snapshot()
	if len(started) != 0 || len(finished) != 0 {
		t.Errorf("started=%v finished=%v, want both empty", started, finished)
	}
}
