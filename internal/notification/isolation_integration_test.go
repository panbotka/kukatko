//go:build integration

package notification_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestStore_inboxIsScopedByProject gives A's member one notification in each
// project and asserts the inbox, its unread count, "read everything" and every
// read by uid see only the project they are asked in.
func TestStore_inboxIsScopedByProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := notification.NewStore(db.Pool())
	ctx := t.Context()
	user := f.A.UserUID

	created := map[string]notification.Notification{}
	for _, tn := range []*projecttest.Tenant{&f.A, &f.B} {
		n, err := store.Create(ctx, notification.New{
			ProjectUID: tn.ProjectUID, ProjectSlug: tn.Slug, UserUID: user, Kind: notification.KindTagged,
			Title: "Označen v " + tn.Slug, SelfLink: true, PhotoUIDs: []string{tn.PhotoUID}, InApp: true,
		})
		if err != nil {
			t.Fatalf("Create in %s: %v", tn.Slug, err)
		}
		if want := project.AppPath(tn.Slug, notification.PathPrefix+n.UID); n.Link != want {
			t.Errorf("self link in %s = %q, want %q", tn.Slug, n.Link, want)
		}
		created[tn.ProjectUID] = n
	}

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		page, err := store.List(ctx, projectUID, user, notification.ListOptions{})
		uids := make([]string, 0, len(page.Notifications))
		for _, n := range page.Notifications {
			uids = append(uids, n.UID)
		}
		if err == nil && page.Total != 1 {
			t.Errorf("List(%s) total = %d, want 1", projectUID, page.Total)
		}
		return uids, err
	})

	foreign := created[f.B.ProjectUID].UID
	if _, err := store.Get(ctx, f.A.ProjectUID, user, foreign); !errors.Is(err, notification.ErrNotFound) {
		t.Errorf("Get(A, B's notification) = %v, want ErrNotFound", err)
	}
	if _, err := store.MarkRead(ctx, f.A.ProjectUID, user, foreign); !errors.Is(err, notification.ErrNotFound) {
		t.Errorf("MarkRead(A, B's notification) = %v, want ErrNotFound", err)
	}
	if _, err := store.Photos(ctx, f.A.ProjectUID, user, foreign, notification.Visibility{}); !errors.Is(err,
		notification.ErrNotFound) {
		t.Errorf("Photos(A, B's notification) = %v, want ErrNotFound", err)
	}

	if marked, err := store.MarkAllRead(ctx, f.A.ProjectUID, user); err != nil || marked != 1 {
		t.Fatalf("MarkAllRead(A) = %d, %v; want exactly A's one", marked, err)
	}
	for side, want := range map[string]int{f.A.ProjectUID: 0, f.B.ProjectUID: 1} {
		if got, err := store.UnreadCount(ctx, side, user); err != nil || got != want {
			t.Errorf("UnreadCount(%s) = %d, %v; want %d", side, got, err, want)
		}
	}
	if _, err := store.UnreadCount(ctx, "", user); !errors.Is(err, notification.ErrNoProject) {
		t.Errorf("UnreadCount without a project = %v, want ErrNoProject", err)
	}
	if _, err := store.UnreadByProject(ctx, user, nil); !errors.Is(err, notification.ErrNoStanding) {
		t.Errorf("UnreadByProject without a standing rule = %v, want ErrNoStanding", err)
	}
	// The standing rule is what narrows the read: one letting every project
	// through counts B's unread one, an empty filter nothing at all.
	all := func(string, string) string { return "TRUE" }
	if got, err := store.UnreadByProject(ctx, user, all); err != nil || len(got) != 1 ||
		got[0] != (notification.ProjectUnread{ProjectUID: f.B.ProjectUID, Unread: 1}) {
		t.Errorf("UnreadByProject(every project) = %+v, %v; want B's one", got, err)
	}
	none := func(string, string) string { return "FALSE" }
	if got, err := store.UnreadByProject(ctx, user, none); err != nil || got == nil || len(got) != 0 {
		t.Errorf("UnreadByProject(no project) = %#v, %v; want an empty, non-nil slice", got, err)
	}
}
