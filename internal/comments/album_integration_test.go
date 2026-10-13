//go:build integration

package comments_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/project"
)

// makeAlbum inserts a hand-made album with the given uid and title, bypassing
// the organize store: the thread is a view of the album, not of how it was made.
func (f *fixture) makeAlbum(t *testing.T, uid, title string) string {
	t.Helper()
	if _, err := f.db.Pool().Exec(context.Background(),
		`INSERT INTO albums (uid, slug, title) VALUES ($1, $2, $3)`, uid, uid, title); err != nil {
		t.Fatalf("creating album %s: %v", uid, err)
	}
	return uid
}

// albumEntry builds the audit entry the organize API writes for an album thread
// mutation: the album is the target.
func albumEntry(action, actorUID, albumUID string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: action, TargetType: "albums", TargetUID: albumUID}
}

// TestAlbumThread_createListDelete verifies an album carries a thread of its
// own, with exactly the behaviour a photo's has: oldest first, the author
// resolved, the album recorded as the comment's one subject, audited with the
// album as the target, counted in bulk, soft-deleted out of every read, and
// kept apart from a photo's thread.
func TestAlbumThread_createListDelete(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")
	album := f.makeAlbum(t, "al_wedding", "Svatba 1987")
	photo := f.makePhoto(t, "one")
	ctx := context.Background()

	first, err := f.comments.Create(ctx, project.BootstrapUID, comments.AlbumSubject(album), alice, "Kdo tam ještě byl?",
		albumEntry(audit.ActionCommentCreate, alice, album))
	if err != nil {
		t.Fatalf("Create(album): %v", err)
	}
	if first.AlbumUID != album || first.PhotoUID != "" || first.TaskUID != "" ||
		first.Subject() != comments.AlbumSubject(album) || first.AuthorName != "Alice A." {
		t.Fatalf("created = %+v, want an album comment by Alice", first)
	}
	second, err := f.comments.Create(ctx, project.BootstrapUID, comments.AlbumSubject(album), alice, "Strejda Karel.",
		albumEntry(audit.ActionCommentCreate, alice, album))
	if err != nil {
		t.Fatalf("Create(album, second): %v", err)
	}
	// A photo's thread stays a photo's thread.
	f.mustCreate(t, photo.UID, alice, "Na fotce je babička.")

	list, err := f.comments.List(ctx, project.BootstrapUID, comments.AlbumSubject(album))
	if err != nil {
		t.Fatalf("List(album): %v", err)
	}
	if len(list) != 2 || list[0].UID != first.UID || list[1].UID != second.UID {
		t.Fatalf("List(album) = %+v, want the two album comments oldest first", list)
	}
	counts, err := f.comments.CountsAmong(ctx, project.BootstrapUID, comments.SubjectAlbum, []string{album, "al_none"})
	if err != nil {
		t.Fatalf("CountsAmong(album): %v", err)
	}
	if counts[album] != 2 || counts["al_none"] != 0 {
		t.Fatalf("CountsAmong(album) = %v, want 2 for the album and nothing for a stranger", counts)
	}
	latest, err := f.comments.LatestAmong(ctx, project.BootstrapUID, comments.SubjectAlbum, []string{album})
	if err != nil {
		t.Fatalf("LatestAmong(album): %v", err)
	}
	if latest[album].UID != second.UID {
		t.Fatalf("LatestAmong(album) = %+v, want the second comment", latest)
	}

	rows := f.auditRows(t, audit.ActionCommentCreate)
	var albumTargets int
	for _, row := range rows {
		if row.TargetType == "albums" && row.TargetUID != nil && *row.TargetUID == album && row.Details["comment_uid"] != nil {
			albumTargets++
		}
	}
	if albumTargets != 2 {
		t.Fatalf("audit rows targeting the album = %d of %+v, want 2", albumTargets, rows)
	}

	if err := f.comments.Delete(ctx, project.BootstrapUID, first.UID, albumEntry(audit.ActionCommentDelete, alice, album)); err != nil {
		t.Fatalf("Delete(album comment): %v", err)
	}
	list, err = f.comments.List(ctx, project.BootstrapUID, comments.AlbumSubject(album))
	if err != nil {
		t.Fatalf("List(album) after delete: %v", err)
	}
	if len(list) != 1 || list[0].UID != second.UID {
		t.Fatalf("List(album) after delete = %+v, want only the second comment", list)
	}
	if got := f.rawCount(t, "album_uid = $1 AND deleted_at IS NOT NULL", album); got != 1 {
		t.Fatalf("soft-deleted album rows = %d, want 1 (the row stays)", got)
	}
}

// TestAlbumThread_missingAlbum verifies a comment on an album that does not
// exist is refused as a missing subject, the same way a missing photo is.
func TestAlbumThread_missingAlbum(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")

	_, err := f.comments.Create(context.Background(), project.BootstrapUID, comments.AlbumSubject("al_gone"), alice, "Haló?",
		albumEntry(audit.ActionCommentCreate, alice, "al_gone"))
	if !errors.Is(err, comments.ErrSubjectNotFound) {
		t.Fatalf("Create(missing album) error = %v, want ErrSubjectNotFound", err)
	}
	if got := f.rawCount(t, "true"); got != 0 {
		t.Fatalf("comments after a refused create = %d, want 0", got)
	}
}

// TestAlbumThread_oneSubjectCheck pins migration 0099: the database itself
// refuses a row naming two subjects, or none, whatever the store does.
func TestAlbumThread_oneSubjectCheck(t *testing.T) {
	f := newFixture(t)
	album := f.makeAlbum(t, "al_one", "Jedno")
	photo := f.makePhoto(t, "one")

	for name, args := range map[string][]any{
		"album and photo": {"cm_two", album, photo.UID},
		"no subject":      {"cm_none", nil, nil},
	} {
		_, err := f.db.Pool().Exec(context.Background(),
			`INSERT INTO comments (uid, album_uid, photo_uid, body) VALUES ($1, $2, $3, 'x')`, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.ConstraintName != "comments_one_subject" {
			t.Errorf("%s: error = %v, want the comments_one_subject check to refuse it", name, err)
		}
	}
}

// TestAlbumThread_cascadeOnAlbumDelete verifies deleting an album takes its
// thread with it — the foreign key's job, not the application's.
func TestAlbumThread_cascadeOnAlbumDelete(t *testing.T) {
	f := newFixture(t)
	alice := f.makeUser(t, "us_alice", "alice", "Alice A.")
	album := f.makeAlbum(t, "al_gone", "Zmizí")
	photo := f.makePhoto(t, "one")
	ctx := context.Background()

	if _, err := f.comments.Create(ctx, project.BootstrapUID, comments.AlbumSubject(album), alice, "Hele.",
		albumEntry(audit.ActionCommentCreate, alice, album)); err != nil {
		t.Fatalf("Create(album): %v", err)
	}
	kept := f.mustCreate(t, photo.UID, alice, "Zůstane.")

	if _, err := f.db.Pool().Exec(ctx, `DELETE FROM albums WHERE uid = $1`, album); err != nil {
		t.Fatalf("deleting album: %v", err)
	}
	if got := f.rawCount(t, "album_uid IS NOT NULL"); got != 0 {
		t.Fatalf("album comments after the album is gone = %d, want 0", got)
	}
	if _, err := f.comments.Get(ctx, project.BootstrapUID, kept.UID); err != nil {
		t.Fatalf("the photo's comment did not survive: %v", err)
	}
}
