//go:build integration

package jobs_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStore returns a jobs.Store over a freshly truncated integration database.
func newStore(t *testing.T) (*jobs.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return jobs.NewStore(db.Pool()), db
}

// photoPayload builds a {"photo_uid": uid} JSON payload for enqueue calls.
func photoPayload(t *testing.T, uid string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"photo_uid": uid})
	if err != nil {
		t.Fatalf("marshaling payload: %v", err)
	}
	return raw
}

// makeRunnable forces a job's run_after into the past so it can be re-claimed
// immediately, side-stepping the backoff delay a real worker would wait out.
func makeRunnable(t *testing.T, db *database.DB, id int64) {
	t.Helper()
	_, err := db.Pool().Exec(t.Context(),
		"UPDATE jobs SET run_after = now() - interval '1 hour' WHERE id = $1", id)
	if err != nil {
		t.Fatalf("forcing run_after: %v", err)
	}
}

// TestEnqueueDedup verifies the partial-unique dedup: at most one active job per
// (type, photo_uid), while a different type or a finished job may be enqueued.
func TestEnqueueDedup(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	j1, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("duplicate enqueue error = %v, want ErrDuplicate", err)
	}
	// A different type for the same photo is allowed.
	if _, err := store.Enqueue(ctx, jobs.TypeFaceDetect, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("face_detect enqueue: %v", err)
	}
	// A different photo for the same type is allowed.
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p2"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("p2 enqueue: %v", err)
	}

	counts, err := store.CountsByState(ctx)
	if err != nil {
		t.Fatalf("CountsByState: %v", err)
	}
	if counts[jobs.StateQueued] != 3 {
		t.Errorf("queued count = %d, want 3", counts[jobs.StateQueued])
	}

	// Finishing the first job frees its dedup slot, so it can be re-enqueued.
	claimed, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.ID != j1.ID {
		t.Errorf("claimed id = %d, want %d (FIFO)", claimed.ID, j1.ID)
	}
	if err := store.Complete(ctx, j1.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("re-enqueue after complete: %v", err)
	}
}

// TestClaimOrdering verifies claiming respects run_after (skips not-yet-due),
// then priority DESC, then FIFO by id.
func TestClaimOrdering(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Hour)

	low, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "low"),
		jobs.EnqueueOptions{Priority: 0, RunAfter: &past})
	if err != nil {
		t.Fatalf("enqueue low: %v", err)
	}
	high, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "high"),
		jobs.EnqueueOptions{Priority: 10, RunAfter: &past})
	if err != nil {
		t.Fatalf("enqueue high: %v", err)
	}
	mid, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "mid"),
		jobs.EnqueueOptions{Priority: 5, RunAfter: &past})
	if err != nil {
		t.Fatalf("enqueue mid: %v", err)
	}
	// Not yet due: must never be claimed in this test.
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "future"),
		jobs.EnqueueOptions{Priority: 100, RunAfter: &future}); err != nil {
		t.Fatalf("enqueue future: %v", err)
	}

	wantOrder := []int64{high.ID, mid.ID, low.ID}
	for i, want := range wantOrder {
		got, err := store.Claim(ctx, "w1")
		if err != nil {
			t.Fatalf("claim %d: %v", i, err)
		}
		if got.ID != want {
			t.Errorf("claim %d id = %d, want %d", i, got.ID, want)
		}
		if got.State != jobs.StateRunning || got.LockedBy == nil || *got.LockedBy != "w1" {
			t.Errorf("claim %d not marked running/locked: %+v", i, got)
		}
	}
	if _, err := store.Claim(ctx, "w1"); !errors.Is(err, jobs.ErrNoJobs) {
		t.Errorf("claim after draining due jobs = %v, want ErrNoJobs", err)
	}
}

// TestClaimSkipLocked verifies two concurrent claimers never receive the same
// job and together drain the queue exactly once each.
func TestClaimSkipLocked(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	const total = 30
	for i := range total {
		if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed,
			photoPayload(t, "c"+strconv.Itoa(i)), jobs.EnqueueOptions{}); err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
	}

	var mu sync.Mutex
	seen := make(map[int64]int)
	var wg sync.WaitGroup
	for _, worker := range []string{"wa", "wb"} {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for {
				job, err := store.Claim(ctx, workerID)
				if errors.Is(err, jobs.ErrNoJobs) {
					return
				}
				if err != nil {
					t.Errorf("%s claim: %v", workerID, err)
					return
				}
				mu.Lock()
				seen[job.ID]++
				mu.Unlock()
			}
		}(worker)
	}
	wg.Wait()

	if len(seen) != total {
		t.Errorf("claimed %d distinct jobs, want %d", len(seen), total)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("job %d claimed %d times, want exactly 1", id, n)
		}
	}
}

// TestRetryBackoffDeadLetter verifies failed jobs increment attempts and requeue
// with backoff until max_attempts, then dead-letter; and that a dead job can be
// listed and requeued.
func TestRetryBackoffDeadLetter(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	const maxAttempts = 3
	job, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "retry"),
		jobs.EnqueueOptions{MaxAttempts: maxAttempts})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		claimed, err := store.Claim(ctx, "w1")
		if err != nil {
			t.Fatalf("claim attempt %d: %v", attempt, err)
		}
		failed, err := store.Fail(ctx, claimed.ID, errors.New("boom"))
		if err != nil {
			t.Fatalf("fail attempt %d: %v", attempt, err)
		}
		if failed.Attempts != attempt {
			t.Errorf("attempt %d: attempts = %d, want %d", attempt, failed.Attempts, attempt)
		}
		if attempt < maxAttempts {
			if failed.State != jobs.StateQueued {
				t.Errorf("attempt %d: state = %q, want queued", attempt, failed.State)
			}
			if !failed.RunAfter.After(time.Now()) {
				t.Errorf("attempt %d: run_after = %v, want a future backoff", attempt, failed.RunAfter)
			}
			makeRunnable(t, db, failed.ID)
		} else if failed.State != jobs.StateDead {
			t.Errorf("final attempt: state = %q, want dead", failed.State)
		}
	}

	if _, err := store.Claim(ctx, "w1"); !errors.Is(err, jobs.ErrNoJobs) {
		t.Errorf("claim after dead-letter = %v, want ErrNoJobs", err)
	}

	dead, err := store.ListDead(ctx, 0, 0)
	if err != nil {
		t.Fatalf("ListDead: %v", err)
	}
	if len(dead) != 1 || dead[0].ID != job.ID || dead[0].LastError != "boom" {
		t.Errorf("ListDead = %+v, want one dead job %d with last_error boom", dead, job.ID)
	}

	requeued, err := store.RequeueDead(ctx, job.ID)
	if err != nil {
		t.Fatalf("RequeueDead: %v", err)
	}
	if requeued.State != jobs.StateQueued || requeued.Attempts != 0 || requeued.LastError != "" {
		t.Errorf("RequeueDead result = %+v, want queued/attempts 0/no error", requeued)
	}
	if _, err := store.Claim(ctx, "w1"); err != nil {
		t.Errorf("claim after requeue: %v", err)
	}
}

// TestRequeueDeadErrors verifies the sentinels for a missing job and a
// non-dead job.
func TestRequeueDeadErrors(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.RequeueDead(ctx, 999999); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Errorf("RequeueDead(missing) = %v, want ErrJobNotFound", err)
	}
	live, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "live"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := store.RequeueDead(ctx, live.ID); !errors.Is(err, jobs.ErrNotDead) {
		t.Errorf("RequeueDead(queued) = %v, want ErrNotDead", err)
	}
}

// TestStaleLockRecovery verifies a running job with a stale lock is requeued and
// re-claimable, while a heartbeated job is left alone.
func TestStaleLockRecovery(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "stale"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// With a zero stale threshold the just-claimed lock is already stale.
	recovered, err := store.RecoverStaleLocks(ctx, 0)
	if err != nil {
		t.Fatalf("RecoverStaleLocks: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	reclaimed, err := store.Claim(ctx, "w2")
	if err != nil {
		t.Fatalf("re-claim after recovery: %v", err)
	}
	if reclaimed.ID != claimed.ID {
		t.Errorf("re-claimed id = %d, want %d", reclaimed.ID, claimed.ID)
	}

	// A heartbeat keeps a long-running job out of recovery.
	if err := store.Heartbeat(ctx, reclaimed.ID, "w2"); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	stillRecovered, err := store.RecoverStaleLocks(ctx, time.Hour)
	if err != nil {
		t.Fatalf("RecoverStaleLocks(1h): %v", err)
	}
	if stillRecovered != 0 {
		t.Errorf("recovered with fresh heartbeat = %d, want 0", stillRecovered)
	}
}

// TestCounts verifies the per-state and per-type aggregate helpers.
func TestCounts(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "a"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue a: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "b"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue b: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeFaceDetect, photoPayload(t, "a"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue face: %v", err)
	}

	byState, err := store.CountsByState(ctx)
	if err != nil {
		t.Fatalf("CountsByState: %v", err)
	}
	if byState[jobs.StateQueued] != 3 {
		t.Errorf("queued = %d, want 3", byState[jobs.StateQueued])
	}

	byType, err := store.CountsByType(ctx)
	if err != nil {
		t.Fatalf("CountsByType: %v", err)
	}
	if byType[jobs.TypeImageEmbed] != 2 || byType[jobs.TypeFaceDetect] != 1 {
		t.Errorf("byType = %+v, want image_embed 2 face_detect 1", byType)
	}
}
