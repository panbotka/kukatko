//go:build integration

package people_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/people"
)

// TestCreateSubjectTx_joinsTheCallersTransaction proves the two halves of what
// CreateSubjectTx promises another package: the subject lands in the caller's
// transaction — so rolling that transaction back takes the subject with it — and
// a slug collision still resolves, even though the colliding insert happens
// inside a transaction the caller needs to survive.
func TestCreateSubjectTx_joinsTheCallersTransaction(t *testing.T) {
	store, _, _, db := newStores(t)
	ctx := t.Context()

	if _, err := store.CreateSubject(ctx, people.Subject{Name: "Marie Nečasová"}); err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}

	// A committed transaction: the namesake gets a suffixed slug of her own.
	var created people.Subject
	inTx(t, db.Pool(), func(tx pgx.Tx) error {
		var err error
		created, err = people.CreateSubjectTx(ctx, tx, people.Subject{Name: "Marie Nečasová"})
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
	if created.Slug != "marie-necasova-2" {
		t.Errorf("slug = %q, want the collision resolved to marie-necasova-2", created.Slug)
	}

	// A rolled-back transaction: nothing survives it.
	var rolledBack people.Subject
	inTx(t, db.Pool(), func(tx pgx.Tx) error {
		var err error
		rolledBack, err = people.CreateSubjectTx(ctx, tx, people.Subject{Name: "Marie Nečasová"})
		return err
	})
	if _, err := store.GetSubjectByUID(ctx, rolledBack.UID); err == nil {
		t.Error("a subject created in a rolled-back transaction survived it")
	}

	subjects, err := store.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(subjects) != 2 {
		t.Errorf("subjects = %d, want 2 (the seed and the committed namesake)", len(subjects))
	}
}

// inTx runs body in a fresh transaction and rolls it back afterwards, which is a
// no-op when body committed it.
func inTx(t *testing.T, pool *pgxpool.Pool, body func(tx pgx.Tx) error) {
	t.Helper()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	if err := body(tx); err != nil {
		t.Fatalf("transaction body: %v", err)
	}
}
