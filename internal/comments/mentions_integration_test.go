//go:build integration

package comments_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/project"
)

// recordingObserver keeps every report the store makes, with the comment's
// body at the time, so a test can tell which mentions counted as fresh.
type recordingObserver struct {
	calls []observed
}

// observed is one Mentioned call.
type observed struct {
	commentUID string
	mentioned  []string
}

// Mentioned records the call.
func (r *recordingObserver) Mentioned(_ context.Context, _ pgx.Tx, c comments.Comment, mentioned []comments.Mention) error {
	uids := make([]string, 0, len(mentioned))
	for _, m := range mentioned {
		uids = append(uids, m.UserUID)
	}
	r.calls = append(r.calls, observed{commentUID: c.UID, mentioned: uids})
	return nil
}

// makeApprovedUser inserts an approved account — the kind that can be
// mentioned — with the given uid, username and display name.
func (f *fixture) makeApprovedUser(t *testing.T, uid, username, displayName string, disabled bool) string {
	t.Helper()
	approved := time.Now().Add(-time.Hour)
	if err := f.users.CreateUser(context.Background(), auth.User{
		UID: uid, Username: username, Email: username + "@example.test", DisplayName: displayName,
		PasswordHash: "x", Role: auth.RoleUser, ProjectRole: project.RoleViewer, Disabled: disabled, ApprovedAt: &approved,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// exec runs one statement or fails the test.
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.db.Pool().Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// storedMentions reads comment_mentions for one comment, in position order.
func (f *fixture) storedMentions(t *testing.T, commentUID string) []string {
	t.Helper()
	rows, err := f.db.Pool().Query(context.Background(),
		`SELECT user_uid FROM comment_mentions WHERE comment_uid = $1 ORDER BY position`, commentUID)
	if err != nil {
		t.Fatalf("reading comment_mentions: %v", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scanning comment_mentions: %v", err)
	}
	return out
}

// mentionUIDs returns the uids of a comment's resolved mentions, in order.
func mentionUIDs(ms []comments.Mention) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.UserUID)
	}
	return out
}

// TestCreate_storesAndResolvesMentions verifies that a comment's tokens are
// resolved against the accounts — an unknown uid, a disabled account and a
// not-yet-approved one are dropped, a repeated one counts once — stored in
// comment_mentions in body order, returned on the create and on every read
// with the account's current name and linked person, and reported to the
// observer without the writer's own mention.
func TestCreate_storesAndResolvesMentions(t *testing.T) {
	f := newFixture(t)
	observer := &recordingObserver{}
	store := f.comments.WithMentionObserver(observer)
	alice := f.makeApprovedUser(t, "us_alice", "alice", "Alice A.", false)
	bob := f.makeApprovedUser(t, "us_bob", "bob", "Bob B.", false)
	off := f.makeApprovedUser(t, "us_off", "off", "Vypnutý", true)
	pending := f.makeUser(t, "us_new", "newcomer", "Nováček")
	f.exec(t, `INSERT INTO subjects (uid, slug, name) VALUES ('su_bob', 'bob', 'Bob')`)
	f.exec(t, `UPDATE project_members SET subject_uid = 'su_bob' WHERE user_uid = $1`, bob)
	photo := f.makePhoto(t, "one")

	body := "Hele @[Bob](user:us_bob), @[Alice](user:us_alice) a @[Bob](user:us_bob) — " +
		"@[Nikdo](user:us_nobody), @[Vypnutý](user:us_off), @[Nováček](user:us_new), v kódu `@[Bob](user:us_bob)`"
	created, err := store.Create(context.Background(), project.BootstrapUID, comments.PhotoSubject(photo.UID), alice, body,
		entry(audit.ActionCommentCreate, alice, photo.UID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_ = off
	_ = pending

	subjectBob := "su_bob"
	wantMentions := []comments.Mention{
		{UserUID: bob, Name: "Bob B.", SubjectUID: &subjectBob},
		{UserUID: alice, Name: "Alice A."},
	}
	if !reflect.DeepEqual(created.Mentions, wantMentions) {
		t.Errorf("created.Mentions = %+v, want %+v", created.Mentions, wantMentions)
	}
	if got := f.storedMentions(t, created.UID); !reflect.DeepEqual(got, []string{bob, alice}) {
		t.Errorf("comment_mentions = %v, want [%s %s]", got, bob, alice)
	}
	if want := []observed{{commentUID: created.UID, mentioned: []string{bob}}}; !reflect.DeepEqual(observer.calls, want) {
		t.Errorf("observer calls = %+v, want %+v (the writer's own mention left out)", observer.calls, want)
	}

	// A read resolves the mentions afresh: a rename shows the new name.
	f.exec(t, `UPDATE users SET display_name = 'Robert B.' WHERE uid = $1`, bob)
	list, err := store.List(context.Background(), project.BootstrapUID, comments.PhotoSubject(photo.UID))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Mentions[0].Name != "Robert B." {
		t.Errorf("List mentions = %+v, want Bob under his new name", list)
	}
	got, err := store.Get(context.Background(), project.BootstrapUID, created.UID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(mentionUIDs(got.Mentions), []string{bob, alice}) {
		t.Errorf("Get mentions = %v", mentionUIDs(got.Mentions))
	}

	// A deleted account drops out of the mentions; the token in the body stays.
	f.exec(t, `DELETE FROM users WHERE uid = $1`, bob)
	got, err = store.Get(context.Background(), project.BootstrapUID, created.UID)
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if !reflect.DeepEqual(mentionUIDs(got.Mentions), []string{alice}) || got.Body != body {
		t.Errorf("after deleting Bob: mentions %v, body changed %v", mentionUIDs(got.Mentions), got.Body != body)
	}
}

// TestCreate_noMentions verifies a plain comment carries an empty, non-nil
// mentions array and never reaches the observer.
func TestCreate_noMentions(t *testing.T) {
	f := newFixture(t)
	observer := &recordingObserver{}
	store := f.comments.WithMentionObserver(observer)
	alice := f.makeApprovedUser(t, "us_alice", "alice", "Alice", false)
	photo := f.makePhoto(t, "one")

	created, err := store.Create(context.Background(), project.BootstrapUID, comments.PhotoSubject(photo.UID), alice, "@alice? ne",
		entry(audit.ActionCommentCreate, alice, photo.UID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Mentions == nil || len(created.Mentions) != 0 {
		t.Errorf("Mentions = %#v, want an empty array", created.Mentions)
	}
	if len(observer.calls) != 0 {
		t.Errorf("observer called for a comment without mentions: %+v", observer.calls)
	}
	list, err := store.List(context.Background(), project.BootstrapUID, comments.PhotoSubject(photo.UID))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list[0].Mentions == nil {
		t.Error("List: Mentions is nil, want an empty array")
	}
}

// TestUpdate_syncsMentions verifies an edit keeps comment_mentions in step
// with the body — a dropped mention is pruned, a new one stored — and that the
// observer hears only the new ones, so fixing a typo announces nobody again.
func TestUpdate_syncsMentions(t *testing.T) {
	f := newFixture(t)
	observer := &recordingObserver{}
	store := f.comments.WithMentionObserver(observer)
	alice := f.makeApprovedUser(t, "us_alice", "alice", "Alice", false)
	bob := f.makeApprovedUser(t, "us_bob", "bob", "Bob", false)
	carol := f.makeApprovedUser(t, "us_carol", "carol", "Carol", false)
	photo := f.makePhoto(t, "one")

	created, err := store.Create(context.Background(), project.BootstrapUID, comments.PhotoSubject(photo.UID), alice,
		"@[Bob](user:us_bob) víš to?", entry(audit.ActionCommentCreate, alice, photo.UID))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	edited, err := store.Update(context.Background(), project.BootstrapUID, created.UID, "@[Bob](user:us_bob) a @[Carol](user:us_carol) víte to?",
		entry(audit.ActionCommentUpdate, alice, photo.UID))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !reflect.DeepEqual(mentionUIDs(edited.Mentions), []string{bob, carol}) {
		t.Errorf("edited mentions = %v, want [%s %s]", mentionUIDs(edited.Mentions), bob, carol)
	}
	again, err := store.Update(context.Background(), project.BootstrapUID, created.UID, "@[Carol](user:us_carol) víš to?",
		entry(audit.ActionCommentUpdate, alice, photo.UID))
	if err != nil {
		t.Fatalf("Update again: %v", err)
	}
	if !reflect.DeepEqual(mentionUIDs(again.Mentions), []string{carol}) {
		t.Errorf("mentions after dropping Bob = %v, want [%s]", mentionUIDs(again.Mentions), carol)
	}
	if got := f.storedMentions(t, created.UID); !reflect.DeepEqual(got, []string{carol}) {
		t.Errorf("comment_mentions = %v, want [%s] (Bob pruned)", got, carol)
	}
	want := []observed{
		{commentUID: created.UID, mentioned: []string{bob}},
		{commentUID: created.UID, mentioned: []string{carol}},
	}
	if !reflect.DeepEqual(observer.calls, want) {
		t.Errorf("observer calls = %+v, want %+v", observer.calls, want)
	}

	// Mentioning Bob again after he was dropped is fresh again.
	if _, err := store.Update(context.Background(), project.BootstrapUID, created.UID, "@[Bob](user:us_bob) znovu",
		entry(audit.ActionCommentUpdate, alice, photo.UID)); err != nil {
		t.Fatalf("Update third time: %v", err)
	}
	if len(observer.calls) != 3 || !reflect.DeepEqual(observer.calls[2].mentioned, []string{bob}) {
		t.Errorf("observer calls after re-mentioning Bob = %+v", observer.calls)
	}
}

// TestMentions_taskThread verifies the same resolution on a task's thread,
// and that a comment's deletion keeps its rows (the cascade is the comment
// row's, and a soft delete keeps the row).
func TestMentions_taskThread(t *testing.T) {
	f := newFixture(t)
	observer := &recordingObserver{}
	store := f.comments.WithMentionObserver(observer)
	alice := f.makeApprovedUser(t, "us_alice", "alice", "Alice", false)
	bob := f.makeApprovedUser(t, "us_bob", "bob", "Bob", false)
	f.exec(t, `INSERT INTO photo_tasks (uid, title, created_by) VALUES ('pt_one', 'Kdy to bylo?', $1)`, alice)

	created, err := store.Create(context.Background(), project.BootstrapUID, comments.TaskSubject("pt_one"), alice,
		"@[Bob](user:us_bob)?", audit.Entry{ActorUID: alice, Action: audit.ActionCommentCreate, TargetType: "photo_tasks"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !reflect.DeepEqual(mentionUIDs(created.Mentions), []string{bob}) || created.TaskUID != "pt_one" {
		t.Errorf("created = %+v", created)
	}
	if err := store.Delete(context.Background(), project.BootstrapUID, created.UID,
		audit.Entry{ActorUID: alice, Action: audit.ActionCommentDelete, TargetType: "photo_tasks"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := f.storedMentions(t, created.UID); !reflect.DeepEqual(got, []string{bob}) {
		t.Errorf("comment_mentions after a soft delete = %v, want [%s]", got, bob)
	}
}
