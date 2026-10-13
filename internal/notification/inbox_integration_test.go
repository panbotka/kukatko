//go:build integration

package notification_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/project"
)

// inbox records one notification for userUID, listed in the inbox or not, and
// stamps its created_at minutesAgo minutes back so the order is deterministic.
func (f *fixture) inbox(t *testing.T, userUID, title string, inApp bool, minutesAgo int) notification.Notification {
	t.Helper()
	created, err := f.store.Create(context.Background(), notification.New{
		ProjectUID: project.BootstrapUID, UserUID: userUID, Kind: notification.KindRegistrationPending, Title: title, Link: "/users", InApp: inApp,
	})
	if err != nil {
		t.Fatalf("Create %s: %v", title, err)
	}
	f.exec(t, "UPDATE notifications SET created_at = now() - make_interval(mins => $2) WHERE uid = $1",
		created.UID, minutesAgo)
	return created
}

// titles returns the titles of a page, in order.
func titles(page notification.Page) []string {
	out := make([]string, 0, len(page.Notifications))
	for _, n := range page.Notifications {
		out = append(out, n.Title)
	}
	return out
}

// TestList_onlyTheOwnersInboxNewestFirst checks the inbox lists the owner's
// in-app notifications newest first, leaves out the records that only back a
// push or an e-mail, and never shows another account's.
func TestList_onlyTheOwnersInboxNewestFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.inbox(t, owner, "old", true, 30)
	f.inbox(t, owner, "push only", false, 20)
	f.inbox(t, owner, "new", true, 10)
	f.inbox(t, stranger, "not yours", true, 5)

	page, err := f.store.List(ctx, project.BootstrapUID, owner, notification.ListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := fmt.Sprint(titles(page)); got != "[new old]" || page.Total != 2 {
		t.Fatalf("inbox = %s (total %d), want [new old] (2)", got, page.Total)
	}
	if !page.Notifications[0].InApp || page.Limit != notification.DefaultListLimit || page.Offset != 0 {
		t.Fatalf("page = %+v, want listed rows and the default bounds", page)
	}

	empty, err := f.store.List(ctx, project.BootstrapUID, "us-nobody", notification.ListOptions{})
	if err != nil || empty.Notifications == nil || len(empty.Notifications) != 0 || empty.Total != 0 {
		t.Fatalf("List(nobody) = %+v, %v; want an empty non-nil page", empty, err)
	}
}

// TestList_pagesAndUnreadFilter checks limit/offset paging is stable and the
// unread filter keeps only unread notifications.
func TestList_pagesAndUnreadFilter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	for i := range 5 {
		f.inbox(t, owner, fmt.Sprintf("n%d", i), true, 50-i)
	}
	read := f.inbox(t, owner, "read", true, 1)
	if _, err := f.store.MarkRead(ctx, project.BootstrapUID, owner, read.UID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	first, err := f.store.List(ctx, project.BootstrapUID, owner, notification.ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	second, err := f.store.List(ctx, project.BootstrapUID, owner, notification.ListOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if fmt.Sprint(titles(first)) != "[read n4]" || fmt.Sprint(titles(second)) != "[n3 n2]" || first.Total != 6 {
		t.Fatalf("pages = %v %v (total %d), want [read n4] [n3 n2] (6)", titles(first), titles(second), first.Total)
	}
	unread, err := f.store.List(ctx, project.BootstrapUID, owner, notification.ListOptions{Unread: true, Limit: 500, Offset: -3})
	if err != nil {
		t.Fatalf("List(unread): %v", err)
	}
	if unread.Total != 5 || len(unread.Notifications) != 5 || unread.Limit != notification.MaxListLimit ||
		unread.Offset != 0 {
		t.Fatalf("unread page = %v (total %d, limit %d, offset %d), want the 5 unread, clamped bounds",
			titles(unread), unread.Total, unread.Limit, unread.Offset)
	}
	for _, n := range unread.Notifications {
		if n.ReadAt != nil {
			t.Errorf("unread filter returned the read %s", n.Title)
		}
	}
}

// TestUnreadCountAndMarkAllRead checks the bell's count covers only the
// owner's unread inbox, and "mark all read" clears exactly that — keeping the
// first reading of an already-read one and leaving records outside the inbox
// and other accounts alone.
func TestUnreadCountAndMarkAllRead(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.inbox(t, owner, "a", true, 3)
	f.inbox(t, owner, "b", true, 2)
	alreadyRead := f.inbox(t, owner, "c", true, 1)
	firstRead, err := f.store.MarkRead(ctx, project.BootstrapUID, owner, alreadyRead.UID)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	pushOnly := f.inbox(t, owner, "push only", false, 1)
	f.inbox(t, stranger, "theirs", true, 1)

	if n, err := f.store.UnreadCount(ctx, project.BootstrapUID, owner); err != nil || n != 2 {
		t.Fatalf("UnreadCount = %d, %v; want 2", n, err)
	}
	marked, err := f.store.MarkAllRead(ctx, project.BootstrapUID, owner)
	if err != nil || marked != 2 {
		t.Fatalf("MarkAllRead = %d, %v; want 2", marked, err)
	}
	if n, err := f.store.UnreadCount(ctx, project.BootstrapUID, owner); err != nil || n != 0 {
		t.Fatalf("UnreadCount after MarkAllRead = %d, %v; want 0", n, err)
	}
	if again, err := f.store.MarkAllRead(ctx, project.BootstrapUID, owner); err != nil || again != 0 {
		t.Fatalf("second MarkAllRead = %d, %v; want 0", again, err)
	}
	kept, err := f.store.Get(ctx, project.BootstrapUID, owner, alreadyRead.UID)
	if err != nil || kept.ReadAt == nil || !kept.ReadAt.Equal(*firstRead.ReadAt) {
		t.Fatalf("already-read notification = %+v, %v; want its first reading kept", kept, err)
	}
	if got, err := f.store.Get(ctx, project.BootstrapUID, owner, pushOnly.UID); err != nil || got.ReadAt != nil || got.InApp {
		t.Fatalf("record outside the inbox = %+v, %v; want it untouched and readable by uid", got, err)
	}
	if n, err := f.store.UnreadCount(ctx, project.BootstrapUID, stranger); err != nil || n != 1 {
		t.Fatalf("stranger's UnreadCount = %d, %v; want their 1 untouched", n, err)
	}
}
