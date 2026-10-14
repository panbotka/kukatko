//go:build integration

package comments_test

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestStore_threadsAreScopedByProject writes one comment on each of B's three
// subjects and asserts that, from inside A, none of them is listed, counted or
// returned as the latest, that each is the ErrNotFound a missing comment gets for
// a read, an edit and a delete, that writing on a subject of B is
// ErrSubjectNotFound, and that B's comments are untouched afterwards.
func TestStore_threadsAreScopedByProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := comments.NewStore(db.Pool())
	ctx := t.Context()
	a, b := f.A.ProjectUID, f.B.ProjectUID
	entryB := audit.Entry{ActorUID: f.B.UserUID, Action: audit.ActionCommentCreate}
	entryA := audit.Entry{ActorUID: f.A.UserUID, Action: audit.ActionCommentCreate}

	subjects := []comments.Subject{
		comments.PhotoSubject(f.B.PhotoUID), comments.TaskSubject(f.B.TaskUID), comments.AlbumSubject(f.B.AlbumUID),
	}
	for _, subj := range subjects {
		inB, err := store.Create(ctx, b, subj, f.B.UserUID, "V projektu B", entryB)
		if err != nil {
			t.Fatalf("Create on B's %s in B: %v", subj.Kind, err)
		}

		if list, err := store.List(ctx, a, subj); err != nil || len(list) != 0 {
			t.Errorf("List(A, B's %s) = %d comments, %v; want none", subj.Kind, len(list), err)
		}
		if counts, err := store.CountsAmong(ctx, a, subj.Kind, []string{subj.UID}); err != nil || len(counts) != 0 {
			t.Errorf("CountsAmong(A, B's %s) = %v, %v; want empty", subj.Kind, counts, err)
		}
		if latest, err := store.LatestAmong(ctx, a, subj.Kind, []string{subj.UID}); err != nil || len(latest) != 0 {
			t.Errorf("LatestAmong(A, B's %s) = %v, %v; want empty", subj.Kind, latest, err)
		}
		if _, err := store.Get(ctx, a, inB.UID); !errors.Is(err, comments.ErrNotFound) {
			t.Errorf("Get(A, B's comment on %s) = %v, want ErrNotFound", subj.Kind, err)
		}
		if _, err := store.Update(ctx, a, inB.UID, "Uneseno", entryA); !errors.Is(err, comments.ErrNotFound) {
			t.Errorf("Update(A, B's comment on %s) = %v, want ErrNotFound", subj.Kind, err)
		}
		if err := store.Delete(ctx, a, inB.UID, entryA); !errors.Is(err, comments.ErrNotFound) {
			t.Errorf("Delete(A, B's comment on %s) = %v, want ErrNotFound", subj.Kind, err)
		}
		if _, err := store.Create(ctx, a, subj, f.A.UserUID, "Přes hranici", entryA); !errors.Is(err,
			comments.ErrSubjectNotFound) {
			t.Errorf("Create(A, B's %s) = %v, want ErrSubjectNotFound", subj.Kind, err)
		}

		survived, err := store.List(ctx, b, subj)
		if err != nil || len(survived) != 1 || survived[0].Body != "V projektu B" {
			t.Errorf("B's thread on %s after A's attempts = %+v, %v; want the one untouched comment",
				subj.Kind, survived, err)
		}
	}
}

// TestMentions_onlyApprovedMembersOfTheProject mentions, in a comment of B, the
// writer (a member of B), A's member — a system administrator who belongs to A
// only — and a uid with no account, and asserts only the writer is resolved and
// stored: somebody outside the project is dropped exactly like nobody, so the
// comment neither shows B their name nor announces anything to them.
func TestMentions_onlyApprovedMembersOfTheProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool(), projecttest.AsMaintainer(projecttest.SideA))
	store := comments.NewStore(db.Pool())
	ctx := t.Context()
	missing := projecttest.MissingUID("us")
	body := "@[Já](user:" + f.B.UserUID + ") @[Správce](user:" + f.A.UserUID + ") @[Nikdo](user:" + missing + ")"

	created, err := store.Create(ctx, f.B.ProjectUID, comments.PhotoSubject(f.B.PhotoUID), f.B.UserUID, body,
		audit.Entry{ActorUID: f.B.UserUID, Action: audit.ActionCommentCreate})
	if err != nil {
		t.Fatalf("Create in B: %v", err)
	}
	if len(created.Mentions) != 1 || created.Mentions[0].UserUID != f.B.UserUID {
		t.Errorf("mentions of B's comment = %+v, want only B's own member", created.Mentions)
	}
	rows, err := db.Pool().Query(ctx, `SELECT user_uid FROM comment_mentions WHERE comment_uid = $1`, created.UID)
	if err != nil {
		t.Fatalf("reading comment_mentions: %v", err)
	}
	stored, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scanning comment_mentions: %v", err)
	}
	if len(stored) != 1 || stored[0] != f.B.UserUID {
		t.Errorf("comment_mentions of B's comment = %v, want only %s", stored, f.B.UserUID)
	}
}
