//go:build integration

package notificationapi_test

import (
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/notification"
)

// inboxPage mirrors the inbox listing response.
type inboxPage struct {
	Notifications []struct {
		UID    string  `json:"uid"`
		Title  string  `json:"title"`
		Link   string  `json:"link"`
		ReadAt *string `json:"read_at"`
	} `json:"notifications"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// seedInbox records one notification for userUID, minutesAgo minutes old,
// listed in the inbox or not.
func (e *env) seedInbox(t *testing.T, userUID, title string, inApp bool, minutesAgo int) notification.Notification {
	t.Helper()
	n, err := notification.NewStore(e.db.Pool()).Create(t.Context(), notification.New{
		UserUID: userUID, Kind: notification.KindRegistrationPending, Title: title, Link: "/users", InApp: inApp,
	})
	if err != nil {
		t.Fatalf("Create %s: %v", title, err)
	}
	e.exec(t, "UPDATE notifications SET created_at = now() - make_interval(mins => $2) WHERE uid = $1",
		n.UID, minutesAgo)
	return n
}

// unread reads the caller's unread count through the API.
func (e *env) unread(t *testing.T, s session) int {
	t.Helper()
	status, body := e.do(t, s, http.MethodGet, "/api/v1/notifications/unread-count", "")
	var got struct {
		Unread *int `json:"unread"`
	}
	decode(t, body, &got)
	if status != http.StatusOK || got.Unread == nil {
		t.Fatalf("unread-count = %d %s, want 200 {unread}", status, body)
	}
	return *got.Unread
}

// TestInbox_listCountAndReadAll walks the inbox end to end: the listing shows
// only the caller's in-app notifications, newest first and paged; the count
// covers only the unread ones; "read all" clears them and nobody else's.
func TestInbox_listCountAndReadAll(t *testing.T) {
	e := newEnv(t, envOptions{})
	vera := e.login(t, "vera", auth.RoleViewer)
	ada := e.login(t, "ada", auth.RoleAdmin)
	e.seedInbox(t, vera.uid, "older", true, 30)
	e.seedInbox(t, vera.uid, "push only", false, 20)
	newest := e.seedInbox(t, vera.uid, "newest", true, 10)
	e.seedInbox(t, ada.uid, "ada's", true, 5)

	status, body := e.do(t, vera, http.MethodGet, "/api/v1/notifications", "")
	var page inboxPage
	decode(t, body, &page)
	if status != http.StatusOK || page.Total != 2 || len(page.Notifications) != 2 ||
		page.Notifications[0].UID != newest.UID || page.Notifications[1].Title != "older" {
		t.Fatalf("inbox = %d %s, want [newest older] of 2", status, body)
	}
	status, body = e.do(t, vera, http.MethodGet, "/api/v1/notifications?limit=1&offset=1", "")
	decode(t, body, &page)
	if status != http.StatusOK || len(page.Notifications) != 1 || page.Notifications[0].Title != "older" ||
		page.Total != 2 || page.Limit != 1 || page.Offset != 1 {
		t.Fatalf("second page = %d %s, want [older] with limit 1, offset 1", status, body)
	}
	if got := e.unread(t, vera); got != 2 {
		t.Fatalf("unread = %d, want 2", got)
	}

	if status, body := e.do(t, vera, http.MethodPost, "/api/v1/notifications/"+newest.UID+"/read", ""); status != 200 {
		t.Fatalf("mark one read = %d %s", status, body)
	}
	status, body = e.do(t, vera, http.MethodGet, "/api/v1/notifications?unread=true", "")
	decode(t, body, &page)
	if status != http.StatusOK || page.Total != 1 || page.Notifications[0].Title != "older" {
		t.Fatalf("unread inbox = %d %s, want only older", status, body)
	}

	status, body = e.do(t, vera, http.MethodPost, "/api/v1/notifications/read-all", "")
	var marked struct {
		Marked *int `json:"marked"`
	}
	decode(t, body, &marked)
	if status != http.StatusOK || marked.Marked == nil || *marked.Marked != 1 {
		t.Fatalf("read-all = %d %s, want 200 {marked: 1}", status, body)
	}
	if got := e.unread(t, vera); got != 0 {
		t.Errorf("unread after read-all = %d, want 0", got)
	}
	if got := e.unread(t, ada); got != 1 {
		t.Errorf("ada's unread after vera's read-all = %d, want her 1 untouched", got)
	}
	status, body = e.do(t, ada, http.MethodGet, "/api/v1/notifications", "")
	decode(t, body, &page)
	if status != http.StatusOK || page.Total != 1 || page.Notifications[0].Title != "ada's" {
		t.Errorf("ada's inbox = %d %s, want only her own", status, body)
	}
}

// TestInbox_refusesBadQuery checks malformed paging and filter values are 400s.
func TestInbox_refusesBadQuery(t *testing.T) {
	e := newEnv(t, envOptions{})
	vera := e.login(t, "vera", auth.RoleViewer)
	for _, query := range []string{"?unread=perhaps", "?limit=0", "?limit=x", "?offset=-1", "?offset=x"} {
		if status, body := e.do(t, vera, http.MethodGet, "/api/v1/notifications"+query, ""); status != 400 {
			t.Errorf("GET /notifications%s = %d, want 400: %s", query, status, body)
		}
	}
}
