//go:build integration

package comments_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fixture bundles the stores one comment test needs over a freshly truncated
// integration database.
type fixture struct {
	db       *database.DB
	comments *comments.Store
	photos   *photos.Store
	users    *auth.Store
}

// newFixture returns the comment, photo and user stores over a clean database.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return &fixture{
		db:       db,
		comments: comments.NewStore(db.Pool()),
		photos:   photos.NewStore(db.Pool()),
		users:    auth.NewStore(db.Pool()),
	}
}

// makeUser inserts an account with the given uid, username and display name, and
// returns the uid.
func (f *fixture) makeUser(t *testing.T, uid, username, displayName string) string {
	t.Helper()
	if err := f.users.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		Email:        username + "@example.test",
		DisplayName:  displayName,
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// makePhoto catalogues a photo whose file hash is derived from name (padded to
// the width of a SHA256 hex digest), so several photos in one test never collide
// on the dedup unique key.
func (f *fixture) makePhoto(t *testing.T, name string) photos.Photo {
	t.Helper()
	created, err := f.photos.Create(context.Background(), photos.Photo{
		FileHash: (name + strings.Repeat("0", 64))[:64],
		FilePath: "2026/08/" + name + ".jpg",
		FileName: name + ".jpg",
		FileSize: 1024,
		FileMime: "image/jpeg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", name, err)
	}
	return created
}

// entry builds an audit entry for a comment mutation the way the HTTP layer does:
// the photo is the target, the store adds the comment UID to the details.
func entry(action, actorUID, photoUID string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: action, TargetType: "photos", TargetUID: photoUID}
}

// mustCreate writes a comment and fails the test if the store rejects it.
func (f *fixture) mustCreate(t *testing.T, photoUID, authorUID, body string) comments.Comment {
	t.Helper()
	c, err := f.comments.Create(context.Background(), photoUID, authorUID, body,
		entry(audit.ActionCommentCreate, authorUID, photoUID))
	if err != nil {
		t.Fatalf("Create(%s, %q): %v", photoUID, body, err)
	}
	return c
}

// auditRows returns the audit entries recorded for the given action.
func (f *fixture) auditRows(t *testing.T, action string) []audit.Record {
	t.Helper()
	rows, err := audit.NewStore(f.db.Pool()).List(context.Background(), audit.Filter{Action: action})
	if err != nil {
		t.Fatalf("listing audit %q: %v", action, err)
	}
	return rows
}

// rawCount returns how many photo_comments rows match the given WHERE clause,
// bypassing the store so a soft delete can be told from a real one.
func (f *fixture) rawCount(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM photo_comments WHERE "+where, args...).Scan(&n); err != nil {
		t.Fatalf("counting photo_comments: %v", err)
	}
	return n
}

// TestCreateAndList verifies a thread reads back oldest first with the authors'
// names resolved, and that each create is audited in the same transaction with
// the new comment's UID in the details.
func TestCreateAndList(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")
	bob := f.makeUser(t, "us_bob", "bob", "")
	photo := f.makePhoto(t, "one")
	other := f.makePhoto(t, "two")

	first := f.mustCreate(t, photo.UID, alice, "Kdo je ten kluk vlevo?")
	second := f.mustCreate(t, photo.UID, bob, "  To je stryc Josef.  ")
	f.mustCreate(t, other.UID, alice, "jiná fotka")

	list, err := f.comments.List(context.Background(), photo.UID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List returned %d comments, want 2 (the other photo's thread must not leak)", len(list))
	}
	if list[0].UID != first.UID || list[1].UID != second.UID {
		t.Errorf("List order = %s, %s, want %s, %s (oldest first)",
			list[0].UID, list[1].UID, first.UID, second.UID)
	}
	if list[0].AuthorName != "Alice A." {
		t.Errorf("author name = %q, want the display name", list[0].AuthorName)
	}
	if list[1].AuthorName != "bob" {
		t.Errorf("author name = %q, want the username fallback for an empty display name", list[1].AuthorName)
	}
	if list[1].Body != "To je stryc Josef." {
		t.Errorf("body = %q, want it trimmed", list[1].Body)
	}
	if list[0].EditedAt != nil {
		t.Errorf("edited_at = %v on a fresh comment, want nil", list[0].EditedAt)
	}

	rows := f.auditRows(t, audit.ActionCommentCreate)
	if len(rows) != 3 {
		t.Fatalf("comment.create audit rows = %d, want 3", len(rows))
	}
	if got := rows[0].Details["comment_uid"]; got == nil || got == "" {
		t.Errorf("audit details carry no comment_uid: %v", rows[0].Details)
	}
	if rows[0].TargetUID == nil || *rows[0].TargetUID != other.UID {
		t.Errorf("audit target = %v, want the photo %s", rows[0].TargetUID, other.UID)
	}
}

// TestList_emptyThread verifies a photo without comments — and an unknown photo —
// yields an empty slice rather than an error, so a caller can render a thread
// without a guard.
func TestList_emptyThread(t *testing.T) {
	f := newFixture(t)
	photo := f.makePhoto(t, "one")

	for _, uid := range []string{photo.UID, "ph_missing"} {
		list, err := f.comments.List(context.Background(), uid)
		if err != nil {
			t.Fatalf("List(%s): %v", uid, err)
		}
		if len(list) != 0 {
			t.Errorf("List(%s) = %d comments, want 0", uid, len(list))
		}
	}
}

// TestCreate_validation verifies the body rules and the missing-photo case, and
// that a rejected create writes no audit row.
func TestCreate_validation(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "")
	photo := f.makePhoto(t, "one")

	tests := []struct {
		name     string
		photoUID string
		body     string
		wantErr  error
	}{
		{name: "empty body", photoUID: photo.UID, body: "", wantErr: comments.ErrEmptyBody},
		{name: "whitespace body", photoUID: photo.UID, body: "  \n ", wantErr: comments.ErrEmptyBody},
		{
			name:     "over the length limit",
			photoUID: photo.UID,
			body:     strings.Repeat("a", comments.MaxBodyLen+1),
			wantErr:  comments.ErrBodyTooLong,
		},
		{name: "missing photo", photoUID: "ph_missing", body: "hello", wantErr: comments.ErrPhotoNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.comments.Create(context.Background(), tt.photoUID, alice, tt.body,
				entry(audit.ActionCommentCreate, alice, tt.photoUID))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	if n := f.rawCount(t, "true"); n != 0 {
		t.Errorf("photo_comments rows = %d after only rejected creates, want 0", n)
	}
	if rows := f.auditRows(t, audit.ActionCommentCreate); len(rows) != 0 {
		t.Errorf("comment.create audit rows = %d after only rejected creates, want 0", len(rows))
	}

	t.Run("a body at the limit is accepted", func(t *testing.T) {
		c := f.mustCreate(t, photo.UID, alice, strings.Repeat("ě", comments.MaxBodyLen))
		if c.Body != strings.Repeat("ě", comments.MaxBodyLen) {
			t.Error("a multibyte body at the character limit was altered")
		}
	})
}

// TestUpdate verifies an edit rewrites the body, stamps edited_at, is audited,
// and refuses a comment that does not exist.
func TestUpdate(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")
	photo := f.makePhoto(t, "one")
	created := f.mustCreate(t, photo.UID, alice, "stryc Josef")

	updated, err := f.comments.Update(context.Background(), created.UID, "  strýc Josef  ",
		entry(audit.ActionCommentUpdate, alice, photo.UID))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Body != "strýc Josef" {
		t.Errorf("body = %q, want the trimmed new text", updated.Body)
	}
	if updated.EditedAt == nil {
		t.Fatal("edited_at = nil after an edit, want a timestamp")
	}
	if updated.EditedAt.Before(updated.CreatedAt) {
		t.Errorf("edited_at %v is before created_at %v", updated.EditedAt, updated.CreatedAt)
	}
	if updated.AuthorName != "Alice A." {
		t.Errorf("author name = %q after an edit, want it resolved", updated.AuthorName)
	}

	if _, err := f.comments.Update(context.Background(), "cm_missing", "text",
		entry(audit.ActionCommentUpdate, alice, photo.UID)); !errors.Is(err, comments.ErrNotFound) {
		t.Fatalf("Update of a missing comment error = %v, want ErrNotFound", err)
	}
	if _, err := f.comments.Update(context.Background(), created.UID, " ",
		entry(audit.ActionCommentUpdate, alice, photo.UID)); !errors.Is(err, comments.ErrEmptyBody) {
		t.Fatalf("Update with a blank body error = %v, want ErrEmptyBody", err)
	}
	if rows := f.auditRows(t, audit.ActionCommentUpdate); len(rows) != 1 {
		t.Errorf("comment.update audit rows = %d, want 1 (the rejected edits write none)", len(rows))
	}
}

// TestDelete verifies the delete is soft — the row survives, invisible to every
// read — that it is audited, and that deleting twice is a not-found rather than a
// silent success.
func TestDelete(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "")
	photo := f.makePhoto(t, "one")
	kept := f.mustCreate(t, photo.UID, alice, "first")
	removed := f.mustCreate(t, photo.UID, alice, "second")

	if err := f.comments.Delete(context.Background(), removed.UID,
		entry(audit.ActionCommentDelete, alice, photo.UID)); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if n := f.rawCount(t, "uid = $1 AND deleted_at IS NOT NULL", removed.UID); n != 1 {
		t.Errorf("the deleted row is gone from the table (%d rows with deleted_at), want it kept", n)
	}
	list, err := f.comments.List(context.Background(), photo.UID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].UID != kept.UID {
		t.Errorf("List after delete = %v, want only %s", list, kept.UID)
	}
	if _, err := f.comments.Get(context.Background(), removed.UID); !errors.Is(err, comments.ErrNotFound) {
		t.Errorf("Get of a deleted comment error = %v, want ErrNotFound", err)
	}
	counts, err := f.comments.CountsAmong(context.Background(), []string{photo.UID})
	if err != nil {
		t.Fatalf("CountsAmong: %v", err)
	}
	if counts[photo.UID] != 1 {
		t.Errorf("count after delete = %d, want 1", counts[photo.UID])
	}

	if err := f.comments.Delete(context.Background(), removed.UID,
		entry(audit.ActionCommentDelete, alice, photo.UID)); !errors.Is(err, comments.ErrNotFound) {
		t.Errorf("second Delete error = %v, want ErrNotFound", err)
	}
	if rows := f.auditRows(t, audit.ActionCommentDelete); len(rows) != 1 {
		t.Errorf("comment.delete audit rows = %d, want 1 (the repeat writes none)", len(rows))
	}
	if _, err := f.comments.Update(context.Background(), removed.UID, "back from the dead",
		entry(audit.ActionCommentUpdate, alice, photo.UID)); !errors.Is(err, comments.ErrNotFound) {
		t.Errorf("editing a deleted comment error = %v, want ErrNotFound", err)
	}
}

// TestCountsAmong verifies the bulk count: one aggregate covers many photos,
// photos without comments are absent, deleted comments do not count, and an empty
// request needs no query.
func TestCountsAmong(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "")
	first := f.makePhoto(t, "one")
	second := f.makePhoto(t, "two")
	third := f.makePhoto(t, "three")

	f.mustCreate(t, first.UID, alice, "a")
	f.mustCreate(t, first.UID, alice, "b")
	gone := f.mustCreate(t, first.UID, alice, "c")
	f.mustCreate(t, second.UID, alice, "d")
	if err := f.comments.Delete(context.Background(), gone.UID,
		entry(audit.ActionCommentDelete, alice, first.UID)); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	counts, err := f.comments.CountsAmong(context.Background(),
		[]string{first.UID, second.UID, third.UID, "ph_missing"})
	if err != nil {
		t.Fatalf("CountsAmong: %v", err)
	}
	if counts[first.UID] != 2 {
		t.Errorf("count(%s) = %d, want 2 (the deleted one does not count)", first.UID, counts[first.UID])
	}
	if counts[second.UID] != 1 {
		t.Errorf("count(%s) = %d, want 1", second.UID, counts[second.UID])
	}
	if _, ok := counts[third.UID]; ok {
		t.Errorf("a photo without comments is present in the map, want it absent")
	}
	if len(counts) != 2 {
		t.Errorf("counts = %v, want only the two commented photos", counts)
	}

	empty, err := f.comments.CountsAmong(context.Background(), nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("CountsAmong(nil) = %v, %v, want an empty map and no error", empty, err)
	}
}

// TestCascadeOnPhotoDelete verifies deleting a photo takes its comments with it:
// a purge is permanent, and a thread about a photo that no longer exists has
// nothing left to be about.
func TestCascadeOnPhotoDelete(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "")
	doomed := f.makePhoto(t, "one")
	kept := f.makePhoto(t, "two")
	f.mustCreate(t, doomed.UID, alice, "a")
	f.mustCreate(t, doomed.UID, alice, "b")
	survivor := f.mustCreate(t, kept.UID, alice, "c")

	if _, err := f.db.Pool().Exec(context.Background(),
		"DELETE FROM photos WHERE uid = $1", doomed.UID); err != nil {
		t.Fatalf("deleting the photo: %v", err)
	}

	if n := f.rawCount(t, "photo_uid = $1", doomed.UID); n != 0 {
		t.Errorf("photo_comments rows for the purged photo = %d, want 0 (ON DELETE CASCADE)", n)
	}
	if _, err := f.comments.Get(context.Background(), survivor.UID); err != nil {
		t.Errorf("the other photo's comment did not survive: %v", err)
	}
}

// TestAuthorDeleted verifies a comment outlives its author's account: the row
// stays, the author fields go empty, and nobody can then claim authorship of it.
func TestAuthorDeleted(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")
	photo := f.makePhoto(t, "one")
	created := f.mustCreate(t, photo.UID, alice, "vzpomínka")

	if _, err := f.db.Pool().Exec(context.Background(), "DELETE FROM users WHERE uid = $1", alice); err != nil {
		t.Fatalf("deleting the author: %v", err)
	}

	got, err := f.comments.Get(context.Background(), created.UID)
	if err != nil {
		t.Fatalf("Get after the author was deleted: %v", err)
	}
	if got.Body != "vzpomínka" {
		t.Errorf("body = %q, want the comment to survive its author", got.Body)
	}
	if got.AuthorUID != "" || got.AuthorName != "" {
		t.Errorf("author = %q/%q, want both empty (ON DELETE SET NULL)", got.AuthorUID, got.AuthorName)
	}
}

// makeSubject inserts a person, optionally with a cover photo, and returns its
// uid.
func (f *fixture) makeSubject(t *testing.T, uid, name, coverPhotoUID string) string {
	t.Helper()
	var cover *string
	if coverPhotoUID != "" {
		cover = &coverPhotoUID
	}
	_, err := f.db.Pool().Exec(context.Background(),
		`INSERT INTO subjects (uid, slug, name, type, cover_photo_uid)
		 VALUES ($1, $2, $3, 'person', $4)`, uid, uid, name, cover)
	if err != nil {
		t.Fatalf("creating subject %s: %v", uid, err)
	}
	return uid
}

// linkUser points an account at a person of the library.
func (f *fixture) linkUser(t *testing.T, userUID, subjectUID string) {
	t.Helper()
	if _, err := f.users.SetUserSubject(context.Background(), userUID, &subjectUID); err != nil {
		t.Fatalf("linking %s to %s: %v", userUID, subjectUID, err)
	}
}

// TestAuthorPhoto_onlyWhenTheLinkedPersonHasACover verifies what a thread shows
// for an author's face: the cover photo of the person their account says it is,
// and nothing at all in every incomplete case — no account, no linked person, or
// a linked person nobody has chosen a cover photo for, which is the common one.
func TestAuthorPhoto_onlyWhenTheLinkedPersonHasACover(t *testing.T) {
	f := newFixture(t)
	photo := f.makePhoto(t, "thread")
	cover := f.makePhoto(t, "cover")

	withCover := f.makeUser(t, "us_cover", "babicka", "Babička")
	f.linkUser(t, withCover, f.makeSubject(t, "sub_cover", "Babička", cover.UID))

	noCover := f.makeUser(t, "us_nocover", "dedecek", "Dědeček")
	f.linkUser(t, noCover, f.makeSubject(t, "sub_nocover", "Dědeček", ""))

	unlinked := f.makeUser(t, "us_plain", "soused", "Soused")

	f.mustCreate(t, photo.UID, withCover, "to jsem já")
	f.mustCreate(t, photo.UID, noCover, "a to já")
	f.mustCreate(t, photo.UID, unlinked, "a já nikdo")

	list, err := f.comments.List(context.Background(), photo.UID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("thread has %d comments, want 3", len(list))
	}
	if list[0].AuthorPhotoUID == nil || *list[0].AuthorPhotoUID != cover.UID {
		t.Errorf("linked author's photo = %v, want %s", list[0].AuthorPhotoUID, cover.UID)
	}
	if list[1].AuthorPhotoUID != nil {
		t.Errorf("a person with no cover photo yielded %q, want none", *list[1].AuthorPhotoUID)
	}
	if list[2].AuthorPhotoUID != nil {
		t.Errorf("an unlinked author yielded %q, want none", *list[2].AuthorPhotoUID)
	}
}

// TestAuthorPhoto_clearedWithTheSubject verifies the thread survives the person
// being deleted from the library: the link goes, the comment stays, and the face
// falls back to nothing rather than to a dangling photo reference.
func TestAuthorPhoto_clearedWithTheSubject(t *testing.T) {
	f := newFixture(t)
	photo := f.makePhoto(t, "thread")
	cover := f.makePhoto(t, "cover")
	author := f.makeUser(t, "us_1", "babicka", "Babička")
	f.linkUser(t, author, f.makeSubject(t, "sub_1", "Babička", cover.UID))
	f.mustCreate(t, photo.UID, author, "to jsem já")

	if _, err := f.db.Pool().Exec(context.Background(),
		`DELETE FROM subjects WHERE uid = 'sub_1'`); err != nil {
		t.Fatalf("deleting the subject: %v", err)
	}

	list, err := f.comments.List(context.Background(), photo.UID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].AuthorName != "Babička" {
		t.Fatalf("thread = %+v, want the comment intact", list)
	}
	if list[0].AuthorPhotoUID != nil {
		t.Errorf("author photo = %q, want none once the person is gone", *list[0].AuthorPhotoUID)
	}
}
