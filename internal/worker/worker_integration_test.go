//go:build integration

package worker_test

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/worker"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they intentionally do not run in parallel.

// jobType is the custom job type used by these tests; it deduplicates per
// photo_uid like the real photo job types.
const jobType = "test_job"

// newStore returns a jobs.Store over a freshly truncated integration database.
func newStore(t *testing.T) (*jobs.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return jobs.NewStore(db.Pool()), db
}

// payload builds a {"photo_uid": uid} JSON payload so enqueued jobs deduplicate
// per photo.
func payload(t *testing.T, uid string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"photo_uid": uid})
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}
	return raw
}

// runWorker starts w.Run in the background and returns a cancel function plus a
// channel that closes once Run has returned, so a test can assert clean shutdown.
func runWorker(w *worker.Worker) (context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(stopped)
	}()
	return cancel, stopped
}

// waitForState polls until the job reaches want or the deadline passes.
func waitForState(t *testing.T, store *jobs.Store, id int64, want jobs.State) jobs.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := store.Get(t.Context(), id)
		if err != nil {
			t.Fatalf("Get(%d): %v", id, err)
		}
		if job.State == want {
			return job
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %d did not reach state %q within 5s", id, want)
	return jobs.Job{}
}

// keepRunnable forces queued jobs runnable immediately until stop is closed, so a
// retry-exhaustion test does not wait out the queue's exponential backoff.
func keepRunnable(db *database.DB, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
			_, _ = db.Pool().Exec(context.Background(),
				"UPDATE jobs SET run_after = now() - interval '1 hour' WHERE state = 'queued'")
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// fastWorker builds a worker over store with reg and short intervals, and
// stale-lock recovery effectively disabled so it does not interfere.
func fastWorker(store *jobs.Store, reg *worker.Registry) *worker.Worker {
	return worker.New(worker.Config{
		Queue:             store,
		Registry:          reg,
		Concurrency:       2,
		PollInterval:      2 * time.Millisecond,
		StaleAfter:        time.Hour,
		StaleScanInterval: time.Hour,
		IDPrefix:          "itest",
	})
}

// TestWorker_runsJobsToCompletion verifies the worker claims enqueued jobs,
// dispatches them to the registered handler, and marks them done.
func TestWorker_runsJobsToCompletion(t *testing.T) {
	store, _ := newStore(t)
	reg := worker.NewRegistry()
	seen := make(chan int64, 8)
	reg.Register(jobType, func(_ context.Context, job jobs.Job) error {
		seen <- job.ID
		return nil
	})

	const total = 5
	ids := make([]int64, 0, total)
	for i := range total {
		job, err := store.Enqueue(t.Context(), jobType, payload(t, "c"+strconv.Itoa(i)), jobs.EnqueueOptions{})
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		ids = append(ids, job.ID)
	}

	cancel, stopped := runWorker(fastWorker(store, reg))
	defer func() { cancel(); <-stopped }()

	for range total {
		select {
		case <-seen:
		case <-time.After(5 * time.Second):
			t.Fatal("handler not invoked for all jobs within 5s")
		}
	}
	for _, id := range ids {
		if job := waitForState(t, store, id, jobs.StateDone); job.State != jobs.StateDone {
			t.Errorf("job %d state = %q, want done", id, job.State)
		}
	}
}

// TestWorker_retryThenDeadLetter verifies a permanently failing handler is
// retried up to max_attempts and then dead-lettered with its last error.
func TestWorker_retryThenDeadLetter(t *testing.T) {
	store, db := newStore(t)
	reg := worker.NewRegistry()
	wantErr := errors.New("handler always fails")
	reg.Register(jobType, func(_ context.Context, _ jobs.Job) error { return wantErr })

	const maxAttempts = 3
	job, err := store.Enqueue(t.Context(), jobType, payload(t, "retry"),
		jobs.EnqueueOptions{MaxAttempts: maxAttempts})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	stop := make(chan struct{})
	go keepRunnable(db, stop)
	defer close(stop)

	cancel, stopped := runWorker(fastWorker(store, reg))
	defer func() { cancel(); <-stopped }()

	dead := waitForState(t, store, job.ID, jobs.StateDead)
	if dead.Attempts != maxAttempts {
		t.Errorf("attempts = %d, want %d", dead.Attempts, maxAttempts)
	}
	if dead.LastError != wantErr.Error() {
		t.Errorf("last_error = %q, want %q", dead.LastError, wantErr.Error())
	}
}

// TestWorker_gracefulShutdown verifies Run returns promptly when its context is
// cancelled while a handler is in flight, abandoning the in-flight job (left
// running for the queue's stale-lock recovery) rather than blocking forever.
func TestWorker_gracefulShutdown(t *testing.T) {
	store, _ := newStore(t)
	reg := worker.NewRegistry()
	started := make(chan struct{}, 1)
	reg.Register(jobType, func(ctx context.Context, _ jobs.Job) error {
		started <- struct{}{}
		<-ctx.Done() // block until shutdown
		return ctx.Err()
	})

	job, err := store.Enqueue(t.Context(), jobType, payload(t, "slow"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	cancel, stopped := runWorker(fastWorker(store, reg))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		<-stopped
		t.Fatal("handler did not start within 5s")
	}

	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of cancellation")
	}

	// The abandoned job stays running; it was neither completed nor dead-lettered.
	got, err := store.Get(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != jobs.StateRunning {
		t.Errorf("abandoned job state = %q, want running (recoverable by the queue)", got.State)
	}
}
