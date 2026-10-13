//go:build integration

package phototask_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/taskdigestjob"
)

// TestDigests_onePerProject makes A's member a member of B as well and leaves
// one task waiting on them in each project. The digest is two digests — one per
// project, each listing only that project's task under that project's name —
// and each membership keeps its own stamp: digesting A leaves B's due.
func TestDigests_onePerProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	store := phototask.NewStore(pool)
	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}
	person := f.A.UserUID
	exec(`INSERT INTO project_members (project_uid, user_uid, role, approved_at) VALUES ($1, $2, 'viewer', now())`,
		f.B.ProjectUID, person)
	// In A, somebody else asks the person; in B, the person is put on B's task,
	// which B's member opened.
	exec(`INSERT INTO photo_tasks (uid, project_id, title, created_by) VALUES ('tkaskedina', $1, 'Otázka v A', $2)`,
		f.A.ProjectUID, f.B.UserUID)
	exec(`INSERT INTO photo_task_participants (task_uid, user_uid) VALUES ('tkaskedina', $1), ($2, $1)`,
		person, f.B.TaskUID)

	digests, err := store.Digests(ctx)
	if err != nil {
		t.Fatalf("Digests: %v", err)
	}
	byProject := map[string]phototask.Digest{}
	for _, d := range digests {
		if d.UserUID != person {
			t.Errorf("unexpected digest for %s in %s", d.UserUID, d.ProjectSlug)
			continue
		}
		byProject[d.ProjectUID] = d
	}
	a, b := byProject[f.A.ProjectUID], byProject[f.B.ProjectUID]
	if len(byProject) != 2 || a.Total != 1 || b.Total != 1 {
		t.Fatalf("digests = %+v, want one per project with one task each", digests)
	}
	if a.Tasks[0].UID != "tkaskedina" || b.Tasks[0].UID != f.B.TaskUID {
		t.Errorf("tasks = A %s / B %s, want each project's own", a.Tasks[0].UID, b.Tasks[0].UID)
	}
	if b.ProjectSlug != f.B.Slug || b.ProjectName == "" {
		t.Errorf("B's digest names %q/%q, want slug %q and its name", b.ProjectSlug, b.ProjectName, f.B.Slug)
	}
	targets, err := store.DigestTargets(ctx)
	if err != nil || len(targets) != 2 {
		t.Fatalf("DigestTargets = %v, %v; want the two pairs", targets, err)
	}
	// The daily tick turns the two pairs into two queued jobs, each naming its
	// project and person.
	scheduler := taskdigestjob.NewScheduler(taskdigestjob.SchedulerConfig{
		Enabled: true, Enqueuer: jobs.NewEnqueuer(jobs.NewStore(pool)), Targets: store, Hour: 7,
	})
	if n, err := scheduler.Tick(ctx); err != nil || n != 2 {
		t.Fatalf("Tick = %d, %v; want two jobs", n, err)
	}
	var queued int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT payload ->> 'project_uid') FROM jobs
		WHERE type = $1 AND payload ->> 'user_uid' = $2`, jobs.TypeTaskDigest, person).Scan(&queued); err != nil || queued != 2 {
		t.Errorf("queued digest jobs for the person = %d (%v), want one per project", queued, err)
	}

	if err := store.MarkDigested(ctx, f.A.ProjectUID, person, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("MarkDigested(A): %v", err)
	}
	gotA, err := store.DigestFor(ctx, f.A.ProjectUID, person)
	if err != nil || gotA != nil {
		t.Errorf("DigestFor(A) after A's stamp = %+v, %v; want nothing new", gotA, err)
	}
	gotB, err := store.DigestFor(ctx, f.B.ProjectUID, person)
	if err != nil || gotB == nil || gotB.Tasks[0].UID != f.B.TaskUID {
		t.Errorf("DigestFor(B) after A's stamp = %s, %v; want B's task still due", fmt.Sprint(gotB), err)
	}
}
