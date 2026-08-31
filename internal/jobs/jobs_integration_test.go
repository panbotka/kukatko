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
	if err := store.Complete(ctx, j1.ID, "w1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("re-enqueue after complete: %v", err)
	}
}

// TestEnqueueDedup_sidecarScopedToQueued verifies migration 0044's scoped dedup:
// a sidecar enqueue while a sidecar job for the same photo is already running is
// not a duplicate (it schedules a follow-up rewrite), while a second *queued*
// sidecar for that photo still dedups, and every other job type keeps the
// queued|running dedup unchanged. It is the regression guard for the dropped
// sidecar rewrite: before the fix the enqueue below collided with the running job
// and the edit that triggered it never reached disk.
func TestEnqueueDedup_sidecarScopedToQueued(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	// A queued sidecar job for a photo still dedups a second queued enqueue: the
	// debounce that collapses an ordinary burst into one write is preserved.
	if _, err := store.Enqueue(ctx, jobs.TypeSidecar, photoPayload(t, "s1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("first sidecar enqueue: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeSidecar, photoPayload(t, "s1"), jobs.EnqueueOptions{}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("second queued sidecar enqueue = %v, want ErrDuplicate (queued-state debounce)", err)
	}

	// Claiming it moves it to running. A sidecar enqueue for the same photo now
	// succeeds — an edit arriving mid-run must schedule a follow-up, not be dropped.
	claimed, err := store.Claim(ctx, "w1", jobs.TypeSidecar)
	if err != nil {
		t.Fatalf("Claim sidecar: %v", err)
	}
	if claimed.State != jobs.StateRunning {
		t.Fatalf("claimed state = %q, want running", claimed.State)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeSidecar, photoPayload(t, "s1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("sidecar enqueue while running = %v, want success (a follow-up rewrite)", err)
	}
	// A second follow-up while the first is queued dedups again — bursts during the
	// running window still coalesce onto the one queued job.
	if _, err := store.Enqueue(ctx, jobs.TypeSidecar, photoPayload(t, "s1"), jobs.EnqueueOptions{}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("second follow-up enqueue = %v, want ErrDuplicate", err)
	}

	// Other job types are unchanged: a running job still blocks a same-photo
	// enqueue, so migration 0044 loosened nothing but sidecar.
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "s1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("image_embed enqueue: %v", err)
	}
	embedClaim, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed)
	if err != nil {
		t.Fatalf("Claim image_embed: %v", err)
	}
	if embedClaim.State != jobs.StateRunning {
		t.Fatalf("image_embed claimed state = %q, want running", embedClaim.State)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "s1"), jobs.EnqueueOptions{}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("image_embed enqueue while running = %v, want ErrDuplicate (semantics unchanged)", err)
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

// TestDefer verifies Defer requeues a running job to run after the delay without
// counting a failed attempt, and that Defer on a non-running job is rejected.
func TestDefer(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	job, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "defer"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	deferred, err := store.Defer(ctx, claimed.ID, "w1", time.Minute)
	if err != nil {
		t.Fatalf("Defer: %v", err)
	}
	if deferred.State != jobs.StateQueued {
		t.Errorf("state = %q, want queued", deferred.State)
	}
	if deferred.Attempts != 0 {
		t.Errorf("attempts = %d, want 0 (no attempt burned)", deferred.Attempts)
	}
	if !deferred.RunAfter.After(time.Now()) {
		t.Errorf("run_after = %v, want a future time", deferred.RunAfter)
	}
	if deferred.LockedBy != nil {
		t.Errorf("locked_by = %v, want nil after defer", deferred.LockedBy)
	}

	// The job is no longer runnable until its delay elapses.
	if _, err := store.Claim(ctx, "w2"); !errors.Is(err, jobs.ErrNoJobs) {
		t.Errorf("claim deferred job = %v, want ErrNoJobs", err)
	}

	// Defer on a job that is not running under this worker matches nothing.
	if _, err := store.Defer(ctx, job.ID, "w1", time.Minute); !errors.Is(err, jobs.ErrLockLost) {
		t.Errorf("Defer non-running = %v, want ErrLockLost", err)
	}
	if _, err := store.Defer(ctx, 999999, "w1", time.Minute); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Errorf("Defer missing job = %v, want ErrJobNotFound", err)
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
		failed, err := store.Fail(ctx, claimed.ID, "w1", errors.New("boom"))
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

// markDead forces a job into the dead letter without waiting out its retry
// budget, which is what the bulk requeue operates on.
func markDead(t *testing.T, db *database.DB, id int64) {
	t.Helper()
	_, err := db.Pool().Exec(t.Context(),
		"UPDATE jobs SET state = 'dead', attempts = 5, last_error = 'boom' WHERE id = $1", id)
	if err != nil {
		t.Fatalf("dead-lettering job %d: %v", id, err)
	}
}

// TestRequeueAllDead verifies the bulk requeue: with no types it empties the
// whole dead letter in one statement, with a type it retries only that kind of
// work, and either way it leaves the jobs that were never dead alone.
func TestRequeueAllDead(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	embed, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue embed: %v", err)
	}
	ocr, err := store.Enqueue(ctx, jobs.TypeOCR, photoPayload(t, "p2"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue ocr: %v", err)
	}
	live, err := store.Enqueue(ctx, jobs.TypeThumbnail, photoPayload(t, "p3"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue thumbnail: %v", err)
	}
	markDead(t, db, embed.ID)
	markDead(t, db, ocr.ID)

	// One type only: the OCR job comes back, the embedding one stays dead.
	requeued, err := store.RequeueAllDead(ctx, jobs.TypeOCR)
	if err != nil {
		t.Fatalf("RequeueAllDead(ocr): %v", err)
	}
	if requeued != 1 {
		t.Errorf("RequeueAllDead(ocr) = %d, want 1", requeued)
	}
	refreshed, err := store.Get(ctx, ocr.ID)
	if err != nil {
		t.Fatalf("Get(ocr): %v", err)
	}
	if refreshed.State != jobs.StateQueued || refreshed.Attempts != 0 || refreshed.LastError != "" {
		t.Errorf("requeued ocr job = %+v, want queued with a fresh attempt budget", refreshed)
	}

	// No types: the rest of the dead letter follows, and only it.
	requeued, err = store.RequeueAllDead(ctx)
	if err != nil {
		t.Fatalf("RequeueAllDead(): %v", err)
	}
	if requeued != 1 {
		t.Errorf("RequeueAllDead() = %d, want the 1 remaining dead job", requeued)
	}
	counts, err := store.CountsByState(ctx)
	if err != nil {
		t.Fatalf("CountsByState: %v", err)
	}
	if counts[jobs.StateDead] != 0 || counts[jobs.StateQueued] != 3 {
		t.Errorf("counts = %+v, want an empty dead letter and 3 queued", counts)
	}
	// The job that was never dead was not touched by either call.
	untouched, err := store.Get(ctx, live.ID)
	if err != nil {
		t.Fatalf("Get(thumbnail): %v", err)
	}
	if !untouched.UpdatedAt.Equal(live.UpdatedAt) {
		t.Errorf("thumbnail job updated_at moved from %v to %v, want it left alone",
			live.UpdatedAt, untouched.UpdatedAt)
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

// TestStaleLockRecovery verifies a running job with a stale lock is requeued
// (after a backoff delay) and then re-claimable, while a heartbeated job is left
// alone.
func TestStaleLockRecovery(t *testing.T) {
	store, db := newStore(t)
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

	// Recovery applies the same backoff as Fail, so a job whose worker keeps
	// dying on it cannot be re-claimed instantly in a tight crash loop.
	requeued, err := store.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if requeued.State != jobs.StateQueued {
		t.Errorf("state = %q, want queued", requeued.State)
	}
	if requeued.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", requeued.Attempts)
	}
	if !requeued.RunAfter.After(time.Now()) {
		t.Errorf("run_after = %v, want a future backoff", requeued.RunAfter)
	}
	if _, err := store.Claim(ctx, "w2"); !errors.Is(err, jobs.ErrNoJobs) {
		t.Errorf("claim during recovery backoff = %v, want ErrNoJobs", err)
	}

	makeRunnable(t, db, claimed.ID)
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

// TestStaleRecoveryDeadLettersWithoutBackoff verifies a recovered job that has
// exhausted its attempts is dead-lettered rather than pushed into a backoff it
// would never be claimed out of.
func TestStaleRecoveryDeadLettersWithoutBackoff(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "lastchance"),
		jobs.EnqueueOptions{MaxAttempts: 1}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := store.RecoverStaleLocks(ctx, 0); err != nil {
		t.Fatalf("RecoverStaleLocks: %v", err)
	}

	dead, err := store.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get recovered job: %v", err)
	}
	if dead.State != jobs.StateDead {
		t.Errorf("state = %q, want dead", dead.State)
	}
	if dead.LastError == "" {
		t.Error("last_error is empty, want the stale-lock reason")
	}
}

// TestOwnershipGuard verifies the lifecycle writes are fenced by the worker id:
// once stale-lock recovery has handed a job to another worker, the previous
// owner's late Complete/Fail/Defer is rejected instead of clobbering the new
// owner's run.
func TestOwnershipGuard(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "fenced"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Worker w1 stalls, recovery requeues the job and w2 picks it up.
	if _, err := store.RecoverStaleLocks(ctx, 0); err != nil {
		t.Fatalf("RecoverStaleLocks: %v", err)
	}
	makeRunnable(t, db, claimed.ID)
	if _, err := store.Claim(ctx, "w2"); err != nil {
		t.Fatalf("re-claim: %v", err)
	}

	// Every late write from w1 must bounce.
	if err := store.Complete(ctx, claimed.ID, "w1"); !errors.Is(err, jobs.ErrLockLost) {
		t.Errorf("Complete by previous owner = %v, want ErrLockLost", err)
	}
	if _, err := store.Fail(ctx, claimed.ID, "w1", errors.New("late")); !errors.Is(err, jobs.ErrLockLost) {
		t.Errorf("Fail by previous owner = %v, want ErrLockLost", err)
	}
	if _, err := store.Defer(ctx, claimed.ID, "w1", time.Minute); !errors.Is(err, jobs.ErrLockLost) {
		t.Errorf("Defer by previous owner = %v, want ErrLockLost", err)
	}
	if err := store.Heartbeat(ctx, claimed.ID, "w1"); !errors.Is(err, jobs.ErrLockLost) {
		t.Errorf("Heartbeat by previous owner = %v, want ErrLockLost", err)
	}
	if err := store.Heartbeat(ctx, 999999, "w2"); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Errorf("Heartbeat on missing job = %v, want ErrJobNotFound", err)
	}

	// The job is still running under its new owner, which can finish it.
	current, err := store.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if current.State != jobs.StateRunning || current.LockedBy == nil || *current.LockedBy != "w2" {
		t.Fatalf("job state = %q locked_by = %v, want running under w2", current.State, current.LockedBy)
	}
	if err := store.Complete(ctx, claimed.ID, "w2"); err != nil {
		t.Errorf("Complete by current owner: %v", err)
	}
	if err := store.Complete(ctx, 999999, "w2"); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Errorf("Complete missing job = %v, want ErrJobNotFound", err)
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

// TestCountsByTypeState verifies the two-dimensional breakdown behind the
// /metrics queue gauges: it splits one type across the states its jobs are
// actually in, and its cells sum to the one-dimensional tallies.
func TestCountsByTypeState(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	for _, uid := range []string{"a", "b"} {
		if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, uid), jobs.EnqueueOptions{}); err != nil {
			t.Fatalf("enqueue image_embed %s: %v", uid, err)
		}
	}
	if _, err := store.Enqueue(ctx, jobs.TypeThumbnail, photoPayload(t, "a"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue thumbnail: %v", err)
	}
	if _, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed); err != nil {
		t.Fatalf("claim: %v", err)
	}

	counts, err := store.CountsByTypeState(ctx)
	if err != nil {
		t.Fatalf("CountsByTypeState: %v", err)
	}
	want := map[jobs.TypeState]int{
		{Type: jobs.TypeImageEmbed, State: jobs.StateRunning}: 1,
		{Type: jobs.TypeImageEmbed, State: jobs.StateQueued}:  1,
		{Type: jobs.TypeThumbnail, State: jobs.StateQueued}:   1,
	}
	if len(counts) != len(want) {
		t.Fatalf("CountsByTypeState() = %+v, want %+v", counts, want)
	}
	for cell, n := range want {
		if counts[cell] != n {
			t.Errorf("count for %+v = %d, want %d (full: %+v)", cell, counts[cell], n, counts)
		}
	}
}

// TestCountPending verifies CountPending counts only queued/running jobs of the
// requested types, excludes other types and terminal (done) jobs, and returns 0
// with no types — the query backing the optional Wake-on-LAN auto-wake.
func TestCountPending(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	for _, uid := range []string{"a", "b"} {
		if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, uid), jobs.EnqueueOptions{}); err != nil {
			t.Fatalf("enqueue image_embed %s: %v", uid, err)
		}
	}
	if _, err := store.Enqueue(ctx, jobs.TypeFaceDetect, photoPayload(t, "a"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue face_detect: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeThumbnail, photoPayload(t, "a"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue thumbnail: %v", err)
	}

	pending, err := store.CountPending(ctx, jobs.TypeImageEmbed, jobs.TypeFaceDetect)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if pending != 3 {
		t.Errorf("pending embedding jobs = %d, want 3", pending)
	}

	if n, err := store.CountPending(ctx); err != nil || n != 0 {
		t.Errorf("CountPending() = %d, %v, want 0, nil", n, err)
	}

	// Completing a claimed image_embed job moves it out of the pending set.
	claimed, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := store.Complete(ctx, claimed.ID, "w1"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	pending, err = store.CountPending(ctx, jobs.TypeImageEmbed, jobs.TypeFaceDetect)
	if err != nil {
		t.Fatalf("CountPending after complete: %v", err)
	}
	if pending != 2 {
		t.Errorf("pending after completing one = %d, want 2", pending)
	}
}

// TestFailTerminal_parksTheJobWithoutARetry verifies a permanent failure ends the
// job then and there: it is stamped 'failed' with its cause, no worker can claim
// it again however much of its attempt budget is left, and an operator can still
// requeue it by hand once the cause is fixed.
func TestFailTerminal_parksTheJobWithoutARetry(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	enqueued, err := store.Enqueue(ctx, jobs.TypeMailSend,
		json.RawMessage(`{"template":"account_approved","to":"gone@example.com"}`), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	failed, err := store.FailTerminal(ctx, claimed.ID, "w1", errors.New("550 no such user"))
	if err != nil {
		t.Fatalf("FailTerminal: %v", err)
	}
	if failed.State != jobs.StateFailed {
		t.Errorf("state = %q, want %q", failed.State, jobs.StateFailed)
	}
	if failed.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", failed.Attempts)
	}
	if failed.LockedBy != nil || failed.LockedAt != nil {
		t.Errorf("lock not released: locked_by=%v locked_at=%v", failed.LockedBy, failed.LockedAt)
	}
	if failed.LastError != "550 no such user" {
		t.Errorf("last_error = %q, want the cause", failed.LastError)
	}
	if failed.Attempts >= failed.MaxAttempts {
		t.Fatalf("attempts %d/%d: the test must leave retries unused to prove they are not taken",
			failed.Attempts, failed.MaxAttempts)
	}

	if _, err := store.Claim(ctx, "w2"); !errors.Is(err, jobs.ErrNoJobs) {
		t.Errorf("a terminally failed job was claimable again: %v", err)
	}

	requeued, err := store.Requeue(ctx, enqueued.ID)
	if err != nil {
		t.Fatalf("Requeue: %v", err)
	}
	if requeued.State != jobs.StateQueued {
		t.Errorf("requeued state = %q, want %q", requeued.State, jobs.StateQueued)
	}
}

// TestFailTerminal_dropsALateResult verifies the ownership guard: a worker whose
// job was meanwhile recovered and reclaimed cannot park the new owner's run.
func TestFailTerminal_dropsALateResult(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeMailSend,
		json.RawMessage(`{"template":"account_approved"}`), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := store.FailTerminal(ctx, claimed.ID, "somebody-else", errors.New("550")); !errors.Is(err, jobs.ErrLockLost) {
		t.Fatalf("FailTerminal under a foreign worker id = %v, want ErrLockLost", err)
	}
}

// TestEnqueue_joinsTheCallersTransaction verifies the package-level Enqueue takes
// its executor seriously: a job written on a transaction that rolls back never
// exists, and one written on a transaction that commits does. It is what lets a
// mutation schedule its own mail without a mail going out for a change that was
// undone.
func TestEnqueue_joinsTheCallersTransaction(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()
	payload := json.RawMessage(`{"template":"registration_received","to":"jan@example.com"}`)

	rolledBack, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := jobs.Enqueue(ctx, rolledBack, jobs.TypeMailSend, payload, jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue in transaction: %v", err)
	}
	if err := rolledBack.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	pending, err := store.CountPending(ctx, jobs.TypeMailSend)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if pending != 0 {
		t.Errorf("pending after rollback = %d, want 0", pending)
	}

	committed, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := jobs.Enqueue(ctx, committed, jobs.TypeMailSend, payload, jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("enqueue in transaction: %v", err)
	}
	if err := committed.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if pending, err = store.CountPending(ctx, jobs.TypeMailSend); err != nil || pending != 1 {
		t.Errorf("pending after commit = %d (err %v), want 1", pending, err)
	}
}

// TestEnqueue_mailNeverDedupes verifies two mails of the same type coexist in the
// queue: the dedup index keys on payload->>'photo_uid', which a mail payload does
// not have, so a second registration is not swallowed as a duplicate of the first.
func TestEnqueue_mailNeverDedupes(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	for _, to := range []string{"jan@example.com", "eva@example.com"} {
		payload := json.RawMessage(`{"template":"registration_received","to":"` + to + `"}`)
		if _, err := store.Enqueue(ctx, jobs.TypeMailSend, payload, jobs.EnqueueOptions{}); err != nil {
			t.Fatalf("enqueue for %s: %v", to, err)
		}
	}
	pending, err := store.CountPending(ctx, jobs.TypeMailSend)
	if err != nil {
		t.Fatalf("CountPending: %v", err)
	}
	if pending != 2 {
		t.Errorf("pending mail jobs = %d, want 2", pending)
	}
}

// forcedPayload builds a {"photo_uid": uid, "force": true} payload — what a
// rebuild enqueue carries, and what a handler branches on to redo work it would
// otherwise skip.
func forcedPayload(t *testing.T, uid string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"photo_uid": uid, "force": true})
	if err != nil {
		t.Fatalf("marshaling forced payload: %v", err)
	}
	return raw
}

// isForced reports whether a job's payload carries the force flag.
func isForced(t *testing.T, payload json.RawMessage) bool {
	t.Helper()
	var decoded struct {
		Force bool `json:"force"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decoding the job payload %q: %v", payload, err)
	}
	return decoded.Force
}

// countActive returns how many queued or running jobs exist for one type and
// photo — the invariant the upgrade must not break, since dedup stays keyed on
// type + photo_uid.
func countActive(t *testing.T, db *database.DB, jobType, photoUID string) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM jobs WHERE type = $1 AND payload ->> 'photo_uid' = $2 "+
			"AND state IN ('queued', 'running')", jobType, photoUID).Scan(&n)
	if err != nil {
		t.Fatalf("counting active jobs: %v", err)
	}
	return n
}

// TestUpgradeToForced_rewritesTheQueuedPlainJob is the bug the upgrade exists to
// fix: a forced enqueue that collides with a queued *plain* job used to be
// dropped, leaving the plain job to take its idempotent skip and the operator
// looking at a success that recomputed nothing. The payload is rewritten instead,
// so the job that runs is the forced one — and there is still exactly one job.
func TestUpgradeToForced_rewritesTheQueuedPlainJob(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("plain enqueue: %v", err)
	}

	outcome, err := store.UpgradeToForced(ctx, jobs.TypeImageEmbed, "p1", forcedPayload(t, "p1"))
	if err != nil {
		t.Fatalf("UpgradeToForced: %v", err)
	}
	if outcome != jobs.ForceUpgraded {
		t.Errorf("outcome = %q, want %q", outcome, jobs.ForceUpgraded)
	}
	if got := countActive(t, db, jobs.TypeImageEmbed, "p1"); got != 1 {
		t.Errorf("active image_embed jobs for p1 = %d, want 1 — the upgrade rewrites, it does not insert", got)
	}

	// The claimed payload is what the handler reads, and a forced one is what makes
	// it recompute instead of skipping (see embedjob.TestHandle_forcePayloadRebuilds).
	claimed, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !isForced(t, claimed.Payload) {
		t.Errorf("the claimed payload is %s, want the forced one — the run would skip the photo", claimed.Payload)
	}
}

// TestUpgradeToForced_absorbsAnAlreadyForcedJob keeps the collapse that is
// correct: two rebuild requests for the same photo are one job, and the second
// changes nothing.
func TestUpgradeToForced_absorbsAnAlreadyForcedJob(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeFaceDetect, forcedPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("forced enqueue: %v", err)
	}

	outcome, err := store.UpgradeToForced(ctx, jobs.TypeFaceDetect, "p1", forcedPayload(t, "p1"))
	if err != nil {
		t.Fatalf("UpgradeToForced: %v", err)
	}
	if outcome != jobs.ForceAbsorbed {
		t.Errorf("outcome = %q, want %q", outcome, jobs.ForceAbsorbed)
	}
	if got := countActive(t, db, jobs.TypeFaceDetect, "p1"); got != 1 {
		t.Errorf("active face_detect jobs for p1 = %d, want 1", got)
	}
}

// TestUpgradeToForced_refusesARunningJob is the collision the queue cannot
// resolve: the worker read the payload when it claimed the job, so rewriting it
// now would either be lost or — worse — read as applying to the run in flight.
// The statement is conditional on the job still being queued, so it leaves the
// running job alone and says so.
func TestUpgradeToForced_refusesARunningJob(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypePlaces, photoPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("plain enqueue: %v", err)
	}
	claimed, err := store.Claim(ctx, "w1", jobs.TypePlaces)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	outcome, err := store.UpgradeToForced(ctx, jobs.TypePlaces, "p1", forcedPayload(t, "p1"))
	if err != nil {
		t.Fatalf("UpgradeToForced: %v", err)
	}
	if outcome != jobs.ForceInFlight {
		t.Errorf("outcome = %q, want %q", outcome, jobs.ForceInFlight)
	}
	if got := countActive(t, db, jobs.TypePlaces, "p1"); got != 1 {
		t.Errorf("active places jobs for p1 = %d, want 1", got)
	}
	var payload json.RawMessage
	if err := db.Pool().QueryRow(ctx, "SELECT payload FROM jobs WHERE id = $1", claimed.ID).Scan(&payload); err != nil {
		t.Fatalf("re-reading the running job: %v", err)
	}
	if isForced(t, payload) {
		t.Error("the running job's payload was forced; the flag would apply to a run that already read it")
	}
}

// TestUpgradeToForced_withoutAnActiveJob reports the window between the two
// statements of a forced enqueue: the insert lost to the dedup index, but the job
// it collided with finished before the upgrade looked for it. There is nothing to
// rewrite, and the caller retries the insert rather than reporting a collision.
func TestUpgradeToForced_withoutAnActiveJob(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	tests := []struct {
		name  string
		setup func()
	}{
		{name: "empty queue", setup: func() {}},
		{name: "the job has finished", setup: func() {
			job, err := store.Enqueue(ctx, jobs.TypeOCR, photoPayload(t, "p1"), jobs.EnqueueOptions{})
			if err != nil {
				t.Fatalf("plain enqueue: %v", err)
			}
			claimed, err := store.Claim(ctx, "w1", jobs.TypeOCR)
			if err != nil {
				t.Fatalf("Claim: %v", err)
			}
			if err := store.Complete(ctx, claimed.ID, "w1"); err != nil {
				t.Fatalf("Complete job %d: %v", job.ID, err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			_, err := store.UpgradeToForced(ctx, jobs.TypeOCR, "p1", forcedPayload(t, "p1"))
			if !errors.Is(err, jobs.ErrNoActiveJob) {
				t.Errorf("UpgradeToForced = %v, want ErrNoActiveJob", err)
			}
		})
	}
}

// TestEnqueue_plainNeverDowngradesAForcedJob is the other direction of the same
// rule: a forced job outranks a plain one both ways round. The ordinary repair
// enqueue is unchanged — it collides with the forced job and is absorbed — and it
// must never rewrite the payload back to the plain one, or the rebuild an operator
// asked for would quietly become a skip again.
func TestEnqueue_plainNeverDowngradesAForcedJob(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, forcedPayload(t, "p1"), jobs.EnqueueOptions{}); err != nil {
		t.Fatalf("forced enqueue: %v", err)
	}
	if _, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("plain enqueue over a forced job = %v, want ErrDuplicate", err)
	}
	if got := countActive(t, db, jobs.TypeImageEmbed, "p1"); got != 1 {
		t.Fatalf("active image_embed jobs for p1 = %d, want 1", got)
	}
	claimed, err := store.Claim(ctx, "w1", jobs.TypeImageEmbed)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if !isForced(t, claimed.Payload) {
		t.Errorf("the claimed payload is %s, want the forced one kept", claimed.Payload)
	}
}

// TestUpgradeToForced_losesToAConcurrentClaim is why the upgrade is one
// statement rather than a read followed by a write: a worker can claim the job
// between the two, and a rewrite that had already decided the job was queued
// would then stamp the forced flag onto a run that read its payload before the
// flag existed. The run would ignore it and the flag would be lost with the job.
//
// The race is staged deterministically — the claim is held open in an
// uncommitted transaction, so the upgrade reaches the row while it still reads as
// queued and blocks on the lock — and the answer must be the in-flight one, with
// the running job's payload untouched.
func TestUpgradeToForced_losesToAConcurrentClaim(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	job, err := store.Enqueue(ctx, jobs.TypeImageEmbed, photoPayload(t, "p1"), jobs.EnqueueOptions{})
	if err != nil {
		t.Fatalf("plain enqueue: %v", err)
	}
	forced := forcedPayload(t, "p1")

	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("beginning the claim transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		"UPDATE jobs SET state = 'running', locked_by = 'w1', locked_at = now() WHERE id = $1", job.ID,
	); err != nil {
		t.Fatalf("claiming the job: %v", err)
	}

	type result struct {
		outcome jobs.ForceOutcome
		err     error
	}
	done := make(chan result, 1)
	go func() {
		outcome, upgradeErr := store.UpgradeToForced(ctx, jobs.TypeImageEmbed, "p1", forced)
		done <- result{outcome: outcome, err: upgradeErr}
	}()

	select {
	case res := <-done:
		t.Fatalf("the upgrade answered %q (%v) while the claim was still open; it never took the row lock",
			res.outcome, res.err)
	case <-time.After(500 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing the claim: %v", err)
	}

	res := <-done
	if res.err != nil {
		t.Fatalf("UpgradeToForced: %v", res.err)
	}
	if res.outcome != jobs.ForceInFlight {
		t.Errorf("outcome = %q, want %q — the job was claimed out from under the upgrade",
			res.outcome, jobs.ForceInFlight)
	}
	var payload json.RawMessage
	if err := db.Pool().QueryRow(ctx, "SELECT payload FROM jobs WHERE id = $1", job.ID).Scan(&payload); err != nil {
		t.Fatalf("re-reading the claimed job: %v", err)
	}
	if isForced(t, payload) {
		t.Error("the forced flag landed on a job that was already running with the plain payload")
	}
}
