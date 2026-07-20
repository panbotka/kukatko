//go:build integration

package dupmerge_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/dupmerge"
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

// TestMerge_archivedCopyLeavesItsStack covers the regression where merging a
// duplicate group archived a copy that was a stack's primary but left the stack
// itself untouched: the copy's still-live stack sibling kept a stack_uid with no
// primary, and the default listing gate (stack_uid IS NULL OR stack_primary)
// hid it everywhere. The archived copy must leave its stack in the merge's own
// transaction, so the sibling stays visible.
func TestMerge_archivedCopyLeavesItsStack(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ctx := t.Context()

	store := photos.NewStore(db.Pool())
	svc := dupmerge.NewService(db.Pool())

	keeper := seedPhoto(t, store, "dm01")
	copyUID := seedPhoto(t, store, "dm02")
	sibling := seedPhoto(t, store, "dm03")

	// The copy is the primary of a two-member stack that has nothing to do with
	// the duplicate group; merging must not strand its sibling.
	if _, err := store.CreateStack(ctx, copyUID, []string{copyUID, sibling}); err != nil {
		t.Fatalf("CreateStack: %v", err)
	}

	res, err := svc.Merge(ctx, dupmerge.Input{
		KeeperUID:  keeper,
		MemberUIDs: []string{keeper, copyUID},
	})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if res.Archived != 1 {
		t.Fatalf("result = %+v, want one archived copy", res)
	}

	archived, err := store.GetByUID(ctx, copyUID)
	if err != nil {
		t.Fatalf("GetByUID(copy): %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Error("merged copy was not archived")
	}
	if archived.StackUID != nil {
		t.Errorf("archived copy kept stack_uid = %q, want NULL", *archived.StackUID)
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
	seen := map[string]bool{}
	for _, p := range list {
		seen[p.UID] = true
	}
	if !seen[sibling] {
		t.Errorf("sibling %s vanished from the default listing: got %v", sibling, seen)
	}
	if !seen[keeper] {
		t.Errorf("keeper %s missing from the default listing: got %v", keeper, seen)
	}
	if seen[copyUID] {
		t.Errorf("archived copy %s is still listed", copyUID)
	}
}
