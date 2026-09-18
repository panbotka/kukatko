//go:build integration

package phototask_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/phototask"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fixture bundles the stores one task test needs over a freshly truncated
// integration database.
type fixture struct {
	db       *database.DB
	tasks    *phototask.Store
	comments *comments.Store
	photos   *photos.Store
	users    *auth.Store
}

// newFixture returns the task, comment, photo and user stores over a clean
// database, with one account already seeded as the actor — every mutation writes
// an audit row, and audit_log.actor_uid is a foreign key.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := &fixture{
		db:       db,
		tasks:    phototask.NewStore(db.Pool()),
		comments: comments.NewStore(db.Pool()),
		photos:   photos.NewStore(db.Pool()),
		users:    auth.NewStore(db.Pool()),
	}
	f.makeUser(t, actor, "panbotka", "Pan Botka")
	return f
}

// actor is the account every mutation in these tests is attributed to.
const actor = "us-actor"

// makeUser inserts an account with the given uid, username and display name.
func (f *fixture) makeUser(t *testing.T, uid, username, displayName string) string {
	t.Helper()
	if err := f.users.CreateUser(context.Background(), auth.User{
		UID: uid, Username: username, Email: username + "@example.test",
		DisplayName: displayName, PasswordHash: "x", Role: auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// makePhoto catalogues a photo whose file hash is derived from name, so several
// photos in one test never collide on the dedup unique key.
func (f *fixture) makePhoto(t *testing.T, name string) photos.Photo {
	t.Helper()
	created, err := f.photos.Create(context.Background(), photos.Photo{
		FileHash: (name + strings.Repeat("0", 64))[:64],
		FilePath: "2026/09/" + name + ".jpg",
		FileName: name + ".jpg",
		FileSize: 1024,
		FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", name, err)
	}
	return created
}

// entry builds an audit entry the way the HTTP layer does, leaving the target to
// the store.
func entry(action string) audit.Entry {
	return audit.Entry{ActorUID: actor, Action: action}
}

// mustCreate opens a task over the given photographs and fails the test if the
// store refuses it.
func (f *fixture) mustCreate(t *testing.T, title string, photoUIDs ...string) phototask.Task {
	t.Helper()
	task, err := f.tasks.Create(context.Background(),
		phototask.Task{Title: title, Body: "kontext", Query: "camera:Olympus"},
		photoUIDs, entry(audit.ActionTaskCreate))
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return task
}

// auditCount returns how many audit rows carry the given action.
func (f *fixture) auditCount(t *testing.T, action string) int {
	t.Helper()
	rows, err := audit.NewStore(f.db.Pool()).List(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("listing audit rows: %v", err)
	}
	return len(rows)
}

// TestCreate_readsBack verifies a new task carries what it was opened with, is
// counted against its photographs, and is audited against itself.
func TestCreate_readsBack(t *testing.T) {
	f := newFixture(t)
	one := f.makePhoto(t, "a")
	two := f.makePhoto(t, "b")

	task := f.mustCreate(t, "V kterém roce?", one.UID, two.UID)

	if task.State != phototask.StateQuestion {
		t.Errorf("state = %q, want the default question", task.State)
	}
	if task.PhotoCount != 2 {
		t.Errorf("photo_count = %d, want 2", task.PhotoCount)
	}
	if task.CoverPhotoUID != one.UID {
		t.Errorf("cover = %q, want the first photo added", task.CoverPhotoUID)
	}
	if task.CreatedByUID != actor || task.CreatedByName != "Pan Botka" {
		t.Errorf("author = %s/%s, want the actor resolved", task.CreatedByUID, task.CreatedByName)
	}
	if task.HasNewAnswer || task.CommentCount != 0 {
		t.Errorf("a fresh task reports comments: %d, new answer: %v", task.CommentCount, task.HasNewAnswer)
	}
	if got := f.auditCount(t, audit.ActionTaskCreate); got != 1 {
		t.Errorf("audit rows = %d, want 1", got)
	}
}

// TestCreate_rejects verifies nothing is written when the task, or one of its
// photographs, is invalid.
func TestCreate_rejects(t *testing.T) {
	f := newFixture(t)
	photo := f.makePhoto(t, "a")

	tests := []struct {
		name      string
		task      phototask.Task
		photoUIDs []string
		wantErr   error
	}{
		{name: "no title", task: phototask.Task{}, wantErr: phototask.ErrEmptyTitle},
		{
			name:    "closed without a resolution",
			task:    phototask.Task{Title: "Kdy?", State: phototask.StateDone},
			wantErr: phototask.ErrClosedNeedsResolution,
		},
		{
			name:    "unknown state",
			task:    phototask.Task{Title: "Kdy?", State: "parked"},
			wantErr: phototask.ErrInvalidState,
		},
		{
			name:      "unknown photo",
			task:      phototask.Task{Title: "Kdy?"},
			photoUIDs: []string{photo.UID, "ph-missing"},
			wantErr:   phototask.ErrPhotoNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.tasks.Create(context.Background(), tt.task, tt.photoUIDs,
				entry(audit.ActionTaskCreate))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create error = %v, want %v", err, tt.wantErr)
			}
		})
	}
	list, total, err := f.tasks.List(context.Background(), phototask.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 || len(list) != 0 {
		t.Errorf("a refused create left %d tasks behind", total)
	}
}

// TestUpdate_closingAndReopening verifies the state moves the two timestamps the
// listing reads, that closing demands a resolution, and that reopening clears
// the closing marks.
func TestUpdate_closingAndReopening(t *testing.T) {
	f := newFixture(t)
	task := f.mustCreate(t, "V kterém roce?")
	ctx := context.Background()

	if _, err := f.tasks.Update(ctx, task.UID,
		phototask.Update{State: statePtr(phototask.StateDone)},
		entry(audit.ActionTaskUpdate)); !errors.Is(err, phototask.ErrClosedNeedsResolution) {
		t.Fatalf("closing without a resolution: error = %v", err)
	}

	closed, err := f.tasks.Update(ctx, task.UID, phototask.Update{
		State:      statePtr(phototask.StateDone),
		Resolution: new("1987 podle kroniky"),
	}, entry(audit.ActionTaskUpdate))
	if err != nil {
		t.Fatalf("closing: %v", err)
	}
	if closed.ClosedAt == nil || closed.ClosedByUID != actor {
		t.Errorf("closed_at = %v, closed_by = %q", closed.ClosedAt, closed.ClosedByUID)
	}
	if !closed.StateAt.After(task.StateAt) {
		t.Errorf("state_at did not move: %v, was %v", closed.StateAt, task.StateAt)
	}

	reopened, err := f.tasks.Update(ctx, task.UID,
		phototask.Update{State: statePtr(phototask.StateQuestion)}, entry(audit.ActionTaskUpdate))
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	if reopened.ClosedAt != nil || reopened.ClosedByUID != "" {
		t.Errorf("reopened task kept closed_at = %v, closed_by = %q",
			reopened.ClosedAt, reopened.ClosedByUID)
	}
	if reopened.Resolution != "1987 podle kroniky" {
		t.Errorf("resolution = %q, want the earlier decision kept", reopened.Resolution)
	}
}

// statePtr returns a pointer to a state, for building a partial Update.
func statePtr(s phototask.State) *phototask.State { return &s }

// TestUpdate_missing verifies editing a task that is not there is a not-found.
func TestUpdate_missing(t *testing.T) {
	f := newFixture(t)
	_, err := f.tasks.Update(context.Background(), "tk-missing",
		phototask.Update{Title: new("Kdy?")}, entry(audit.ActionTaskUpdate))
	if !errors.Is(err, phototask.ErrNotFound) {
		t.Fatalf("Update error = %v, want ErrNotFound", err)
	}
}

// TestMembership verifies photographs can be added and removed, that replaying a
// batch adds nothing, and that a missing task or photograph is refused.
func TestMembership(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	one := f.makePhoto(t, "a")
	two := f.makePhoto(t, "b")
	task := f.mustCreate(t, "Kdy?", one.UID)

	added, err := f.tasks.AddPhotos(ctx, task.UID, []string{one.UID, two.UID},
		entry(audit.ActionTaskAddPhotos))
	if err != nil {
		t.Fatalf("AddPhotos: %v", err)
	}
	if added != 1 {
		t.Errorf("added = %d, want 1 (the other was already there)", added)
	}

	if _, err := f.tasks.AddPhotos(ctx, task.UID, []string{"ph-missing"},
		entry(audit.ActionTaskAddPhotos)); !errors.Is(err, phototask.ErrPhotoNotFound) {
		t.Errorf("adding a missing photo: error = %v, want ErrPhotoNotFound", err)
	}
	if _, err := f.tasks.AddPhotos(ctx, "tk-missing", []string{one.UID},
		entry(audit.ActionTaskAddPhotos)); !errors.Is(err, phototask.ErrNotFound) {
		t.Errorf("adding to a missing task: error = %v, want ErrNotFound", err)
	}

	removed, err := f.tasks.RemovePhotos(ctx, task.UID, []string{two.UID, "ph-missing"},
		entry(audit.ActionTaskRemovePhotos))
	if err != nil {
		t.Fatalf("RemovePhotos: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	after, err := f.tasks.Get(ctx, task.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if after.PhotoCount != 1 {
		t.Errorf("photo_count = %d, want 1", after.PhotoCount)
	}
}

// TestDelete verifies a task takes its membership and its thread with it, and
// that deleting twice is a not-found rather than a silent success.
func TestDelete(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	photo := f.makePhoto(t, "a")
	task := f.mustCreate(t, "Kdy?", photo.UID)
	if _, err := f.comments.Create(ctx, comments.TaskSubject(task.UID), actor, "odpověď",
		audit.Entry{ActorUID: actor, Action: audit.ActionCommentCreate,
			TargetType: "photo_tasks", TargetUID: task.UID}); err != nil {
		t.Fatalf("commenting: %v", err)
	}

	if err := f.tasks.Delete(ctx, task.UID, entry(audit.ActionTaskDelete)); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := f.tasks.Delete(ctx, task.UID, entry(audit.ActionTaskDelete)); !errors.Is(
		err, phototask.ErrNotFound,
	) {
		t.Errorf("deleting twice: error = %v, want ErrNotFound", err)
	}

	var members, thread int
	if err := f.db.Pool().QueryRow(ctx,
		"SELECT (SELECT count(*) FROM photo_task_photos), (SELECT count(*) FROM comments WHERE task_uid IS NOT NULL)",
	).Scan(&members, &thread); err != nil {
		t.Fatalf("counting leftovers: %v", err)
	}
	if members != 0 || thread != 0 {
		t.Errorf("delete left %d members and %d comments behind", members, thread)
	}
	// The photograph itself is untouched: a task is work about a picture, never
	// a claim on it.
	if _, err := f.photos.GetByUID(ctx, photo.UID); err != nil {
		t.Errorf("deleting a task removed its photo: %v", err)
	}
}

// TestList_filtersAndOrder verifies the listing's filters, its paging total, and
// that open tasks come before closed ones.
func TestList_filtersAndOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	photo := f.makePhoto(t, "a")

	open := f.mustCreate(t, "Otevřená otázka o domě", photo.UID)
	working := f.mustCreate(t, "Rozpracovaná")
	if _, err := f.tasks.Update(ctx, working.UID,
		phototask.Update{State: statePtr(phototask.StateWorking)},
		entry(audit.ActionTaskUpdate)); err != nil {
		t.Fatalf("advancing: %v", err)
	}
	done := f.mustCreate(t, "Hotová")
	if _, err := f.tasks.Update(ctx, done.UID, phototask.Update{
		State: statePtr(phototask.StateDone), Resolution: new("hotovo"),
	}, entry(audit.ActionTaskUpdate)); err != nil {
		t.Fatalf("closing: %v", err)
	}

	all, total, err := f.tasks.List(ctx, phototask.Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("List() = %d tasks, total %d, want 3 and 3", len(all), total)
	}
	if all[len(all)-1].UID != done.UID {
		t.Errorf("the closed task is not last: %v", uids(all))
	}

	openOnly, openTotal, err := f.tasks.List(ctx, phototask.Filter{Open: true})
	if err != nil {
		t.Fatalf("List(open): %v", err)
	}
	if openTotal != 2 {
		t.Errorf("open total = %d, want 2", openTotal)
	}
	for _, task := range openOnly {
		if task.State.Closed() {
			t.Errorf("the open listing contains %q", task.State)
		}
	}

	byPhoto, _, err := f.tasks.List(ctx, phototask.Filter{PhotoUID: photo.UID})
	if err != nil {
		t.Fatalf("List(photo): %v", err)
	}
	if len(byPhoto) != 1 || byPhoto[0].UID != open.UID {
		t.Errorf("List(photo) = %v, want just the task the photo is in", uids(byPhoto))
	}

	found, _, err := f.tasks.List(ctx, phototask.Filter{Search: "o domě"})
	if err != nil {
		t.Fatalf("List(search): %v", err)
	}
	if len(found) != 1 || found[0].UID != open.UID {
		t.Errorf("List(search) = %v, want just the matching task", uids(found))
	}

	page, pageTotal, err := f.tasks.List(ctx, phototask.Filter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List(page): %v", err)
	}
	if pageTotal != 3 || len(page) != 1 {
		t.Errorf("paged List = %d of %d, want 1 of 3", len(page), pageTotal)
	}
}

// uids reduces a task slice to its UIDs, for readable failures.
func uids(tasks []phototask.Task) []string {
	out := make([]string, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, task.UID)
	}
	return out
}

// TestList_answered verifies the signal an agent polls for: a reply written
// after the state last moved marks the task as answered, and advancing the state
// clears it again.
func TestList_answered(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	task := f.mustCreate(t, "V kterém roce?")
	f.mustCreate(t, "Bez odpovědi")

	if _, err := f.comments.Create(ctx, comments.TaskSubject(task.UID), actor, "1987",
		audit.Entry{ActorUID: actor, Action: audit.ActionCommentCreate,
			TargetType: "photo_tasks", TargetUID: task.UID}); err != nil {
		t.Fatalf("commenting: %v", err)
	}

	answered, total, err := f.tasks.List(ctx, phototask.Filter{Answered: true})
	if err != nil {
		t.Fatalf("List(answered): %v", err)
	}
	if total != 1 || len(answered) != 1 || answered[0].UID != task.UID {
		t.Fatalf("List(answered) = %v (total %d), want just the replied-to task", uids(answered), total)
	}
	if !answered[0].HasNewAnswer || answered[0].CommentCount != 1 {
		t.Errorf("answered task reports has_new_answer = %v, comments = %d",
			answered[0].HasNewAnswer, answered[0].CommentCount)
	}

	// Advancing the state means "seen": the reply is now older than the move.
	time.Sleep(10 * time.Millisecond)
	if _, err := f.tasks.Update(ctx, task.UID,
		phototask.Update{State: statePtr(phototask.StateWorking)},
		entry(audit.ActionTaskUpdate)); err != nil {
		t.Fatalf("advancing: %v", err)
	}
	stillAnswered, _, err := f.tasks.List(ctx, phototask.Filter{Answered: true})
	if err != nil {
		t.Fatalf("List(answered): %v", err)
	}
	if len(stillAnswered) != 0 {
		t.Errorf("List(answered) = %v after the state moved, want none", uids(stillAnswered))
	}
}

// TestOpenForPhotos verifies the reverse lookup the photo detail draws its chip
// from: the open tasks a photograph is part of, and no closed ones.
func TestOpenForPhotos(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	one := f.makePhoto(t, "a")
	two := f.makePhoto(t, "b")
	lonely := f.makePhoto(t, "c")

	open := f.mustCreate(t, "Otevřená", one.UID, two.UID)
	closed := f.mustCreate(t, "Zavřená", one.UID)
	if _, err := f.tasks.Update(ctx, closed.UID, phototask.Update{
		State: statePtr(phototask.StateRejected), Resolution: new("nedohledatelné"),
	}, entry(audit.ActionTaskUpdate)); err != nil {
		t.Fatalf("closing: %v", err)
	}

	got, err := f.tasks.OpenForPhotos(ctx, []string{one.UID, two.UID, lonely.UID})
	if err != nil {
		t.Fatalf("OpenForPhotos: %v", err)
	}
	if len(got[one.UID]) != 1 || got[one.UID][0].UID != open.UID {
		t.Errorf("open tasks of the first photo = %v, want only the open one", got[one.UID])
	}
	if got[one.UID][0].Title != "Otevřená" || got[one.UID][0].State != phototask.StateQuestion {
		t.Errorf("ref = %+v, want the title and state for the chip", got[one.UID][0])
	}
	if len(got[two.UID]) != 1 {
		t.Errorf("open tasks of the second photo = %v, want one", got[two.UID])
	}
	if _, ok := got[lonely.UID]; ok {
		t.Errorf("a photo in no task appears in the map: %v", got[lonely.UID])
	}
	if empty, err := f.tasks.OpenForPhotos(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("OpenForPhotos(nil) = %v, %v, want an empty map and no error", empty, err)
	}
}
