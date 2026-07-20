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

// RetryAfterError is the error a handler returns to tell the worker to requeue
// the job to run after Delay WITHOUT counting a failed attempt (the queue's
// Defer, not Fail). Handlers use it for transient, no-fault conditions — chiefly
// the embeddings box being offline — so a job waits in the queue for the
// condition to clear without ever exhausting its retry budget. Cause is the
// underlying error, kept for logging and errors.Is/As unwrapping.
type RetryAfterError struct {
	// Delay is how long to wait before the job becomes runnable again.
	Delay time.Duration
	// Cause is the transient error that triggered the deferral.
	Cause error
}

// RetryAfter wraps cause as a RetryAfterError requesting the job be requeued
// after delay without burning a retry attempt. It is the constructor handlers
// use to signal a transient, no-fault retry.
func RetryAfter(delay time.Duration, cause error) error {
	return &RetryAfterError{Delay: delay, Cause: cause}
}

// Error implements error, describing the deferral and its cause.
func (e *RetryAfterError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("worker: retry after %s", e.Delay)
	}
	return fmt.Sprintf("worker: retry after %s: %v", e.Delay, e.Cause)
}

// Unwrap exposes the underlying cause so errors.Is/As can match it.
func (e *RetryAfterError) Unwrap() error {
	return e.Cause
}

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
	// heartbeatDivisor derives the heartbeat interval from StaleAfter: a running
	// job refreshes its lock this many times per stale window, so a couple of
	// missed or slow beats still cannot make a live job look abandoned.
	heartbeatDivisor = 3
	// minHeartbeatInterval floors the derived heartbeat interval so a very short
	// StaleAfter (tests, aggressive configs) cannot turn into a busy loop of
	// UPDATEs.
	minHeartbeatInterval = 100 * time.Millisecond
)

// Job outcome label values reported to the Observer.
const (
	// outcomeSuccess marks a handler that returned nil.
	outcomeSuccess = "success"
	// outcomeError marks a handler that returned a (non-deferral) error or a
	// job whose type had no handler.
	outcomeError = "error"
	// outcomeDeferred marks a job requeued without a burned attempt because the
	// handler asked to retry later (the box was offline).
	outcomeDeferred = "deferred"
)

// Observer receives background-job lifecycle signals so the worker stays
// decoupled from the metrics package: it is satisfied by *metrics.Registry and
// faked in tests. A nil Observer disables instrumentation. Implementations must
// be safe for concurrent use.
type Observer interface {
	// JobStarted records that a job of jobType was dispatched to its handler.
	JobStarted(jobType string)
	// JobFinished records that a job of jobType finished with outcome (one of
	// "success", "error", "deferred") after running for d.
	JobFinished(jobType, outcome string, d time.Duration)
}

// nopObserver is the default Observer used when Config.Metrics is nil; every
// method is a no-op.
type nopObserver struct{}

// JobStarted does nothing.
func (nopObserver) JobStarted(string) {}

// JobFinished does nothing.
func (nopObserver) JobFinished(string, string, time.Duration) {}

// Queue is the subset of jobs.Store the worker depends on, expressed as an
// interface so the runtime can be unit-tested with a fake.
type Queue interface {
	// Claim atomically picks and locks the next runnable job for workerID,
	// optionally restricted to the given types, or returns jobs.ErrNoJobs.
	Claim(ctx context.Context, workerID string, types ...string) (jobs.Job, error)
	// Complete marks a running job owned by workerID done.
	Complete(ctx context.Context, id int64, workerID string) error
	// Fail records a failed attempt on a job owned by workerID, requeuing with
	// backoff or dead-lettering.
	Fail(ctx context.Context, id int64, workerID string, cause error) (jobs.Job, error)
	// Defer requeues a job owned by workerID to run after delay without counting
	// a failed attempt.
	Defer(ctx context.Context, id int64, workerID string, delay time.Duration) (jobs.Job, error)
	// Heartbeat refreshes the lock timestamp of a running job owned by workerID
	// so a long-running job is not mistaken for an abandoned one.
	Heartbeat(ctx context.Context, id int64, workerID string) error
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
	// Metrics receives per-job lifecycle signals. Nil disables instrumentation.
	Metrics Observer
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
	metrics           Observer
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
		metrics:           orDefaultObserver(cfg.Metrics),
	}
}

// orDefaultObserver returns obs when non-nil, otherwise a no-op Observer so the
// worker never has to nil-check its metrics hook.
func orDefaultObserver(obs Observer) Observer {
	if obs != nil {
		return obs
	}
	return nopObserver{}
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
// goroutine has stopped. New jobs are not claimed after cancellation; a job in
// flight when shutdown begins keeps its lock heartbeated until its handler
// returns, and is then either recorded (a success or a deferral) or abandoned to
// the queue's stale-lock recovery (a genuine error). Run always returns nil: a
// cancelled context is a normal, graceful stop, not an error.
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

// process dispatches one claimed job to its handler and records the outcome,
// refreshing the job's lock on a heartbeat for as long as the handler runs so a
// job that legitimately outlives StaleAfter is not recovered underneath itself.
// A job whose type has no handler is failed. A job whose handler returned a
// genuine error while ctx was already cancelled is abandoned without a status
// write, leaving its lock to be recovered by the queue; a deferral is still
// written, because a RetryAfterError must never burn a retry attempt.
func (w *Worker) process(ctx context.Context, workerID string, job jobs.Job) {
	handler, ok := w.registry.Handler(job.Type)
	if !ok {
		w.record(ctx, workerID, job, fmt.Errorf("%w: %q", ErrNoHandler, job.Type))
		return
	}
	w.metrics.JobStarted(job.Type)
	start := time.Now()
	stopHeartbeat := w.startHeartbeat(ctx, workerID, job)
	err := runHandler(ctx, handler, job)
	stopHeartbeat()
	if err != nil && ctx.Err() != nil && !isRetryAfter(err) {
		log.Printf("worker %s: job %d (%s) abandoned on shutdown", workerID, job.ID, job.Type)
		return
	}
	w.metrics.JobFinished(job.Type, outcomeFor(err), time.Since(start))
	w.record(ctx, workerID, job, err)
}

// heartbeatInterval is how often a running job refreshes its lock: a fraction of
// the stale window, floored so an aggressively short StaleAfter cannot turn the
// heartbeat into a busy loop.
func (w *Worker) heartbeatInterval() time.Duration {
	interval := w.staleAfter / heartbeatDivisor
	if interval < minHeartbeatInterval {
		return minHeartbeatInterval
	}
	return interval
}

// startHeartbeat launches a goroutine that refreshes job's lock every
// heartbeatInterval and returns a function that stops it and waits for the
// goroutine to exit — so no heartbeat can race the outcome write that follows.
// The ticker runs on a shutdown-immune context: while the process drains, a
// handler still working must keep its lock, so only the returned stop function
// ends it.
func (w *Worker) startHeartbeat(ctx context.Context, workerID string, job jobs.Job) func() {
	beatCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.heartbeatLoop(beatCtx, workerID, job)
	}()
	return func() {
		cancel()
		<-done
	}
}

// heartbeatLoop refreshes job's lock until ctx is cancelled. It gives up early
// if the queue reports the job is gone or was reclaimed — there is nothing left
// to keep alive, and the outcome write will drop the result for the same reason.
func (w *Worker) heartbeatLoop(ctx context.Context, workerID string, job jobs.Job) {
	interval := w.heartbeatInterval()
	for sleep(ctx, interval) {
		err := w.queue.Heartbeat(ctx, job.ID, workerID)
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return
		case errors.Is(err, jobs.ErrLockLost), errors.Is(err, jobs.ErrJobNotFound):
			log.Printf("worker %s: job %d (%s) lost its lock while running: %v",
				workerID, job.ID, job.Type, err)
			return
		default:
			log.Printf("worker %s: heartbeating job %d: %v", workerID, job.ID, err)
		}
	}
}

// outcomeFor classifies a handler's result into an Observer outcome label:
// a nil error is success, a RetryAfterError is a deferral, anything else is an
// error.
func outcomeFor(err error) string {
	switch {
	case err == nil:
		return outcomeSuccess
	case isRetryAfter(err):
		return outcomeDeferred
	default:
		return outcomeError
	}
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

// record writes a job's outcome to the queue under workerID: Complete when cause
// is nil, Defer (no attempt burned) when cause is a RetryAfterError, otherwise
// Fail. The write uses a fresh, shutdown-immune context with a short timeout so a
// result computed just before shutdown is still persisted. Every write is guarded
// by the worker id, so if the job was meanwhile reclaimed the result is dropped
// instead of clobbering the new owner's run.
func (w *Worker) record(ctx context.Context, workerID string, job jobs.Job, cause error) {
	bookCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	switch {
	case cause == nil:
		w.logOutcomeWrite(job, "completing", w.queue.Complete(bookCtx, job.ID, workerID))
	case isRetryAfter(cause):
		w.deferJob(bookCtx, workerID, job, cause)
	default:
		_, err := w.queue.Fail(bookCtx, job.ID, workerID, cause)
		w.logOutcomeWrite(job, fmt.Sprintf("failing (cause %v)", cause), err)
	}
}

// logOutcomeWrite reports the result of an outcome write. A lost lock is not a
// malfunction — stale-lock recovery handed the job to another worker and
// dropping this result is exactly the intended behaviour — so it is logged as
// such rather than as a failed write.
func (w *Worker) logOutcomeWrite(job jobs.Job, action string, err error) {
	switch {
	case err == nil:
	case errors.Is(err, jobs.ErrLockLost):
		log.Printf("worker: job %d (%s) was reclaimed by another worker; dropping result",
			job.ID, job.Type)
	default:
		log.Printf("worker: %s job %d: %v", action, job.ID, err)
	}
}

// deferJob requeues job for a later run without counting an attempt, used when a
// handler returns a RetryAfterError for a transient, no-fault condition.
func (w *Worker) deferJob(ctx context.Context, workerID string, job jobs.Job, cause error) {
	var ra *RetryAfterError
	_ = errors.As(cause, &ra)
	_, err := w.queue.Defer(ctx, job.ID, workerID, ra.Delay)
	w.logOutcomeWrite(job, fmt.Sprintf("deferring (cause %v)", cause), err)
}

// isRetryAfter reports whether err is (or wraps) a RetryAfterError.
func isRetryAfter(err error) bool {
	var ra *RetryAfterError
	return errors.As(err, &ra)
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
