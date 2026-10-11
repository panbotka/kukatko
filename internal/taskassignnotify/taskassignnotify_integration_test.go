//go:build integration

package taskassignnotify_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/phototask"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/taskassignnotify"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// Accounts every fixture seeds: the creator opens tasks and asks people; Anna
// and Bedřich want the kind in the app (Anna by mail too); the newcomer never
// chose; the disabled account chose but is switched off.
const (
	creatorUID  = "us-creator"
	annaUID     = "us-anna"
	bedrichUID  = "us-bedrich"
	newcomerUID = "us-new"
	disabledUID = "us-off"
)

// fixture is one freshly truncated database with the notifier hooked into the
// task store over the real delivery path.
type fixture struct {
	db    *database.DB
	tasks *phototask.Store
}

// newFixture builds a fixture with the accounts seeded and the kind wanted in
// the app by Anna, Bedřich and the disabled account, by mail by Anna; mail is
// on, push off.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := &fixture{db: db}
	deliverer := notifydeliver.New(notifydeliver.Config{
		Notifications: notification.NewStore(db.Pool()),
		Push:          pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: false}),
		Mail:          mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true}),
		BaseURL:       "https://kukatko.example.org",
	})
	notifier := taskassignnotify.New(taskassignnotify.Config{Deliver: deliverer})
	f.tasks = phototask.NewStore(db.Pool()).WithAssignObserver(notifier)
	users := auth.NewStore(db.Pool())
	approved := time.Now().Add(-time.Hour)
	for uid, name := range map[string]string{
		creatorUID: "Pan Botka", annaUID: "Anna", bedrichUID: "Bedřich", newcomerUID: "Nováček", disabledUID: "Vypnutý",
	} {
		if err := users.CreateUser(t.Context(), auth.User{
			UID: uid, Username: uid, Email: uid + "@example.test", DisplayName: name, PasswordHash: "x",
			Role: auth.RoleEditor, Disabled: uid == disabledUID, ApprovedAt: &approved,
		}); err != nil {
			t.Fatalf("creating account %s: %v", uid, err)
		}
	}
	for _, uid := range []string{annaUID, bedrichUID, disabledUID} {
		f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
			VALUES ($1, 'task_assigned', 'in_app', true)`, uid)
	}
	f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
		VALUES ($1, 'task_assigned', 'email', true)`, annaUID)
	return f
}

// exec runs one statement or fails the test.
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// create opens a task titled title on behalf of actor, asking asked.
func (f *fixture) create(t *testing.T, actor, title string, asked ...string) phototask.Task {
	t.Helper()
	task, err := f.tasks.Create(t.Context(), phototask.Task{Title: title},
		phototask.Opening{AskedUIDs: asked}, audit.Entry{ActorUID: actor, Action: audit.ActionTaskCreate})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return task
}

// assign asks userUID on taskUID on behalf of actor.
func (f *fixture) assign(t *testing.T, taskUID, userUID, actor string) {
	t.Helper()
	entry := audit.Entry{ActorUID: actor, Action: audit.ActionTaskAssign}
	if err := f.tasks.Assign(t.Context(), taskUID, userUID, entry); err != nil {
		t.Fatalf("Assign: %v", err)
	}
}

// sent is one stored task_assigned notification.
type sent struct {
	user, title, body, link string
	inApp                   bool
}

// sentNotifications returns every task_assigned notification, by account.
func (f *fixture) sentNotifications(t *testing.T) []sent {
	t.Helper()
	rows, err := f.db.Pool().Query(t.Context(), `
		SELECT user_uid, title, body, link, in_app FROM notifications
		WHERE kind = 'task_assigned' ORDER BY user_uid, created_at`)
	if err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	defer rows.Close()
	var out []sent
	for rows.Next() {
		var s sent
		if err := rows.Scan(&s.user, &s.title, &s.body, &s.link, &s.inApp); err != nil {
			t.Fatalf("scanning notification: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	return out
}

// recipients returns who received a task_assigned notification, in uid order.
func (f *fixture) recipients(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, s := range f.sentNotifications(t) {
		out = append(out, s.user)
	}
	return out
}

// mails returns the queued mail_send jobs as (template, recipient, task URL).
func (f *fixture) mails(t *testing.T) [][3]string {
	t.Helper()
	rows, err := f.db.Pool().Query(t.Context(), `SELECT payload FROM jobs WHERE type = 'mail_send' ORDER BY id`)
	if err != nil {
		t.Fatalf("reading mail jobs: %v", err)
	}
	defer rows.Close()
	var out [][3]string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scanning mail job: %v", err)
		}
		var p struct {
			Template string `json:"template"`
			To       string `json:"to"`
			Data     struct {
				Who   string
				Title string
				URL   string
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("decoding mail job %s: %v", raw, err)
		}
		out = append(out, [3]string{p.Template, p.To, p.Data.Who + "|" + p.Data.Title + "|" + p.Data.URL})
	}
	return out
}

// TestCreate_asksEveryoneButTheCreator checks an opening that hands the task
// over: each asked person gets one notification naming the creator and the
// task, linking to the task's page, in their inbox; the creator — asked along
// — gets none, the newcomer who never chose gets none, and Anna, who wants
// the mail, gets one mail_send with the absolute link.
func TestCreate_asksEveryoneButTheCreator(t *testing.T) {
	f := newFixture(t)
	task := f.create(t, creatorUID, "Kdy to bylo?", annaUID, bedrichUID, creatorUID, newcomerUID, annaUID)

	got := f.sentNotifications(t)
	want := []sent{
		{user: annaUID, title: "Pan Botka tě přidal(a) k úkolu: Kdy to bylo?", body: "Otevři úkol a odpověz.",
			link: "/tasks/" + task.UID, inApp: true},
		{user: bedrichUID, title: "Pan Botka tě přidal(a) k úkolu: Kdy to bylo?", body: "Otevři úkol a odpověz.",
			link: "/tasks/" + task.UID, inApp: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("notifications = %+v, want %+v", got, want)
	}
	mails := f.mails(t)
	wantMails := [][3]string{{"task_assigned", annaUID + "@example.test",
		"Pan Botka|Kdy to bylo?|https://kukatko.example.org/tasks/" + task.UID}}
	if !reflect.DeepEqual(mails, wantMails) {
		t.Fatalf("mails = %v, want %v", mails, wantMails)
	}
}

// TestAssign_rules checks the hand-over after the fact: one notification for
// the person asked, none for asking them again, none for asking yourself, none
// for a disabled account, and none for somebody who is already on the task by
// having acted on it.
func TestAssign_rules(t *testing.T) {
	f := newFixture(t)
	task := f.create(t, creatorUID, "Kdo je to?")

	f.assign(t, task.UID, annaUID, creatorUID)
	if got := f.recipients(t); !reflect.DeepEqual(got, []string{annaUID}) {
		t.Fatalf("after Assign, recipients = %v, want Anna", got)
	}

	f.assign(t, task.UID, annaUID, creatorUID)
	f.assign(t, task.UID, annaUID, bedrichUID)
	if got := f.recipients(t); !reflect.DeepEqual(got, []string{annaUID}) {
		t.Fatalf("after re-asking, recipients = %v, want still just Anna once", got)
	}

	other := f.create(t, creatorUID, "Sám sobě")
	f.assign(t, other.UID, bedrichUID, bedrichUID)
	f.assign(t, other.UID, disabledUID, creatorUID)
	if got := f.recipients(t); !reflect.DeepEqual(got, []string{annaUID}) {
		t.Fatalf("after self-assign and a disabled account, recipients = %v, want just Anna", got)
	}

	// Bedřich acts on the task (joins by acting), then is asked: already on it.
	acted := f.create(t, creatorUID, "Už tam je")
	state := phototask.StateWorking
	if _, err := f.tasks.Update(t.Context(), acted.UID, phototask.Update{State: &state},
		audit.Entry{ActorUID: bedrichUID, Action: audit.ActionTaskUpdate}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	f.assign(t, acted.UID, bedrichUID, creatorUID)
	if got := f.recipients(t); !reflect.DeepEqual(got, []string{annaUID}) {
		t.Fatalf("after asking somebody already on the task, recipients = %v, want just Anna", got)
	}

	// Bedřich put himself on "other" above, so asking him there is a re-ask;
	// a fresh task is a fresh hand-over, and he hears about that one.
	f.assign(t, other.UID, bedrichUID, creatorUID)
	if got := f.recipients(t); !reflect.DeepEqual(got, []string{annaUID}) {
		t.Fatalf("re-asking Bedřich on a task he is on = %v, want just Anna", got)
	}
	third := f.create(t, creatorUID, "Třetí", bedrichUID)
	got := f.sentNotifications(t)
	if len(got) != 2 || got[1].user != bedrichUID || got[1].link != "/tasks/"+third.UID {
		t.Fatalf("after a third task asked of Bedřich, notifications = %+v, want Anna's and Bedřich's", got)
	}
}

// TestAssign_withoutObserver checks the plain store announces nothing: the
// hook is opt-in.
func TestAssign_withoutObserver(t *testing.T) {
	f := newFixture(t)
	plain := phototask.NewStore(f.db.Pool())
	task := f.create(t, creatorUID, "Potichu")
	entry := audit.Entry{ActorUID: creatorUID, Action: audit.ActionTaskAssign}
	if err := plain.Assign(t.Context(), task.UID, annaUID, entry); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if got := f.recipients(t); got != nil {
		t.Fatalf("recipients = %v, want none without an observer", got)
	}
}
