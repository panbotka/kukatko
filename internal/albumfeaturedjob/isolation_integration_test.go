//go:build integration

package albumfeaturedjob_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/albumfeaturedjob"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// TestAnnounce_onlyInsideTheAlbumsProject features B's album and asserts the
// announcement reaches B's member in B's inbox, with a link that opens in B,
// and nobody who is a member of A only.
func TestAnnounce_onlyInsideTheAlbumsProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	ctx := t.Context()
	// B's member features the album, so the announcement goes to somebody
	// else: A's member, made a viewer of B, and a member of A only.
	projects := project.NewStore(pool)
	if _, err := projects.AddMember(ctx, audit.Meta{}, f.B.ProjectUID, f.A.UserUID, project.RoleViewer, true); err != nil {
		t.Fatalf("adding A's member to B: %v", err)
	}
	const outsiderUID = "us-only-in-a"
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users (uid, username, email, password_hash, role, approved_at)
			VALUES ($1, $2, $3, 'x', 'user', now())`, []any{outsiderUID, outsiderUID, outsiderUID + "@example.test"}},
		{`INSERT INTO project_members (project_uid, user_uid, role, approved_at) VALUES ($1, $2, 'viewer', now())`,
			[]any{f.A.ProjectUID, outsiderUID}},
		{`INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
			SELECT uid, 'album_featured', 'in_app', true FROM users WHERE uid = ANY($1)`,
			[]any{[]string{outsiderUID, f.A.UserUID}}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	albums := organize.NewStore(pool)
	if _, err := albums.FeatureAlbumAudited(ctx, f.B.ProjectUID, f.B.AlbumUID, time.Now().Add(24*time.Hour),
		f.B.UserUID, audit.Entry{ActorUID: f.B.UserUID, Action: audit.ActionAlbumFeature}); err != nil {
		t.Fatalf("featuring B's album: %v", err)
	}
	svc := albumfeaturedjob.New(albumfeaturedjob.Config{DB: pool, Albums: albums, Deliver: notifydeliver.New(
		notifydeliver.Config{
			Projects: projects, Notifications: notification.NewStore(pool),
			Push: pushjob.NewEnqueuer(pushjob.EnqueuerConfig{}), Mail: mailjob.NewEnqueuer(mailjob.EnqueuerConfig{}),
		})})
	if err := svc.Announce(ctx, f.B.AlbumUID); err != nil {
		t.Fatalf("Announce: %v", err)
	}

	inbox := notification.NewStore(pool)
	pageB, err := inbox.List(ctx, f.B.ProjectUID, f.A.UserUID, notification.ListOptions{})
	if err != nil {
		t.Fatalf("B's inbox: %v", err)
	}
	want := project.AppPath(f.B.Slug, albumfeaturedjob.AlbumPath(f.B.AlbumUID))
	if pageB.Total != 1 || pageB.Notifications[0].Link != want {
		t.Fatalf("A's member's inbox in B = %+v, want the one announcement linking %s", pageB.Notifications, want)
	}
	for _, read := range []struct{ projectUID, userUID string }{
		{f.A.ProjectUID, f.A.UserUID}, {f.A.ProjectUID, outsiderUID}, {f.B.ProjectUID, outsiderUID},
	} {
		page, err := inbox.List(ctx, read.projectUID, read.userUID, notification.ListOptions{})
		if err != nil || page.Total != 0 {
			t.Errorf("inbox of %s in %s = %d, %v; want empty", read.userUID, read.projectUID, page.Total, err)
		}
	}
}
