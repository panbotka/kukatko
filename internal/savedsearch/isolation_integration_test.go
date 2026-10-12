//go:build integration

package savedsearch_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/projecttest"
	"github.com/panbotka/kukatko/internal/savedsearch"
)

// uidsOf returns the uids of searches.
func uidsOf(searches []savedsearch.SavedSearch) []string {
	out := make([]string, 0, len(searches))
	for _, s := range searches {
		out = append(out, s.UID)
	}
	return out
}

// TestStore_listIsScopedByProject asserts each project's member lists only the
// saved search seeded in that project.
func TestStore_listIsScopedByProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := savedsearch.NewStore(db.Pool())

	f.AssertStoreScoped(t, func(projectUID string) ([]string, error) {
		owner := f.A.UserUID
		if projectUID == f.B.ProjectUID {
			owner = f.B.UserUID
		}
		list, err := store.List(t.Context(), projectUID, owner)
		return uidsOf(list), err
	})
}

// TestStore_sameUserOtherProjectIsMissing gives A's member a saved search in
// project B as well and asserts that, from project A, it is the very ErrNotFound
// a missing uid gets for every read and write, that it is never listed there, and
// that every refused write left it untouched.
func TestStore_sameUserOtherProjectIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := savedsearch.NewStore(db.Pool())
	ctx := t.Context()
	user := f.A.UserUID

	inB, err := store.Create(ctx, f.B.ProjectUID, user, "Jen v B", json.RawMessage(`{"q":"b"}`))
	if err != nil {
		t.Fatalf("Create in B: %v", err)
	}
	if inB.ProjectUID != f.B.ProjectUID {
		t.Fatalf("created search project = %s, want %s", inB.ProjectUID, f.B.ProjectUID)
	}

	listA, err := store.List(ctx, f.A.ProjectUID, user)
	if err != nil {
		t.Fatalf("List in A: %v", err)
	}
	for _, s := range listA {
		if s.UID == inB.UID {
			t.Fatalf("List in A returned B's search %s", inB.UID)
		}
	}
	if len(listA) != 1 || listA[0].UID != f.A.SavedSearchUID {
		t.Fatalf("List in A = %v, want only %s", uidsOf(listA), f.A.SavedSearchUID)
	}

	if _, err := store.Get(ctx, f.A.ProjectUID, user, inB.UID); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Get from A = %v, want ErrNotFound", err)
	}
	if _, err := store.Update(ctx, f.A.ProjectUID, user, inB.UID, "Uneseno", nil); !errors.Is(err,
		savedsearch.ErrNotFound) {
		t.Errorf("Update from A = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, f.A.ProjectUID, user, inB.UID); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Delete from A = %v, want ErrNotFound", err)
	}

	survived, err := store.Get(ctx, f.B.ProjectUID, user, inB.UID)
	if err != nil {
		t.Fatalf("Get in B after the refused writes: %v", err)
	}
	if survived.Name != "Jen v B" || !survived.UpdatedAt.Equal(inB.UpdatedAt) {
		t.Fatalf("B's search changed under A's writes: %+v", survived)
	}
}

// TestStore_otherOwnerSameProjectIsMissing asserts that inside one project a
// saved search of another member is ErrNotFound for every read and write.
func TestStore_otherOwnerSameProjectIsMissing(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := savedsearch.NewStore(db.Pool())
	ctx := t.Context()
	// B's member asks inside project A for A's member's search.
	intruder, target := f.B.UserUID, f.A.SavedSearchUID

	if _, err := store.Get(ctx, f.A.ProjectUID, intruder, target); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
	if _, err := store.Update(ctx, f.A.ProjectUID, intruder, target, "x", nil); !errors.Is(err,
		savedsearch.ErrNotFound) {
		t.Errorf("Update = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, f.A.ProjectUID, intruder, target); !errors.Is(err, savedsearch.ErrNotFound) {
		t.Errorf("Delete = %v, want ErrNotFound", err)
	}
	if _, err := store.Get(ctx, f.A.ProjectUID, f.A.UserUID, target); err != nil {
		t.Fatalf("A's own search after the refused writes: %v", err)
	}
}

// TestStore_noProject asserts every method refuses an empty project with
// ErrNoProject instead of reading or writing the whole instance.
func TestStore_noProject(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	f := projecttest.Seed(t, db.Pool())
	store := savedsearch.NewStore(db.Pool())
	ctx := t.Context()
	user, uid := f.A.UserUID, f.A.SavedSearchUID

	if _, err := store.Create(ctx, "", user, "x", nil); !errors.Is(err, savedsearch.ErrNoProject) {
		t.Errorf("Create = %v, want ErrNoProject", err)
	}
	if _, err := store.List(ctx, "", user); !errors.Is(err, savedsearch.ErrNoProject) {
		t.Errorf("List = %v, want ErrNoProject", err)
	}
	if _, err := store.Get(ctx, "", user, uid); !errors.Is(err, savedsearch.ErrNoProject) {
		t.Errorf("Get = %v, want ErrNoProject", err)
	}
	if _, err := store.Update(ctx, "", user, uid, "x", nil); !errors.Is(err, savedsearch.ErrNoProject) {
		t.Errorf("Update = %v, want ErrNoProject", err)
	}
	if err := store.Delete(ctx, "", user, uid); !errors.Is(err, savedsearch.ErrNoProject) {
		t.Errorf("Delete = %v, want ErrNoProject", err)
	}
}
