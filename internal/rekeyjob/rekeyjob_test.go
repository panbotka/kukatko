package rekeyjob

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/rekey"
	"github.com/panbotka/kukatko/internal/worker"
)

// fakeMover answers Status with run (or statusErr) and Step/Finish with the
// canned results, recording the calls and advancing clock by stepTakes on every
// step.
type fakeMover struct {
	run       rekey.Run
	statusErr error

	processed int
	done      bool
	stepErr   error
	finishErr error

	clock     *time.Time
	stepTakes time.Duration

	steps    int
	batch    int
	finishes int
}

// Status returns the canned run.
func (f *fakeMover) Status(context.Context, string) (rekey.Run, error) {
	return f.run, f.statusErr
}

// Step records the call and returns the canned result.
func (f *fakeMover) Step(_ context.Context, _ rekey.Run, batchSize int) (int, bool, error) {
	f.steps++
	f.batch = batchSize
	if f.clock != nil {
		*f.clock = f.clock.Add(f.stepTakes)
	}
	return f.processed, f.done, f.stepErr
}

// Finish records the call and returns the canned result.
func (f *fakeMover) Finish(_ context.Context, run rekey.Run) (rekey.Run, error) {
	f.finishes++
	return run, f.finishErr
}

// newTestService wires a Service over mover with a frozen clock it advances.
func newTestService(mover *fakeMover, perSecond int) *Service {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	mover.clock = &now
	return New(Config{
		Mover: mover, PerSecond: perSecond,
		Clock:  func() time.Time { return *mover.clock },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// rekeyJob is a storage_rekey job of project pj_a.
var rekeyJob = jobs.Job{ID: 7, Type: jobs.TypeStorageRekey, Payload: []byte(`{"project_uid":"pj_a"}`)}

// deferral returns the delay of err when it is a deferral.
func deferral(err error) (time.Duration, bool) {
	var retry *worker.RetryAfterError
	if !errors.As(err, &retry) {
		return 0, false
	}
	return retry.Delay, true
}

func TestHandle_states(t *testing.T) {
	t.Parallel()
	errDB := errors.New("database down")
	tests := []struct {
		name      string
		mover     fakeMover
		wantDelay time.Duration
		deferred  bool
		wantErr   error
		steps     int
		finishes  int
	}{
		{
			name:      "a batch with more to do comes back",
			mover:     fakeMover{run: rekey.Run{State: rekey.StateRunning}, processed: 10},
			wantDelay: time.Second, deferred: true, steps: 1,
		},
		{
			name:      "a paused run is re-read after a minute",
			mover:     fakeMover{run: rekey.Run{State: rekey.StatePaused}},
			wantDelay: PausedRecheck, deferred: true,
		},
		{name: "a done run completes", mover: fakeMover{run: rekey.Run{State: rekey.StateDone}}},
		{name: "a failed run completes", mover: fakeMover{run: rekey.Run{State: rekey.StateFailed}}},
		{name: "no run completes", mover: fakeMover{statusErr: rekey.ErrNoRun}},
		{name: "an unreadable run is retried", mover: fakeMover{statusErr: errDB}, wantErr: errDB},
		{
			name:  "a run the step failed completes",
			mover: fakeMover{run: rekey.Run{State: rekey.StateRunning}, stepErr: rekey.ErrRunFailed}, steps: 1,
		},
		{
			name:     "a run that stopped under the step is re-read at once",
			mover:    fakeMover{run: rekey.Run{State: rekey.StateRunning}, stepErr: rekey.ErrNotRunning},
			deferred: true, steps: 1,
		},
		{
			name:    "a systemic step failure is retried",
			mover:   fakeMover{run: rekey.Run{State: rekey.StateRunning}, stepErr: errDB},
			wantErr: errDB, steps: 1,
		},
		{
			name:  "the end of the pass finishes the run and completes",
			mover: fakeMover{run: rekey.Run{State: rekey.StateRunning}, done: true}, steps: 1, finishes: 1,
		},
		{
			name: "photos left behind complete the failed run",
			mover: fakeMover{
				run: rekey.Run{State: rekey.StateRunning}, done: true, finishErr: rekey.ErrPhotosRemain,
			},
			steps: 1, finishes: 1,
		},
		{
			name: "a pending cleanup at the end is read again",
			mover: fakeMover{
				run: rekey.Run{State: rekey.StateRunning}, done: true, finishErr: rekey.ErrPendingCleanup,
			},
			deferred: true, steps: 1, finishes: 1,
		},
		{
			name: "a failing finish is retried",
			mover: fakeMover{
				run: rekey.Run{State: rekey.StateRunning}, done: true, finishErr: errDB,
			},
			wantErr: errDB, steps: 1, finishes: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mover := tt.mover
			svc := newTestService(&mover, 10)
			err := svc.Handle(t.Context(), rekeyJob)
			delay, deferred := deferral(err)
			switch {
			case tt.deferred:
				if !deferred || delay != tt.wantDelay {
					t.Errorf("Handle = %v, want a deferral of %s", err, tt.wantDelay)
				}
			case tt.wantErr != nil:
				if deferred || !errors.Is(err, tt.wantErr) {
					t.Errorf("Handle = %v, want a plain %v", err, tt.wantErr)
				}
			case err != nil:
				t.Errorf("Handle = %v, want completion", err)
			}
			if mover.steps != tt.steps || mover.finishes != tt.finishes {
				t.Errorf("steps %d, finishes %d; want %d, %d", mover.steps, mover.finishes, tt.steps, tt.finishes)
			}
		})
	}
}

// TestHandle_pace verifies a batch takes per_second photos and the job comes
// back once the batch's share of the pace has passed — at once when the batch
// was slower than the pace.
func TestHandle_pace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		perSecond int
		processed int
		takes     time.Duration
		wantBatch int
		wantDelay time.Duration
	}{
		{name: "a fast full batch waits out the second", perSecond: 10, processed: 10, takes: 300 * time.Millisecond,
			wantBatch: 10, wantDelay: 700 * time.Millisecond},
		{name: "a short batch waits its share", perSecond: 10, processed: 4, takes: 100 * time.Millisecond,
			wantBatch: 10, wantDelay: 300 * time.Millisecond},
		{name: "a slow batch comes back at once", perSecond: 10, processed: 10, takes: 3 * time.Second,
			wantBatch: 10, wantDelay: 0},
		{name: "no configured pace is the default", perSecond: 0, processed: 10, takes: 0,
			wantBatch: DefaultPerSecond, wantDelay: time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mover := fakeMover{run: rekey.Run{State: rekey.StateRunning}, processed: tt.processed, stepTakes: tt.takes}
			svc := newTestService(&mover, tt.perSecond)
			delay, deferred := deferral(svc.Handle(t.Context(), rekeyJob))
			if !deferred || delay != tt.wantDelay {
				t.Errorf("deferral = %s (%v), want %s", delay, deferred, tt.wantDelay)
			}
			if mover.batch != tt.wantBatch {
				t.Errorf("batch = %d, want %d", mover.batch, tt.wantBatch)
			}
		})
	}
}

// TestHandle_noProject verifies a payload naming no project fails terminally
// without touching any run.
func TestHandle_noProject(t *testing.T) {
	t.Parallel()
	mover := fakeMover{run: rekey.Run{State: rekey.StateRunning}}
	svc := newTestService(&mover, 10)
	err := svc.Handle(t.Context(), jobs.Job{ID: 1, Type: jobs.TypeStorageRekey, Payload: []byte(`{}`)})
	var terminal *worker.TerminalError
	if !errors.As(err, &terminal) || !errors.Is(err, errNoProject) {
		t.Errorf("Handle = %v, want a terminal errNoProject", err)
	}
	if mover.steps != 0 {
		t.Errorf("a job naming no project stepped %d times", mover.steps)
	}
}

// TestNew_requiresMover verifies the wiring bug of a missing mover panics.
func TestNew_requiresMover(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("New without a Mover did not panic")
		}
	}()
	New(Config{})
}
