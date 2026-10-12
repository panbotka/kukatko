//go:build integration

package mentionnotify_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/mentionnotify"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// Accounts every fixture seeds: the writer comments; Anna and Bedřich want the
// kind in the app (Anna by mail too); the newcomer never chose; the disabled
// account chose but is switched off.
const (
	writerUID   = "us-writer"
	annaUID     = "us-anna"
	bedrichUID  = "us-bedrich"
	newcomerUID = "us-new"
	disabledUID = "us-off"
)

// fixture is one freshly truncated database with the notifier hooked into the
// comments store over the real delivery path.
type fixture struct {
	db       *database.DB
	comments *comments.Store
	photos   *photos.Store
}

// newFixture builds a fixture with the accounts seeded and the kind wanted in
// the app by Anna, Bedřich and the disabled account, by mail by Anna; mail is
// on, push off.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := &fixture{db: db, photos: photos.NewStore(db.Pool())}
	deliverer := notifydeliver.New(notifydeliver.Config{
		Notifications: notification.NewStore(db.Pool()),
		Push:          pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: false}),
		Mail:          mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true}),
		BaseURL:       "https://kukatko.example.org",
	})
	notifier := mentionnotify.New(mentionnotify.Config{Deliver: deliverer})
	f.comments = comments.NewStore(db.Pool()).WithMentionObserver(notifier)
	users := auth.NewStore(db.Pool())
	approved := time.Now().Add(-time.Hour)
	for uid, name := range map[string]string{
		writerUID: "Pan Botka", annaUID: "Anna", bedrichUID: "Bedřich", newcomerUID: "Nováček", disabledUID: "Vypnutý",
	} {
		if err := users.CreateUser(t.Context(), auth.User{
			UID: uid, Username: uid, Email: uid + "@example.test", DisplayName: name, PasswordHash: "x",
			Role: auth.RoleUser, ProjectRole: project.RoleViewer, Disabled: uid == disabledUID, ApprovedAt: &approved,
		}); err != nil {
			t.Fatalf("creating account %s: %v", uid, err)
		}
	}
	for _, uid := range []string{annaUID, bedrichUID, disabledUID} {
		f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
			VALUES ($1, 'comment_mention', 'in_app', true)`, uid)
	}
	f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
		VALUES ($1, 'comment_mention', 'email', true)`, annaUID)
	return f
}

// exec runs one statement or fails the test.
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// makePhoto catalogues a photo titled title.
func (f *fixture) makePhoto(t *testing.T, name, title string) photos.Photo {
	t.Helper()
	created, err := f.photos.Create(t.Context(), photos.Photo{
		FileHash: (name + strings.Repeat("0", 64))[:64], FilePath: "2026/10/" + name + ".jpg",
		FileName: name + ".jpg", FileSize: 1024, FileMime: "image/jpeg", Title: title,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", name, err)
	}
	return created
}

// comment writes body on subj as writerUID.
func (f *fixture) comment(t *testing.T, subj comments.Subject, body string) comments.Comment {
	t.Helper()
	c, err := f.comments.Create(t.Context(), subj, writerUID, body,
		audit.Entry{ActorUID: writerUID, Action: audit.ActionCommentCreate, TargetType: "photos"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return c
}

// sent is one stored comment_mention notification.
type sent struct {
	user, title, body, link string
	inApp                   bool
}

// sentNotifications returns every comment_mention notification, by account.
func (f *fixture) sentNotifications(t *testing.T) []sent {
	t.Helper()
	rows, err := f.db.Pool().Query(t.Context(), `
		SELECT user_uid, title, body, link, in_app FROM notifications
		WHERE kind = 'comment_mention' ORDER BY user_uid, created_at`)
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

// mails returns the queued mail_send jobs as (template, recipient, data).
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
				Who     string
				OnTask  bool
				OnAlbum bool
				Title   string
				Excerpt string
				URL     string
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("decoding mail job %s: %v", raw, err)
		}
		d := p.Data
		kind := "photo"
		switch {
		case d.OnTask:
			kind = "task"
		case d.OnAlbum:
			kind = "album"
		}
		out = append(out, [3]string{p.Template, p.To, strings.Join([]string{d.Who, d.Title, d.Excerpt, d.URL, kind}, "|")})
	}
	return out
}

// TestPhotoComment_announcesEveryMentionButTheWriter checks a comment on a
// photo: each mentioned person in good standing who wants the kind gets one
// notification naming the writer, the photo and quoting the comment, linking
// to the comment in the viewer, in their inbox; the writer naming themselves
// gets none, the newcomer who never chose none, the disabled account none,
// and Anna, who wants the mail, gets one mail_send with the absolute link.
func TestPhotoComment_announcesEveryMentionButTheWriter(t *testing.T) {
	f := newFixture(t)
	photo := f.makePhoto(t, "one", "Pouť 1987")
	c := f.comment(t, comments.PhotoSubject(photo.UID),
		"Hele @[Anna](user:us-anna), @[Bedřich](user:us-bedrich), @[já](user:us-writer), "+
			"@[Nováček](user:us-new) a @[Vypnutý](user:us-off) — víte, kdo to je?")

	link := "/photos/" + photo.UID + "?panel=info#comment-" + c.UID
	excerpt := "Hele @Anna, @Bedřich, @já, @Nováček a @Vypnutý — víte, kdo to je?"
	want := []sent{
		{user: annaUID, title: "Pan Botka tě zmínil(a) v komentáři u fotky Pouť 1987", body: excerpt, link: link, inApp: true},
		{user: bedrichUID, title: "Pan Botka tě zmínil(a) v komentáři u fotky Pouť 1987", body: excerpt, link: link, inApp: true},
	}
	if got := f.sentNotifications(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("notifications = %+v, want %+v", got, want)
	}
	wantMails := [][3]string{{"comment_mention", "us-anna@example.test",
		"Pan Botka|Pouť 1987|" + excerpt + "|https://kukatko.example.org" + link + "|photo"}}
	if got := f.mails(t); !reflect.DeepEqual(got, wantMails) {
		t.Fatalf("mails = %+v, want %+v", got, wantMails)
	}
}

// TestTaskComment_linksToTheTask checks a comment on a task names the task and
// links to the comment on the task's page.
func TestTaskComment_linksToTheTask(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `INSERT INTO photo_tasks (uid, title, created_by) VALUES ('pt-one', 'Kdy to bylo?', $1)`, writerUID)
	c := f.comment(t, comments.TaskSubject("pt-one"), "@[Anna](user:us-anna) víš to?")

	want := []sent{{user: annaUID, title: "Pan Botka tě zmínil(a) v komentáři u úkolu Kdy to bylo?",
		body: "@Anna víš to?", link: "/tasks/pt-one#comment-" + c.UID, inApp: true}}
	if got := f.sentNotifications(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("notifications = %+v, want %+v", got, want)
	}
	if got := f.mails(t); len(got) != 1 || !strings.HasSuffix(got[0][2], "|task") {
		t.Fatalf("mails = %+v, want one task mail", got)
	}
}

// TestAlbumComment_linksToTheAlbum checks a comment on an album names the album
// and links to the comment on the album's page with the comments drawer open;
// a private album is announced like any other, because its page is open to
// every signed-in role.
func TestAlbumComment_linksToTheAlbum(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `INSERT INTO albums (uid, slug, title, private) VALUES ('al-one', 'svatba', 'Svatba 1987', true)`)
	c := f.comment(t, comments.AlbumSubject("al-one"), "@[Anna](user:us-anna) kdo tam ještě byl?")

	want := []sent{{user: annaUID, title: "Pan Botka tě zmínil(a) v komentáři u alba Svatba 1987",
		body: "@Anna kdo tam ještě byl?", link: "/albums/al-one?panel=comments#comment-" + c.UID, inApp: true}}
	if got := f.sentNotifications(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("notifications = %+v, want %+v", got, want)
	}
	if got := f.mails(t); len(got) != 1 || !strings.HasSuffix(got[0][2], "|album") {
		t.Fatalf("mails = %+v, want one album mail", got)
	}
}

// TestHiddenPhoto_announcesNobody checks a private, hidden or archived photo:
// the mention is stored (the thread still highlights it) but nobody is told.
func TestHiddenPhoto_announcesNobody(t *testing.T) {
	f := newFixture(t)
	for _, flag := range []string{"private = true", "hidden_from_library = true", "archived_at = now()"} {
		photo := f.makePhoto(t, strings.Fields(flag)[0], "")
		f.exec(t, `UPDATE photos SET `+flag+` WHERE uid = $1`, photo.UID)
		c := f.comment(t, comments.PhotoSubject(photo.UID), "@[Anna](user:us-anna)?")
		if len(c.Mentions) != 1 {
			t.Errorf("%s: mentions = %+v, want Anna stored", flag, c.Mentions)
		}
	}
	if got := f.sentNotifications(t); len(got) != 0 {
		t.Fatalf("notifications = %+v, want none", got)
	}
}

// TestEdit_announcesOnlyTheNewMention checks an edit: the person the previous
// body already named hears nothing again, the newly named one does.
func TestEdit_announcesOnlyTheNewMention(t *testing.T) {
	f := newFixture(t)
	photo := f.makePhoto(t, "one", "")
	c := f.comment(t, comments.PhotoSubject(photo.UID), "@[Anna](user:us-anna)?")
	if _, err := f.comments.Update(t.Context(), c.UID, "@[Anna](user:us-anna) a @[Bedřich](user:us-bedrich)?",
		audit.Entry{ActorUID: writerUID, Action: audit.ActionCommentUpdate, TargetType: "photos"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := f.sentNotifications(t)
	if len(got) != 2 || got[0].user != annaUID || got[1].user != bedrichUID {
		t.Fatalf("notifications = %+v, want one for Anna (the create) and one for Bedřich (the edit)", got)
	}
	if got[0].title != "Pan Botka tě zmínil(a) v komentáři u fotky" {
		t.Errorf("untitled photo title = %q", got[0].title)
	}
}
