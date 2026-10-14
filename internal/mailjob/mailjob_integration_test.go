//go:build integration

package mailjob_test

import (
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notifystanding"
	"github.com/panbotka/kukatko/internal/project"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// TestMailSend_reChecksTheAddressee queues a notification mail for an approved
// member through the real enqueuer and queue, then changes the account's
// standing before the job runs: the mail goes out only while the account is
// still in standing; a disabled account, a removed or unapproved membership and
// a deactivated project all complete the job unsent. The account's own mail
// (no addressee) is sent regardless.
func TestMailSend_reChecksTheAddressee(t *testing.T) {
	tests := []struct {
		name   string
		change string
	}{
		{name: "in standing"},
		{name: "account disabled", change: `UPDATE users SET disabled = true WHERE uid = $1`},
		{name: "membership removed", change: `DELETE FROM project_members WHERE user_uid = $1`},
		{name: "membership unapproved", change: `UPDATE project_members SET approved_at = NULL WHERE user_uid = $1`},
		{
			name: "project deactivated", change: `UPDATE projects SET active = false
				WHERE uid IN (SELECT project_uid FROM project_members WHERE user_uid = $1)`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := dbtest.New(t)
			dbtest.TruncateAll(t, db)
			pool := db.Pool()
			ctx := t.Context()
			const uid = "us-anna"
			for _, sql := range []string{
				`INSERT INTO users (uid, username, email, password_hash, role)
					VALUES ($1, 'anna', 'anna@example.org', 'x', 'user')`,
				`INSERT INTO project_members (project_uid, user_uid, role, approved_at)
					VALUES ('` + project.BootstrapUID + `', $1, 'viewer', now())`,
			} {
				if _, err := pool.Exec(ctx, sql, uid); err != nil {
					t.Fatalf("seeding: %v", err)
				}
			}
			enqueuer := mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true})
			notice := mailjob.AlbumFeatured("anna@example.org", mailer.AlbumFeaturedData{Title: "Pouť"}).
				Addressed(notifystanding.Addressee{UserUID: uid, ProjectUID: project.BootstrapUID, Kind: "album_featured"})
			own := mailjob.AccountApproved("anna@example.org", mailer.AccountApprovedData{SignInURL: "https://k.example"})
			for _, m := range []mailjob.Mail{notice, own} {
				if err := enqueuer.Enqueue(ctx, pool, m); err != nil {
					t.Fatalf("Enqueue: %v", err)
				}
			}
			if tt.change != "" {
				if _, err := pool.Exec(ctx, tt.change, uid); err != nil {
					t.Fatalf("changing the standing: %v", err)
				}
			}

			fake := mailer.NewFake()
			svc := mailjob.NewService(mailjob.ServiceConfig{Sender: fake, Standing: notifystanding.NewChecker(pool)})
			queue := jobs.NewStore(pool)
			for range 2 {
				job, err := queue.Claim(ctx, "test", jobs.TypeMailSend)
				if err != nil {
					t.Fatalf("Claim: %v", err)
				}
				if err := svc.Handle(ctx, job); err != nil {
					t.Fatalf("Handle: %v", err)
				}
			}
			// The account's own mail always goes; the notification mail only
			// while the account is in standing.
			want := []string{own.To}
			if tt.change == "" {
				want = []string{notice.To, own.To}
			}
			got := make([]string, 0, len(want))
			for _, m := range fake.Sent() {
				got = append(got, m.To)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("sent to %v, want %v", got, want)
			}
		})
	}
}
