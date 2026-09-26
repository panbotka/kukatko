//go:build integration

package tagnotifyjob_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/notification"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/pushjob"
	"github.com/panbotka/kukatko/internal/tagnotifyjob"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// Accounts every fixture seeds: anna is linked to the subject being tagged and
// has one subscribed browser; tagger is somebody else doing the tagging.
const (
	annaUID   = "us-anna"
	taggerUID = "us-tagger"
)

// fixture is one freshly truncated database with the recorder hooked into the
// people store, the handler, and a subject linked to anna.
type fixture struct {
	db       *database.DB
	people   *people.Store
	recorder *tagnotifyjob.Recorder
	svc      *tagnotifyjob.Service
	jobs     *jobs.Store
	subject  string
	now      time.Time
}

// newFixture builds a fixture with a one-hour window measured from a fixed
// clock.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := &fixture{db: db, jobs: jobs.NewStore(db.Pool()), now: time.Now().UTC().Truncate(time.Second)}
	notes := notification.NewStore(db.Pool())
	f.recorder = tagnotifyjob.NewRecorder(tagnotifyjob.RecorderConfig{
		Enabled: true, Window: time.Hour, Preferences: notes, Now: func() time.Time { return f.now },
	})
	f.people = people.NewStore(db.Pool()).WithTagObserver(f.recorder)
	f.svc = tagnotifyjob.New(tagnotifyjob.Config{
		DB: db.Pool(), Notifications: notes,
		Push: pushjob.NewEnqueuer(pushjob.EnqueuerConfig{Enabled: true}),
	})
	for _, uid := range []string{annaUID, taggerUID} {
		f.exec(t, `INSERT INTO users (uid, username, email, password_hash, role, approved_at)
			VALUES ($1, $2, $3, 'x', 'editor', now())`, uid, uid, uid+"@example.test")
	}
	subject, err := f.people.CreateSubject(t.Context(), people.Subject{Name: "Anna"})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	f.subject = subject.UID
	f.exec(t, "UPDATE users SET subject_uid = $2 WHERE uid = $1", annaUID, f.subject)
	f.exec(t, `INSERT INTO push_subscriptions (id, user_uid, endpoint, p256dh, auth, user_agent)
		VALUES ('psanna', $1, 'https://push.example.test/anna', 'key', 'secret', 'test')`, annaUID)
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

// addPhoto inserts a live photo taken daysAgo days before now and returns its
// uid.
func (f *fixture) addPhoto(t *testing.T, name string, daysAgo int) string {
	t.Helper()
	uid := "ph" + name
	f.exec(t, `INSERT INTO photos (uid, file_hash, file_path, taken_at) VALUES ($1, $2, $3, $4)`,
		uid, uid, uid+".jpg", f.now.AddDate(0, 0, -daysAgo))
	return uid
}

// tag puts anna's subject on photoUID the way the face UI does — a new marker,
// audited, by actor — and returns the marker uid.
func (f *fixture) tag(t *testing.T, photoUID, actor string) string {
	t.Helper()
	marker, err := f.people.CreateMarkerAudited(t.Context(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &f.subject, W: 0.1, H: 0.1,
	}, audit.Entry{Action: audit.ActionFaceAssign, TargetType: "markers", ActorUID: actor})
	if err != nil {
		t.Fatalf("tagging %s: %v", photoUID, err)
	}
	return marker.UID
}

// untag clears the subject of markerUID the audited way.
func (f *fixture) untag(t *testing.T, markerUID string) {
	t.Helper()
	if _, err := f.people.UnassignSubjectAudited(t.Context(), markerUID, audit.Entry{
		Action: audit.ActionFaceUnassign, TargetType: "markers", ActorUID: taggerUID,
	}); err != nil {
		t.Fatalf("untagging %s: %v", markerUID, err)
	}
}

// notices is how many notices anna has waiting.
func (f *fixture) notices(t *testing.T) int {
	t.Helper()
	return f.count(t, "SELECT count(*) FROM tag_notices WHERE user_uid = $1", annaUID)
}

// queuedJobs returns anna's queued tag_notify jobs.
func (f *fixture) queuedJobs(t *testing.T) []jobs.Job {
	t.Helper()
	state := jobs.StateQueued
	list, err := f.jobs.List(t.Context(), jobs.ListOptions{State: &state})
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	var out []jobs.Job
	for _, job := range list {
		if job.Type == jobs.TypeTagNotify {
			out = append(out, job)
		}
	}
	return out
}

// closeWindow runs the handler on the single queued job and marks it done, the
// way the worker would.
func (f *fixture) closeWindow(t *testing.T) {
	t.Helper()
	queued := f.queuedJobs(t)
	if len(queued) != 1 {
		t.Fatalf("queued tag_notify jobs = %d, want 1", len(queued))
	}
	if err := f.svc.Handle(t.Context(), queued[0]); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	f.exec(t, "UPDATE jobs SET state = 'done' WHERE id = $1", queued[0].ID)
}

// sent is one notification anna received, as stored.
type sent struct {
	uid, title, body, link string
	photos                 []string
}

// sentNotifications returns anna's notifications, oldest first, with their
// frozen photo sets in stored order.
func (f *fixture) sentNotifications(t *testing.T) []sent {
	t.Helper()
	rows, err := f.db.Pool().Query(t.Context(), `
		SELECT n.uid, n.title, n.body, n.link,
		       COALESCE(array_agg(np.photo_uid ORDER BY np.position)
		                FILTER (WHERE np.photo_uid IS NOT NULL), '{}')
		FROM notifications n LEFT JOIN notification_photos np ON np.notification_uid = n.uid
		WHERE n.user_uid = $1 AND n.kind = 'tagged'
		GROUP BY n.uid ORDER BY n.created_at, n.uid`, annaUID)
	if err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	defer rows.Close()
	var out []sent
	for rows.Next() {
		var s sent
		if err := rows.Scan(&s.uid, &s.title, &s.body, &s.link, &s.photos); err != nil {
			t.Fatalf("scanning notification: %v", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading notifications: %v", err)
	}
	return out
}

// TestWindow_manyTagsCollapseIntoOneNotification tags twelve photos inside one
// window and checks they become one job, one notification with the right
// Czech count, the photos newest first, a link to its own page and one push
// delivery for anna's one device.
func TestWindow_manyTagsCollapseIntoOneNotification(t *testing.T) {
	f := newFixture(t)
	var want []string
	for i := range 12 {
		want = append(want, f.addPhoto(t, fmt.Sprintf("p%02d", i), i))
	}
	// Tag oldest first, so the stored order cannot just be the tagging order.
	for _, uid := range slices.Backward(want) {
		f.tag(t, uid, taggerUID)
	}
	// Tagging the same person twice on one photo still counts once.
	f.tag(t, want[0], taggerUID)

	if n := f.notices(t); n != 12 {
		t.Fatalf("pending notices = %d, want 12", n)
	}
	queued := f.queuedJobs(t)
	if len(queued) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(queued))
	}
	if want := f.now.Add(time.Hour); !queued[0].RunAfter.Equal(want) {
		t.Fatalf("run_after = %v, want one window from the first tag (%v)", queued[0].RunAfter, want)
	}

	f.closeWindow(t)

	got := f.sentNotifications(t)
	if len(got) != 1 {
		t.Fatalf("notifications = %d, want 1", len(got))
	}
	n := got[0]
	if n.title != "Označili vás na fotkách" || n.body != "Přibylo 12 fotek, na kterých vás označili." {
		t.Errorf("text = %q / %q", n.title, n.body)
	}
	if n.link != notification.Path(n.uid) {
		t.Errorf("link = %q, want %q", n.link, notification.Path(n.uid))
	}
	if !slices.Equal(n.photos, want) {
		t.Errorf("photos = %v, want newest first %v", n.photos, want)
	}
	if left := f.notices(t); left != 0 {
		t.Errorf("notices left after the window closed = %d, want 0", left)
	}
	var payload json.RawMessage
	if err := f.db.Pool().QueryRow(t.Context(),
		"SELECT payload FROM jobs WHERE type = $1", jobs.TypePushSend).Scan(&payload); err != nil {
		t.Fatalf("reading the push_send job (want exactly one): %v", err)
	}
	var delivery struct {
		Notification struct{ URL, Tag string } `json:"notification"`
	}
	if err := json.Unmarshal(payload, &delivery); err != nil {
		t.Fatalf("decoding the push payload: %v", err)
	}
	if delivery.Notification.URL != n.link || delivery.Notification.Tag != n.uid {
		t.Errorf("push payload = %s, want url %s and tag %s", payload, n.link, n.uid)
	}
}

// TestWindow_secondWindowOpensAfterTheFirstClosed checks that once a window
// closed, the next tag opens a new one and becomes its own notification — in
// the singular.
func TestWindow_secondWindowOpensAfterTheFirstClosed(t *testing.T) {
	f := newFixture(t)
	f.tag(t, f.addPhoto(t, "first", 1), taggerUID)
	f.tag(t, f.addPhoto(t, "second", 2), taggerUID)
	f.tag(t, f.addPhoto(t, "third", 3), taggerUID)
	f.closeWindow(t)

	f.tag(t, f.addPhoto(t, "later", 0), taggerUID)
	f.closeWindow(t)

	got := f.sentNotifications(t)
	if len(got) != 2 {
		t.Fatalf("notifications = %d, want 2", len(got))
	}
	if got[0].body != "Přibyly 3 fotky, na kterých vás označili." {
		t.Errorf("first body = %q", got[0].body)
	}
	if got[1].title != "Označili vás na fotce" || got[1].body != "Přibyla 1 fotka, na které vás označili." ||
		!slices.Equal(got[1].photos, []string{"phlater"}) {
		t.Errorf("second notification = %+v", got[1])
	}
}

// TestWindow_tagWhileRunningOpensNewWindow checks the dedup index is scoped to
// queued jobs: a tag landing while the job runs opens a fresh window instead
// of being swallowed.
func TestWindow_tagWhileRunningOpensNewWindow(t *testing.T) {
	f := newFixture(t)
	f.tag(t, f.addPhoto(t, "before", 1), taggerUID)
	f.exec(t, "UPDATE jobs SET state = 'running' WHERE type = $1", jobs.TypeTagNotify)

	f.tag(t, f.addPhoto(t, "during", 0), taggerUID)
	if queued := f.queuedJobs(t); len(queued) != 1 {
		t.Fatalf("queued jobs after a tag during a run = %d, want 1 (a new window)", len(queued))
	}
}

// TestWindow_inFlightTagWaitsForTheClosingJob checks the advisory lock: a
// tagging transaction still open when the window closes is waited for, so its
// photo lands in the notification rather than between two windows.
func TestWindow_inFlightTagWaitsForTheClosingJob(t *testing.T) {
	f := newFixture(t)
	f.tag(t, f.addPhoto(t, "early", 1), taggerUID)
	late := f.addPhoto(t, "late", 0)
	marker, err := f.people.CreateMarker(t.Context(), people.Marker{PhotoUID: late, W: 0.2, H: 0.2})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	ctx := t.Context()
	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "UPDATE markers SET subject_uid = $2 WHERE uid = $1", marker.UID, f.subject); err != nil {
		t.Fatalf("assigning in the open transaction: %v", err)
	}
	if err := f.recorder.Tagged(ctx, tx, people.Tagging{
		PhotoUID: late, SubjectUID: f.subject, ActorUID: taggerUID,
	}); err != nil {
		t.Fatalf("Tagged: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- f.svc.Close(ctx, annaUID) }()
	select {
	case err := <-done:
		t.Fatalf("Close finished (%v) while a tag of the same account was still in flight", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Close: %v", err)
	}
	got := f.sentNotifications(t)
	if len(got) != 1 || len(got[0].photos) != 2 {
		t.Fatalf("notifications = %+v, want one naming both photos", got)
	}
}

// TestUntag_removesTheNotice checks an untagging before the window closes
// takes the photo out of the notification.
func TestUntag_removesTheNotice(t *testing.T) {
	f := newFixture(t)
	kept := f.addPhoto(t, "kept", 1)
	f.tag(t, kept, taggerUID)
	marker := f.tag(t, f.addPhoto(t, "gone", 0), taggerUID)
	f.untag(t, marker)

	if n := f.notices(t); n != 1 {
		t.Fatalf("notices after the untag = %d, want 1", n)
	}
	f.closeWindow(t)
	if got := f.sentNotifications(t); len(got) != 1 || !slices.Equal(got[0].photos, []string{kept}) {
		t.Fatalf("notifications = %+v, want one naming only %s", got, kept)
	}
}

// TestUntag_emptyWindowSendsNothing checks a window whose every notice was
// untagged closes successfully and sends nothing — never "0 photos".
func TestUntag_emptyWindowSendsNothing(t *testing.T) {
	f := newFixture(t)
	f.untag(t, f.tag(t, f.addPhoto(t, "gone", 0), taggerUID))
	f.closeWindow(t)
	if got := f.sentNotifications(t); len(got) != 0 {
		t.Fatalf("notifications = %+v, want none", got)
	}
	if n := f.count(t, "SELECT count(*) FROM jobs WHERE type = $1", jobs.TypePushSend); n != 0 {
		t.Fatalf("push_send jobs = %d, want 0", n)
	}
}

// TestHandAttachment_isATagging checks a person attached by hand — the only way
// a video names anybody — records a notice like a face does, and detaching
// them takes it back.
func TestHandAttachment_isATagging(t *testing.T) {
	f := newFixture(t)
	photoUID := f.addPhoto(t, "clip", 0)
	entry := audit.Entry{Action: audit.ActionFaceAssign, TargetType: "markers", ActorUID: taggerUID}
	if _, err := f.people.AttachSubjectToPhoto(t.Context(), photoUID, f.subject, entry); err != nil {
		t.Fatalf("AttachSubjectToPhoto: %v", err)
	}
	if n := f.notices(t); n != 1 || len(f.queuedJobs(t)) != 1 {
		t.Fatalf("after attaching: notices %d, queued jobs %d, want 1 and 1", n, len(f.queuedJobs(t)))
	}
	if _, err := f.people.DetachSubjectFromPhoto(t.Context(), photoUID, f.subject, entry); err != nil {
		t.Fatalf("DetachSubjectFromPhoto: %v", err)
	}
	if n := f.notices(t); n != 0 {
		t.Fatalf("notices after detaching = %d, want 0", n)
	}
}

// TestWindow_photoHiddenInsideTheWindowIsLeftOut checks the job re-reads the
// photo's state: one archived after it was tagged is not named.
func TestWindow_photoHiddenInsideTheWindowIsLeftOut(t *testing.T) {
	f := newFixture(t)
	kept := f.addPhoto(t, "kept", 1)
	archived := f.addPhoto(t, "archived", 0)
	f.tag(t, kept, taggerUID)
	f.tag(t, archived, taggerUID)
	f.exec(t, "UPDATE photos SET archived_at = now() WHERE uid = $1", archived)

	f.closeWindow(t)
	if got := f.sentNotifications(t); len(got) != 1 || !slices.Equal(got[0].photos, []string{kept}) {
		t.Fatalf("notifications = %+v, want one naming only %s", got, kept)
	}
}

// TestRecordsNothing covers every case in which a tagging must leave no notice
// and open no window.
func TestRecordsNothing(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, f *fixture, photoUID string)
		actor string
	}{
		{name: "self assignment", actor: annaUID},
		{name: "private photo", actor: taggerUID, setup: func(t *testing.T, f *fixture, photoUID string) {
			t.Helper()
			f.exec(t, "UPDATE photos SET private = TRUE WHERE uid = $1", photoUID)
		}},
		{name: "hidden photo", actor: taggerUID, setup: func(t *testing.T, f *fixture, photoUID string) {
			t.Helper()
			f.exec(t, "UPDATE photos SET hidden_from_library = TRUE WHERE uid = $1", photoUID)
		}},
		{name: "archived photo", actor: taggerUID, setup: func(t *testing.T, f *fixture, photoUID string) {
			t.Helper()
			f.exec(t, "UPDATE photos SET archived_at = now() WHERE uid = $1", photoUID)
		}},
		{name: "kind turned off", actor: taggerUID, setup: func(t *testing.T, f *fixture, _ string) {
			t.Helper()
			f.exec(t, "INSERT INTO notification_prefs (user_uid, kind, enabled) VALUES ($1, 'tagged', FALSE)", annaUID)
		}},
		{name: "subject linked to no account", actor: taggerUID, setup: func(t *testing.T, f *fixture, _ string) {
			t.Helper()
			f.exec(t, "UPDATE users SET subject_uid = NULL WHERE uid = $1", annaUID)
		}},
		{name: "disabled account", actor: taggerUID, setup: func(t *testing.T, f *fixture, _ string) {
			t.Helper()
			f.exec(t, "UPDATE users SET disabled = TRUE WHERE uid = $1", annaUID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			photoUID := f.addPhoto(t, "p", 0)
			if tc.setup != nil {
				tc.setup(t, f, photoUID)
			}
			f.tag(t, photoUID, tc.actor)
			if n := f.count(t, "SELECT count(*) FROM tag_notices"); n != 0 {
				t.Errorf("notices = %d, want 0", n)
			}
			if n := f.count(t, "SELECT count(*) FROM jobs WHERE type = $1", jobs.TypeTagNotify); n != 0 {
				t.Errorf("tag_notify jobs = %d, want 0", n)
			}
		})
	}
}

// TestRecorder_pushOffRecordsNothing checks an instance with push switched off
// records no notice: nothing would ever be delivered.
func TestRecorder_pushOffRecordsNothing(t *testing.T) {
	f := newFixture(t)
	off := tagnotifyjob.NewRecorder(tagnotifyjob.RecorderConfig{Preferences: notification.NewStore(f.db.Pool())})
	store := people.NewStore(f.db.Pool()).WithTagObserver(off)
	if _, err := store.CreateMarker(t.Context(), people.Marker{
		PhotoUID: f.addPhoto(t, "p", 0), SubjectUID: &f.subject, W: 0.1, H: 0.1,
	}); err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM tag_notices"); n != 0 {
		t.Fatalf("notices with push off = %d, want 0", n)
	}
}

// TestRecorder_rolledBackAssignmentAnnouncesNothing checks the notice and the
// job share the assignment's transaction.
func TestRecorder_rolledBackAssignmentAnnouncesNothing(t *testing.T) {
	f := newFixture(t)
	photoUID := f.addPhoto(t, "p", 0)
	marker, err := f.people.CreateMarker(t.Context(), people.Marker{PhotoUID: photoUID, W: 0.1, H: 0.1})
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	ctx := t.Context()
	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE markers SET subject_uid = $2 WHERE uid = $1", marker.UID, f.subject); err != nil {
		t.Fatalf("assigning: %v", err)
	}
	if err := f.recorder.Tagged(ctx, tx, people.Tagging{PhotoUID: photoUID, SubjectUID: f.subject}); err != nil {
		t.Fatalf("Tagged: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM tag_notices") +
		f.count(t, "SELECT count(*) FROM jobs"); n != 0 {
		t.Fatalf("rows left by a rolled-back assignment = %d, want 0", n)
	}
}

// TestBulk_clusterInOneTransactionQueuesOneJob assigns a whole cluster's worth
// of faces in one transaction and checks the first-notice check queued one job
// for hundreds of notices — and that it becomes one notification.
func TestBulk_clusterInOneTransactionQueuesOneJob(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	const faces = 300
	f.exec(t, `INSERT INTO photos (uid, file_hash, file_path, taken_at)
		SELECT 'phbulk' || g, 'phbulk' || g, 'phbulk' || g || '.jpg', now() - g * interval '1 day'
		FROM generate_series(1, $1) AS g`, faces)
	f.exec(t, `INSERT INTO markers (uid, photo_uid, subject_uid, type, x, y, w, h)
		SELECT 'mkbulk' || g, 'phbulk' || g, $2, 'face', 0, 0, 0.1, 0.1
		FROM generate_series(1, $1) AS g`, faces, f.subject)

	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := 1; i <= faces; i++ {
		if err := f.recorder.Tagged(ctx, tx, people.Tagging{
			PhotoUID: fmt.Sprintf("phbulk%d", i), SubjectUID: f.subject,
		}); err != nil {
			t.Fatalf("Tagged %d: %v", i, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if n := f.notices(t); n != faces {
		t.Fatalf("notices = %d, want %d", n, faces)
	}
	if n := f.count(t, "SELECT count(*) FROM jobs WHERE type = $1", jobs.TypeTagNotify); n != 1 {
		t.Fatalf("tag_notify jobs = %d, want exactly 1", n)
	}
	f.closeWindow(t)
	got := f.sentNotifications(t)
	if len(got) != 1 || len(got[0].photos) != faces || got[0].body != "Přibylo 300 fotek, na kterých vás označili." {
		t.Fatalf("notifications = %d (first: %d photos), want one with %d", len(got), len(got[0].photos), faces)
	}
}

// TestAccountDeleted_windowFindsNothing checks deleting the recipient before
// the window closes cascades its notices away and the job completes quietly.
func TestAccountDeleted_windowFindsNothing(t *testing.T) {
	f := newFixture(t)
	f.tag(t, f.addPhoto(t, "p", 0), taggerUID)
	queued := f.queuedJobs(t)
	f.exec(t, "DELETE FROM users WHERE uid = $1", annaUID)

	if n := f.count(t, "SELECT count(*) FROM tag_notices"); n != 0 {
		t.Fatalf("notices after the account was deleted = %d, want 0", n)
	}
	if len(queued) != 1 {
		t.Fatalf("queued jobs = %d, want 1", len(queued))
	}
	if err := f.svc.Handle(t.Context(), queued[0]); err != nil {
		t.Fatalf("Handle for a deleted account: %v", err)
	}
	if n := f.count(t, "SELECT count(*) FROM notifications"); n != 0 {
		t.Fatalf("notifications = %d, want 0", n)
	}
}
