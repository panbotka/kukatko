package worker

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/jobs"
)

// Sentinel errors recorded as a job's last_error when dispatch itself fails
// (as opposed to the handler returning an error). Both are matchable with
// errors.Is by callers and tests.
var (
	// ErrNoHandler indicates a job was claimed whose type has no registered
	// handler; the job is failed so the queue retries or dead-letters it.
	ErrNoHandler = errors.New("worker: no handler registered for job type")
	// ErrHandlerPanic indicates a handler panicked; it is recovered and turned
	// into a job failure so one bad handler cannot crash the worker.
	ErrHandlerPanic = errors.New("worker: handler panicked")
)

const (
	// defaultConcurrency is the number of worker goroutines when Config.Concurrency
	// is not positive.
	defaultConcurrency = 2
	// defaultPollInterval is how long an idle worker waits before polling Claim
	// again when the queue is empty.
	defaultPollInterval = 2 * time.Second
	// defaultStaleAfter is the lock age beyond which a running job is presumed
	// abandoned and recovered.
	defaultStaleAfter = 5 * time.Minute
	// defaultStaleScanInterval is how often stale-lock recovery runs.
	defaultStaleScanInterval = time.Minute
	// bookkeepingTimeout bounds the Complete/Fail write that records a job's
	// outcome; it uses a shutdown-immune context so an in-flight result is still
	// persisted while the process drains.
	bookkeepingTimeout = 10 * time.Second
)

// Queue is the subset of jobs.Store the worker depends on, expressed as an
// interface so the runtime can be unit-tested with a fake.
type Queue interface {
	// Claim atomically picks and locks the next runnable job for workerID,
	// optionally restricted to the given types, or returns jobs.ErrNoJobs.
	Claim(ctx context.Context, workerID string, types ...string) (jobs.Job, error)
	// Complete marks a running job done.
	Complete(ctx context.Context, id int64) error
	// Fail records a failed attempt, requeuing with backoff or dead-lettering.
	Fail(ctx context.Context, id int64, cause error) (jobs.Job, error)
	// RecoverStaleLocks requeues running jobs whose lock is older than staleAfter.
	RecoverStaleLocks(ctx context.Context, staleAfter time.Duration) (int64, error)
}

// Config bundles the worker's dependencies and tuning knobs. Queue and Registry
// are required; the remaining fields fall back to package defaults when unset.
type Config struct {
	// Queue is the persistent job queue the worker drains.
	Queue Queue
	// Registry resolves a job type to its handler.
	Registry *Registry
	// Concurrency is the number of jobs processed in parallel. <= 0 uses
	// defaultConcurrency.
	Concurrency int
	// PollInterval is the idle delay between empty Claim attempts. <= 0 uses
	// defaultPollInterval.
	PollInterval time.Duration
	// StaleAfter is the lock age past which a job is recovered. <= 0 uses
	// defaultStaleAfter.
	StaleAfter time.Duration
	// StaleScanInterval is how often stale-lock recovery runs. <= 0 uses
	// defaultStaleScanInterval.
	StaleScanInterval time.Duration
	// IDPrefix prefixes the per-goroutine worker id stamped on claimed jobs.
	// Empty uses "<hostname>-<pid>".
	IDPrefix string
}

// Worker polls the queue with bounded concurrency and dispatches claimed jobs to
// registered handlers until its Run context is cancelled.
type Worker struct {
	queue             Queue
	registry          *Registry
	concurrency       int
	pollInterval      time.Duration
	staleAfter        time.Duration
	staleScanInterval time.Duration
	idPrefix          string
}

// New constructs a Worker from cfg, applying defaults for any unset tuning knob.
// It panics if Queue or Registry is nil, since neither has a sensible default.
func New(cfg Config) *Worker {
	if cfg.Queue == nil {
		panic("worker: New requires a non-nil Queue")
	}
	if cfg.Registry == nil {
		panic("worker: New requires a non-nil Registry")
	}
	return &Worker{
		queue:             cfg.Queue,
		registry:          cfg.Registry,
		concurrency:       orDefaultInt(cfg.Concurrency, defaultConcurrency),
		pollInterval:      orDefaultDuration(cfg.PollInterval, defaultPollInterval),
		staleAfter:        orDefaultDuration(cfg.StaleAfter, defaultStaleAfter),
		staleScanInterval: orDefaultDuration(cfg.StaleScanInterval, defaultStaleScanInterval),
		idPrefix:          orDefaultPrefix(cfg.IDPrefix),
	}
}

// orDefaultInt returns v when positive, otherwise fallback.
func orDefaultInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

// orDefaultDuration returns v when positive, otherwise fallback.
func orDefaultDuration(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}

// orDefaultPrefix returns prefix when non-empty, otherwise a "<hostname>-<pid>"
// identifier so distinct processes claim under distinct worker ids.
func orDefaultPrefix(prefix string) string {
	if prefix != "" {
		return prefix
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "kukatko"
	}
	return host + "-" + strconv.Itoa(os.Getpid())
}

// Run starts the worker goroutines plus the stale-lock recovery loop and blocks
// until ctx is cancelled (for example on SIGINT/SIGTERM), then returns once every
// goroutine has stopped. New jobs are not claimed after cancellation; a job
// in flight when shutdown begins is abandoned and later recovered by the queue's
// stale-lock recovery. Run always returns nil: a cancelled context is a normal,
// graceful stop, not an error.
func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := range w.concurrency {
		workerID := w.idPrefix + "-" + strconv.Itoa(i)
		wg.Go(func() { w.loop(ctx, workerID) })
	}
	wg.Go(func() { w.recoverLoop(ctx) })
	wg.Wait()
	return nil
}

// loop is one worker goroutine: it claims and processes jobs until ctx is
// cancelled, backing off for pollInterval whenever the queue is empty or a claim
// transiently fails.
func (w *Worker) loop(ctx context.Context, workerID string) {
	for ctx.Err() == nil {
		job, err := w.queue.Claim(ctx, workerID, w.registry.Types()...)
		switch {
		case errors.Is(err, jobs.ErrNoJobs):
			if !sleep(ctx, w.pollInterval) {
				return
			}
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker %s: claim failed: %v", workerID, err)
			if !sleep(ctx, w.pollInterval) {
				return
			}
		default:
			w.process(ctx, workerID, job)
		}
	}
}

// process dispatches one claimed job to its handler and records the outcome. A
// job whose type has no handler is failed. A job interrupted by shutdown (its
// handler returned while ctx was already cancelled) is abandoned without a
// status write, leaving its lock to be recovered by the queue.
func (w *Worker) process(ctx context.Context, workerID string, job jobs.Job) {
	handler, ok := w.registry.Handler(job.Type)
	if !ok {
		w.record(ctx, job, fmt.Errorf("%w: %q", ErrNoHandler, job.Type))
		return
	}
	err := runHandler(ctx, handler, job)
	if err != nil && ctx.Err() != nil {
		log.Printf("worker %s: job %d (%s) abandoned on shutdown", workerID, job.ID, job.Type)
		return
	}
	w.record(ctx, job, err)
}

// runHandler invokes handler, converting a panic into an ErrHandlerPanic error so
// a single misbehaving handler fails only its job rather than crashing the
// worker goroutine.
func runHandler(ctx context.Context, handler HandlerFunc, job jobs.Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", ErrHandlerPanic, r)
		}
	}()
	return handler(ctx, job)
}

// record writes a job's terminal outcome to the queue: Complete when cause is
// nil, otherwise Fail. The write uses a fresh, shutdown-immune context with a
// short timeout so a result computed just before shutdown is still persisted.
func (w *Worker) record(ctx context.Context, job jobs.Job, cause error) {
	bookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if cause == nil {
		if err := w.queue.Complete(bookCtx, job.ID); err != nil {
			log.Printf("worker: completing job %d: %v", job.ID, err)
		}
		return
	}
	if _, err := w.queue.Fail(bookCtx, job.ID, cause); err != nil {
		log.Printf("worker: failing job %d (cause %v): %v", job.ID, cause, err)
	}
}

// recoverLoop periodically requeues jobs whose lock has gone stale (their worker
// died), until ctx is cancelled.
func (w *Worker) recoverLoop(ctx context.Context) {
	for sleep(ctx, w.staleScanInterval) {
		n, err := w.queue.RecoverStaleLocks(ctx, w.staleAfter)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("worker: recovering stale locks: %v", err)
			continue
		}
		if n > 0 {
			log.Printf("worker: recovered %d stale job lock(s)", n)
		}
	}
}

// sleep waits for d or until ctx is cancelled, returning true if the full delay
// elapsed and false if ctx was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
