//go:build integration

package bulk_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/bulk"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They truncate the database, so they do not run
// in parallel.

// seedPhoto creates a minimal photo with a distinct file hash and returns its UID.
func seedPhoto(t *testing.T, store *photos.Store, hash string) string {
	t.Helper()
	taken := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	created, err := store.Create(t.Context(), photos.Photo{
		FileHash:   hash,
		FilePath:   "2023/06/" + hash + ".jpg",
		FileName:   hash + ".jpg",
		FileSize:   1234,
		FileMime:   "image/jpeg",
		FileWidth:  4000,
		FileHeight: 3000,
		TakenAt:    &taken,
		Exif:       json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create %s: %v", hash, err)
	}
	return created.UID
}

// TestApply_archiveRepairsStack covers the bulk archive path: archiving a
// stack's primary in a batch must take it out of the stack and re-elect (here:
// dissolve, the stack drops to one member) so the still-live sibling is not
// hidden by the (stack_uid IS NULL OR stack_primary) listing gate.
func TestApply_archiveRepairsStack(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()

	store := photos.NewStore(db.Pool())
	svc := bulk.NewService(db.Pool(), 0)

	primary := seedPhoto(t, store, "bk01")
	sibling := seedPhoto(t, store, "bk02")
	if _, err := store.CreateStack(ctx, primary, []string{primary, sibling}); err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	archive := true
	res, err := svc.Apply(ctx, "", []string{primary}, bulk.Operations{Archive: &archive})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if res.Counts.Total != 1 {
		t.Fatalf("result = %+v, want one updated photo", res)
	}

	survivor, err := store.GetByUID(ctx, sibling)
	if err != nil {
		t.Fatalf("GetByUID(sibling): %v", err)
	}
	if survivor.StackUID != nil {
		t.Errorf("sibling still carries stack_uid = %q, want NULL", *survivor.StackUID)
	}

	list, err := store.List(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].UID != sibling {
		t.Errorf("default listing = %+v, want only the live sibling %s", list, sibling)
	}
}

// TestApply_unarchiveLeavesStacksAlone guards the other direction: an unarchive
// batch must not touch stack membership at all.
func TestApply_unarchiveLeavesStacksAlone(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()

	store := photos.NewStore(db.Pool())
	svc := bulk.NewService(db.Pool(), 0)

	primary := seedPhoto(t, store, "bk03")
	sibling := seedPhoto(t, store, "bk04")
	if _, err := store.CreateStack(ctx, primary, []string{primary, sibling}); err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	archive := false
	if _, err := svc.Apply(ctx, "", []string{primary}, bulk.Operations{Archive: &archive}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	stacked, err := store.GetByUID(ctx, primary)
	if err != nil {
		t.Fatalf("GetByUID(primary): %v", err)
	}
	if stacked.StackUID == nil || !stacked.StackPrimary {
		t.Errorf("unarchive disturbed the stack: %+v", stacked)
	}
}
