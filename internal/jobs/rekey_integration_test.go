//go:build integration

package jobs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/jobs"
)

// countRekeyJobs counts the storage_rekey jobs of a project in the given states.
func countRekeyJobs(t *testing.T, db *database.DB, projectUID string, states ...string) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(t.Context(), `SELECT count(*) FROM jobs
		WHERE type = 'storage_rekey' AND payload ->> 'project_uid' = $1 AND state = ANY($2)`,
		projectUID, states).Scan(&n)
	if err != nil {
		t.Fatalf("counting storage_rekey jobs: %v", err)
	}
	return n
}

// TestEnqueueStorageRekey_onePerProject verifies the job dedupes per project
// over queued and running jobs alike: a second enqueue beside a queued or a
// running job is a no-op, the running job can still defer itself back to
// queued, and another project's job is never a duplicate.
func TestEnqueueStorageRekey_onePerProject(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()
	pool := db.Pool()

	for range 2 {
		if err := jobs.EnqueueStorageRekey(ctx, pool, "pj_a"); err != nil {
			t.Fatalf("EnqueueStorageRekey(pj_a): %v", err)
		}
	}
	if n := countRekeyJobs(t, db, "pj_a", "queued", "running"); n != 1 {
		t.Fatalf("pj_a has %d active jobs after two enqueues, want 1", n)
	}

	job, err := store.Claim(ctx, "w1", jobs.TypeStorageRekey)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if err := jobs.EnqueueStorageRekey(ctx, pool, "pj_a"); err != nil {
		t.Fatalf("EnqueueStorageRekey beside a running job: %v", err)
	}
	if n := countRekeyJobs(t, db, "pj_a", "queued", "running"); n != 1 {
		t.Fatalf("pj_a has %d active jobs beside a running one, want 1", n)
	}
	if _, err := store.Defer(ctx, job.ID, "w1", 0); err != nil {
		t.Fatalf("Defer of the running job back to queued: %v", err)
	}

	if err := jobs.EnqueueStorageRekey(ctx, pool, "pj_b"); err != nil {
		t.Fatalf("EnqueueStorageRekey(pj_b): %v", err)
	}
	if n := countRekeyJobs(t, db, "pj_b", "queued"); n != 1 {
		t.Fatalf("pj_b has %d queued jobs, want 1", n)
	}
	if err := jobs.EnqueueStorageRekey(ctx, pool, ""); !errors.Is(err, jobs.ErrProjectRequired) {
		t.Errorf("EnqueueStorageRekey(\"\") = %v, want ErrProjectRequired", err)
	}
}

// TestEnqueueStorageRekey_hurriesAWaitingJob verifies an enqueue that finds the
// project's job queued for later makes it runnable now — a resume does not wait
// out the paused job's minute.
func TestEnqueueStorageRekey_hurriesAWaitingJob(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()
	if err := jobs.EnqueueStorageRekey(ctx, db.Pool(), "pj_a"); err != nil {
		t.Fatalf("EnqueueStorageRekey: %v", err)
	}
	job, err := store.Claim(ctx, "w1", jobs.TypeStorageRekey)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := store.Defer(ctx, job.ID, "w1", time.Hour); err != nil {
		t.Fatalf("Defer: %v", err)
	}
	if _, err := store.Claim(ctx, "w1", jobs.TypeStorageRekey); !errors.Is(err, jobs.ErrNoJobs) {
		t.Fatalf("Claim of a job deferred an hour = %v, want ErrNoJobs", err)
	}
	if err := jobs.EnqueueStorageRekey(ctx, db.Pool(), "pj_a"); err != nil {
		t.Fatalf("EnqueueStorageRekey: %v", err)
	}
	again, err := store.Claim(ctx, "w1", jobs.TypeStorageRekey)
	if err != nil {
		t.Fatalf("Claim after the hurry: %v", err)
	}
	if again.ID != job.ID {
		t.Errorf("claimed job %d, want the same job %d", again.ID, job.ID)
	}
}

// TestEnqueueStorageRekey_joinsTheTransaction verifies a rolled-back
// transaction schedules nothing, and that a duplicate inside a transaction
// leaves it usable.
func TestEnqueueStorageRekey_joinsTheTransaction(t *testing.T) {
	_, db := newStore(t)
	ctx := t.Context()
	errRollback := errors.New("roll back")
	err := pgx.BeginFunc(ctx, db.Pool(), func(tx pgx.Tx) error {
		if err := jobs.EnqueueStorageRekey(ctx, tx, "pj_a"); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("transaction = %v, want the rollback", err)
	}
	if n := countRekeyJobs(t, db, "pj_a", "queued", "running"); n != 0 {
		t.Fatalf("a rolled-back enqueue left %d jobs", n)
	}

	if err := jobs.EnqueueStorageRekey(ctx, db.Pool(), "pj_a"); err != nil {
		t.Fatalf("EnqueueStorageRekey: %v", err)
	}
	err = pgx.BeginFunc(ctx, db.Pool(), func(tx pgx.Tx) error {
		if err := jobs.EnqueueStorageRekey(ctx, tx, "pj_a"); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "SELECT 1")
		return err
	})
	if err != nil {
		t.Fatalf("a duplicate inside a transaction broke it: %v", err)
	}
}
