//go:build integration

package people_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestSubjectCover_foreignPhotoIsRefused asserts every write path of a
// subject's hand-picked cover refuses a photo of the other project exactly like
// a photo uid that names nothing — ErrPhotoNotFound, never the foreign-key
// failure that told the two apart — and stores nothing.
func TestSubjectCover_foreignPhotoIsRefused(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	entry := audit.Entry{Action: audit.ActionSubjectUpdate, TargetType: "subject"}

	for _, cover := range []string{f.A.PhotoUID, projecttest.MissingUID("ph")} {
		subj := people.Subject{Name: "Obálka", CoverPhotoUID: &cover}
		upd := people.SubjectUpdate{Name: "Osoba " + f.B.Token, CoverPhotoUID: &cover}
		writes := map[string]func() error{
			"CreateSubject": func() error {
				_, err := store.CreateSubject(ctx, f.B.ProjectUID, subj)
				return err
			},
			"CreateSubjectAudited": func() error {
				_, err := store.CreateSubjectAudited(ctx, f.B.ProjectUID, subj, entry)
				return err
			},
			"CreateSubjectTx": func() error {
				tx, err := db.Pool().Begin(ctx)
				if err != nil {
					return err
				}
				defer func() { _ = tx.Rollback(ctx) }()
				_, err = people.CreateSubjectTx(ctx, tx, f.B.ProjectUID, subj)
				return err
			},
			"UpdateSubject": func() error {
				_, err := store.UpdateSubject(ctx, f.B.ProjectUID, f.B.SubjectUID, upd)
				return err
			},
			"UpdateSubjectAudited": func() error {
				_, err := store.UpdateSubjectAudited(ctx, f.B.ProjectUID, f.B.SubjectUID, upd, entry)
				return err
			},
		}
		for name, write := range writes {
			if err := write(); !errors.Is(err, people.ErrPhotoNotFound) {
				t.Errorf("%s in B with cover %s = %v, want ErrPhotoNotFound", name, cover, err)
			}
		}
	}

	var stored int
	if err := db.Pool().QueryRow(ctx,
		`SELECT count(*) FROM subjects WHERE project_id = $1 AND cover_photo_uid IS NOT NULL`,
		f.B.ProjectUID).Scan(&stored); err != nil || stored != 0 {
		t.Errorf("B's subjects with a cover after the refused writes = %d (%v), want 0", stored, err)
	}
}

// TestSubjectCover_foreignPhotoIsNeverRead plants A's photo as the cover of B's
// subject behind the store's back — a row written before the write was checked
// — and asserts neither the people covers (which global search turns into a
// thumbnail URL) nor the avatar ever answer with it.
func TestSubjectCover_foreignPhotoIsNeverRead(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()
	if _, err := db.Pool().Exec(ctx, `UPDATE subjects SET cover_photo_uid = $1 WHERE uid = $2`,
		f.A.PhotoUID, f.B.SubjectUID); err != nil {
		t.Fatalf("planting A's photo as B's cover: %v", err)
	}

	covers, err := store.SubjectCovers(ctx, f.B.ProjectUID, []string{f.B.SubjectUID})
	if err != nil {
		t.Fatalf("SubjectCovers: %v", err)
	}
	if got := covers[f.B.SubjectUID]; got.PhotoUID == f.A.PhotoUID || got.FileHash == f.A.PhotoFileHash {
		t.Errorf("B's subject cover = %+v, want anything but A's photo", got)
	}
	avatar, err := store.SubjectAvatar(ctx, f.B.ProjectUID, f.B.SubjectUID)
	if err != nil && !errors.Is(err, people.ErrNoAvatar) {
		t.Fatalf("SubjectAvatar: %v", err)
	}
	if avatar.PhotoUID == f.A.PhotoUID {
		t.Errorf("B's subject avatar is cut from A's photo: %+v", avatar)
	}
}

// TestRestoreSubject_dropsAForeignCover restores, into B, a hand-supplied undo
// snapshot whose cover is A's photo, and one whose cover names nothing: both
// subjects come back, without the cover — the restore keeps what it can, and
// another project's photo is not something it can keep.
func TestRestoreSubject_dropsAForeignCover(t *testing.T) {
	store, _, _, db := newStores(t)
	f := projecttest.Seed(t, db.Pool())
	ctx := t.Context()

	for _, cover := range []string{f.A.PhotoUID, projecttest.MissingUID("ph")} {
		snap := people.SubjectSnapshot{Subject: people.Subject{
			UID: projecttest.MissingUID("su"), Slug: "obnoveny", Name: "Obnovený", Type: people.SubjectPerson,
			CoverPhotoUID: &cover,
		}}
		restored, err := store.RestoreSubject(ctx, f.B.ProjectUID, snap,
			audit.Entry{Action: audit.ActionSubjectCreate, TargetType: "subject"})
		if err != nil {
			t.Fatalf("RestoreSubject with cover %s: %v", cover, err)
		}
		if restored.CoverPhotoUID != nil {
			t.Errorf("restored subject's cover = %s, want none", *restored.CoverPhotoUID)
		}
	}
}
