//go:build integration

package notifypurgejob_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifypurgejob"
	"github.com/panbotka/kukatko/internal/project"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// TestPurge_reachesADeactivatedProject proves the retention applies to a
// deactivated project too (J-6): one scheduler round over the real project list
// queues its purge, and running it deletes its notifications past the
// retention while keeping a fresh one — a library switched off no longer keeps
// its notifications forever.
func TestPurge_reachesADeactivatedProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	ctx := t.Context()
	const offUID, ownerUID = "pjoff", "us-owner"
	old := time.Now().Add(-200 * 24 * time.Hour)
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO projects (uid, slug, name, active) VALUES ($1, 'off', 'Off', false)`, []any{offUID}},
		{`INSERT INTO users (uid, username, email, password_hash, role) VALUES ($1, 'owner', 'o@example.test', 'x', 'user')`,
			[]any{ownerUID}},
		{`INSERT INTO notifications (uid, project_id, user_uid, kind, title, created_at, read_at)
			VALUES ('ntold', $1, $2, 'tagged', 'old', $3, $3), ('ntnew', $1, $2, 'tagged', 'new', now(), NULL)`,
			[]any{offUID, ownerUID, old}},
	} {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("seeding %q: %v", s.sql, err)
		}
	}

	queue := jobs.NewStore(pool)
	sched := notifypurgejob.NewScheduler(jobs.NewEnqueuer(queue), project.NewStore(pool), time.Hour, nil)
	if _, err := sched.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	svc := notifypurgejob.New(notifypurgejob.Config{Notifications: notification.NewStore(pool)})
	purged := map[string]bool{}
	for {
		job, err := queue.Claim(ctx, "test", jobs.TypeNotificationPurge)
		if err != nil {
			break
		}
		purged[jobs.ProjectUIDOf(job)] = true
		if err := svc.Handle(ctx, job); err != nil {
			t.Fatalf("Handle: %v", err)
		}
	}
	if !purged[offUID] || !purged[project.BootstrapUID] {
		t.Fatalf("purged projects = %v, want the deactivated one and the bootstrap one", purged)
	}
	var left []string
	rows, err := pool.Query(ctx, `SELECT uid FROM notifications WHERE project_id = $1 ORDER BY uid`, offUID)
	if err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		left = append(left, uid)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	if len(left) != 1 || left[0] != "ntnew" {
		t.Fatalf("notifications left in the deactivated project = %v, want only the fresh one", left)
	}
}
