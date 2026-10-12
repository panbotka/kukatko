//go:build integration

package albumfeaturedjob_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/albumfeaturedjob"
	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailjob"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/notifydeliver"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/pushjob"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// Accounts every fixture seeds: the curator features the album; the viewer and
// the editor want the kind in the app, the admin never chose, the disabled
// editor chose but is switched off.
const (
	curatorUID  = "us-curator"
	viewerUID   = "us-viewer"
	editorUID   = "us-editor"
	adminUID    = "us-admin"
	disabledUID = "us-off"
)

// fixture is one freshly truncated database with the scheduler hooked into the
// organize store and the handler over the real delivery path.
type fixture struct {
	db    *database.DB
	store *organize.Store
	svc   *albumfeaturedjob.Service
	jobs  *jobs.Store
}

// newFixture builds a fixture with the accounts seeded and the kind wanted in
// the app by the viewer, the editor and the disabled account; mail is on, push
// off.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := &fixture{db: db, jobs: jobs.NewStore(db.Pool())}
	deliverer := notifydeliver.New(notifydeliver.Config{
		Notifications: notification.NewStore(db.Pool()),
		Push:          pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: false}),
		Mail:          mailjob.NewEnqueuer(mailjob.EnqueuerConfig{Enabled: true}),
		BaseURL:       "https://kukatko.example.org",
	})
	f.store = organize.NewStore(db.Pool()).WithFeatureObserver(albumfeaturedjob.NewScheduler(nil))
	f.svc = albumfeaturedjob.New(albumfeaturedjob.Config{DB: db.Pool(), Albums: f.store, Deliver: deliverer})
	for uid, role := range map[string]string{
		curatorUID: "curator", viewerUID: "viewer", editorUID: "editor", adminUID: "admin", disabledUID: "editor",
	} {
		f.exec(t, `INSERT INTO users (uid, username, email, password_hash, role, approved_at, disabled)
			VALUES ($1, $2, $3, 'x', 'user', now(), $4)`, uid, uid, uid+"@example.test", uid == disabledUID)
		f.exec(t, `INSERT INTO project_members (project_uid, user_uid, role, approved_at)
			VALUES ($1, $2, $3, now())`, project.BootstrapUID, uid, role)
	}
	for _, uid := range []string{curatorUID, viewerUID, editorUID, disabledUID} {
		f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
			VALUES ($1, 'album_featured', 'in_app', true)`, uid)
	}
	return f
}

// exec runs one statement or fails the test.
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// count runs a count(*) query or fails the test.
func (f *fixture) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// album creates a hand-made album with one photo in it and returns its uid.
func (f *fixture) album(t *testing.T, title string, private bool) string {
	t.Helper()
	album, err := f.store.CreateAlbum(t.Context(), project.BootstrapUID, organize.Album{Title: title, Private: private})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	photo := "ph" + album.UID
	f.exec(t, `INSERT INTO photos (uid, file_hash, file_path, taken_at) VALUES ($1, $1, $2, now())`, photo, photo+".jpg")
	if err := f.store.AddPhoto(t.Context(), project.BootstrapUID, album.UID, photo); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}
	return album.UID
}

// feature features albumUID for a week on behalf of actor.
func (f *fixture) feature(t *testing.T, albumUID, actor string) {
	t.Helper()
	until := time.Now().Add(7 * 24 * time.Hour)
	entry := audit.Entry{Action: audit.ActionAlbumFeature, TargetType: "albums", ActorUID: actor}
	if _, err := f.store.FeatureAlbumAudited(t.Context(), project.BootstrapUID, albumUID, until, actor, entry); err != nil {
		t.Fatalf("FeatureAlbumAudited: %v", err)
	}
}

// unfeature takes albumUID off the homepage.
func (f *fixture) unfeature(t *testing.T, albumUID string) {
	t.Helper()
	entry := audit.Entry{Action: audit.ActionAlbumUnfeature, TargetType: "albums", ActorUID: curatorUID}
	if _, err := f.store.UnfeatureAlbumAudited(t.Context(), project.BootstrapUID, albumUID, entry); err != nil {
		t.Fatalf("UnfeatureAlbumAudited: %v", err)
	}
}

// queuedJobs returns the queued album_featured_notify jobs.
func (f *fixture) queuedJobs(t *testing.T) []jobs.Job {
	t.Helper()
	state := jobs.StateQueued
	list, err := f.jobs.List(t.Context(), jobs.ListOptions{State: &state})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	var out []jobs.Job
	for _, job := range list {
		if job.Type == jobs.TypeAlbumFeaturedNotify {
			out = append(out, job)
		}
	}
	return out
}

// runJob runs the handler on the single queued job, checks it names albumUID
// and marks it done, the way the worker would.
func (f *fixture) runJob(t *testing.T, albumUID string) {
	t.Helper()
	queued := f.queuedJobs(t)
	if len(queued) != 1 {
		t.Fatalf("queued album_featured_notify jobs = %d, want 1", len(queued))
	}
	var p struct {
		AlbumUID string `json:"album_uid"`
	}
	if err := json.Unmarshal(queued[0].Payload, &p); err != nil || p.AlbumUID != albumUID {
		t.Fatalf("payload = %s (%v), want album_uid %s", queued[0].Payload, err, albumUID)
	}
	if err := f.svc.Handle(t.Context(), queued[0]); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	f.exec(t, "UPDATE jobs SET state = 'done' WHERE id = $1", queued[0].ID)
}

// sent is one stored album_featured notification.
type sent struct {
	user, title, body, link string
	inApp                   bool
	photos                  []string
}

// sentNotifications returns every album_featured notification, by account.
func (f *fixture) sentNotifications(t *testing.T) []sent {
	t.Helper()
	rows, err := f.db.Pool().Query(t.Context(), `
		SELECT n.user_uid, n.title, n.body, n.link, n.in_app,
		       COALESCE(array_agg(np.photo_uid ORDER BY np.position)
		                FILTER (WHERE np.photo_uid IS NOT NULL), '{}')
		FROM notifications n LEFT JOIN notification_photos np ON np.notification_uid = n.uid
		WHERE n.kind = 'album_featured'
		GROUP BY n.uid ORDER BY n.user_uid`)
	if err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	defer rows.Close()
	var out []sent
	for rows.Next() {
		var s sent
		if err := rows.Scan(&s.user, &s.title, &s.body, &s.link, &s.inApp, &s.photos); err != nil {
			t.Fatalf("scanning notification: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	return out
}

// recipients returns who received an album_featured notification.
func (f *fixture) recipients(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, s := range f.sentNotifications(t) {
		out = append(out, s.user)
	}
	return out
}

// TestFeature_announcesToTheRightAccounts features a public album and runs the
// job: the viewer and the editor who want the kind get it (title, body, album
// link, the cover as the frozen set, listed in the inbox), the curator who
// featured it, the admin who never chose and the disabled account do not; the
// editor's e-mail is the album_featured mail with the absolute link.
func TestFeature_announcesToTheRightAccounts(t *testing.T) {
	f := newFixture(t)
	album := f.album(t, "Pouť 2026", false)
	f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
		VALUES ($1, 'album_featured', 'email', true)`, editorUID)

	f.feature(t, album, curatorUID)
	if n := f.count(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Fatalf("notifications before the job ran = %d, want 0 (the fan-out is the job's)", n)
	}
	f.runJob(t, album)

	got := f.sentNotifications(t)
	if len(got) != 2 || got[0].user != editorUID || got[1].user != viewerUID {
		t.Fatalf("recipients = %v, want the editor and the viewer", f.recipients(t))
	}
	for _, s := range got {
		if s.title != "Nové doporučené album: Pouť 2026" || s.link != "/albums/"+album || !s.inApp {
			t.Errorf("notification of %s = %+v", s.user, s)
		}
		if !reflect.DeepEqual(s.photos, []string{"ph" + album}) {
			t.Errorf("frozen set of %s = %v, want the cover", s.user, s.photos)
		}
		if s.body != "Na úvodní stránce Kukátka je nově doporučené album Pouť 2026." {
			t.Errorf("body of %s = %q", s.user, s.body)
		}
	}
	var raw json.RawMessage
	if err := f.db.Pool().QueryRow(t.Context(),
		"SELECT payload FROM jobs WHERE type = $1", jobs.TypeMailSend).Scan(&raw); err != nil {
		t.Fatalf("reading the mail_send job (want exactly one): %v", err)
	}
	var mail struct {
		Template string `json:"template"`
		To       string `json:"to"`
		Data     struct {
			Title string `json:"Title"`
			URL   string `json:"URL"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &mail); err != nil {
		t.Fatalf("decoding the mail payload: %v", err)
	}
	wantURL := "https://kukatko.example.org/albums/" + album
	if mail.Template != "album_featured" || mail.To != editorUID+"@example.test" ||
		mail.Data.Title != "Pouť 2026" || mail.Data.URL != wantURL {
		t.Fatalf("mail = %s, want the album_featured template to the editor linking %s", raw, wantURL)
	}
}

// TestFeature_refeatureIsSilent prolongs a running feature: no second job, no
// second notification. A feature that lapsed and is featured again is a fresh
// one and is announced again.
func TestFeature_refeatureIsSilent(t *testing.T) {
	f := newFixture(t)
	album := f.album(t, "Pouť 2026", false)

	f.feature(t, album, curatorUID)
	f.runJob(t, album)
	f.feature(t, album, curatorUID)
	if n := len(f.queuedJobs(t)); n != 0 {
		t.Fatalf("queued jobs after prolonging = %d, want 0", n)
	}
	if got := f.recipients(t); len(got) != 2 {
		t.Fatalf("recipients after prolonging = %v, want still 2", got)
	}

	f.exec(t, "UPDATE albums SET featured_until = now() - interval '1 day' WHERE uid = $1", album)
	f.feature(t, album, curatorUID)
	f.runJob(t, album)
	if got := f.recipients(t); len(got) != 4 {
		t.Fatalf("recipients after a lapsed feature was renewed = %v, want 4", got)
	}
}

// TestFeature_unfeaturedBeforeTheJobRan takes the album off the homepage before
// the job runs: the job completes and sends nothing. Featuring it again while
// the job is still queued collapses into that one job.
func TestFeature_unfeaturedBeforeTheJobRan(t *testing.T) {
	f := newFixture(t)
	album := f.album(t, "Pouť 2026", false)

	f.feature(t, album, curatorUID)
	f.unfeature(t, album)
	f.feature(t, album, curatorUID)
	f.unfeature(t, album)
	f.runJob(t, album)
	if n := f.count(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Fatalf("notifications for an unfeatured album = %d, want 0", n)
	}
	if n := f.count(t, "SELECT count(*) FROM jobs WHERE type = $1", jobs.TypeMailSend); n != 0 {
		t.Fatalf("mail_send jobs = %d, want 0", n)
	}
}

// TestFeature_privateAlbumReachesCuratorsOnly features a private album: the
// editor (who may curate) is told, the viewer is not, and the curator who
// featured it still is not.
func TestFeature_privateAlbumReachesCuratorsOnly(t *testing.T) {
	f := newFixture(t)
	album := f.album(t, "Rodina", true)
	f.exec(t, `INSERT INTO notification_prefs (user_uid, kind, channel, enabled)
		VALUES ($1, 'album_featured', 'in_app', true)`, adminUID)

	f.feature(t, album, curatorUID)
	f.runJob(t, album)
	if got, want := f.recipients(t), []string{adminUID, editorUID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
}

// TestFeature_nobodyWantsIt features an album nobody asked to hear about: the
// job completes with no record at all.
func TestFeature_nobodyWantsIt(t *testing.T) {
	f := newFixture(t)
	f.exec(t, "DELETE FROM notification_prefs")
	album := f.album(t, "Pouť 2026", false)

	f.feature(t, album, curatorUID)
	f.runJob(t, album)
	if n := f.count(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Fatalf("notifications = %d, want 0", n)
	}
}
