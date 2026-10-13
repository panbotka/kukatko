//go:build integration

package tagnotifyjob_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/tagnotifyjob"
)

// TestTagInB_isAnnouncedOnlyInB makes A's member a member of B linked to B's
// person, tags that person on a new photo of B and closes the window: the
// notification lands in the member's inbox in B, opening its own page under
// B's slug, and the member's inbox in A stays empty.
func TestTagInB_isAnnouncedOnlyInB(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	f := projecttest.Seed(t, pool)
	ctx := t.Context()
	member := f.A.UserUID

	projects := project.NewStore(pool)
	if _, err := projects.AddMember(ctx, audit.Meta{}, f.B.ProjectUID, member, project.RoleViewer, true); err != nil {
		t.Fatalf("adding A's member to B: %v", err)
	}
	const photoUID = "ph-tagged-in-b"
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"UPDATE project_members SET subject_uid = $3 WHERE user_uid = $1 AND project_uid = $2",
			[]any{member, f.B.ProjectUID, f.B.SubjectUID}},
		{`INSERT INTO notification_prefs (user_uid, kind, channel, enabled) VALUES ($1, 'tagged', 'in_app', true)`,
			[]any{member}},
		{`INSERT INTO photos (uid, project_id, file_hash, file_path, taken_at) VALUES ($1, $2, $3, $4, now())`,
			[]any{photoUID, f.B.ProjectUID, photoUID, photoUID + ".jpg"}},
	} {
		if _, err := pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seeding: %v", err)
		}
	}

	deliverer := notifydeliver.New(notifydeliver.Config{
		Projects: projects, Notifications: notification.NewStore(pool),
		Push: pushjob.NewEnqueuer(pushjob.EnqueuerConfig{}), Mail: mailjob.NewEnqueuer(mailjob.EnqueuerConfig{}),
	})
	recorder := tagnotifyjob.NewRecorder(tagnotifyjob.RecorderConfig{Window: time.Hour, Audience: deliverer})
	subject := f.B.SubjectUID
	if _, err := people.NewStore(pool).WithTagObserver(recorder).CreateMarkerAudited(ctx, f.B.ProjectUID,
		people.Marker{PhotoUID: photoUID, SubjectUID: &subject, W: 0.1, H: 0.1},
		audit.Entry{Action: audit.ActionFaceAssign, TargetType: "markers", ActorUID: f.B.UserUID}); err != nil {
		t.Fatalf("tagging in B: %v", err)
	}
	if err := tagnotifyjob.New(tagnotifyjob.Config{DB: pool, Deliver: deliverer}).Close(ctx, member); err != nil {
		t.Fatalf("closing the window: %v", err)
	}

	inbox := notification.NewStore(pool)
	inA, err := inbox.List(ctx, f.A.ProjectUID, member, notification.ListOptions{})
	if err != nil || inA.Total != 0 {
		t.Errorf("the member's inbox in A = %+v, %v; want empty", inA.Notifications, err)
	}
	inB, err := inbox.List(ctx, f.B.ProjectUID, member, notification.ListOptions{})
	if err != nil {
		t.Fatalf("the member's inbox in B: %v", err)
	}
	if inB.Total != 1 || inB.Notifications[0].Link != notification.Path(f.B.Slug, inB.Notifications[0].UID) {
		t.Fatalf("the member's inbox in B = %+v, want one notification opening its page in B", inB.Notifications)
	}
	photos, err := inbox.Photos(ctx, f.B.ProjectUID, member, inB.Notifications[0].UID, notification.Visibility{})
	if err != nil || len(photos) != 1 || photos[0] != photoUID {
		t.Errorf("the notification's photos = %v, %v; want the photo tagged in B", photos, err)
	}
}
