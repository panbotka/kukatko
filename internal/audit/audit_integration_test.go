//go:build integration

package audit_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
)

// TestStore_RecordAndList writes audit entries and reads them back newest-first,
// confirming the details JSONB round-trips. ActorUID is left empty (stored NULL)
// so the test needs no seeded user.
func TestStore_RecordAndList(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := audit.NewStore(db.Pool())
	ctx := t.Context()

	first := audit.Entry{Action: audit.ActionPhotosBulk, TargetType: "photos",
		Details: map[string]any{"updated": float64(2)}, IP: "203.0.113.1", UserAgent: "agent/1"}
	second := audit.Entry{Action: "test.action", TargetType: "photos",
		Details: map[string]any{"note": "hi"}}
	for _, entry := range []audit.Entry{first, second} {
		if err := store.Record(ctx, entry); err != nil {
			t.Fatalf("Record(%s): %v", entry.Action, err)
		}
	}

	records, err := store.List(ctx, audit.Filter{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("List returned %d records, want 2", len(records))
	}
	// Newest first: the second write is returned first.
	if records[0].Action != "test.action" {
		t.Errorf("records[0].Action = %q, want test.action", records[0].Action)
	}
	if records[0].ActorUID != nil {
		t.Errorf("records[0].ActorUID = %v, want nil", records[0].ActorUID)
	}
	if records[1].Action != audit.ActionPhotosBulk {
		t.Errorf("records[1].Action = %q, want %s", records[1].Action, audit.ActionPhotosBulk)
	}
	if records[1].Details["updated"] != float64(2) {
		t.Errorf("records[1].Details[updated] = %v, want 2", records[1].Details["updated"])
	}
	if records[1].IP == nil || *records[1].IP != "203.0.113.1" {
		t.Errorf("records[1].IP = %v, want 203.0.113.1", records[1].IP)
	}
	if records[1].UserAgent == nil || *records[1].UserAgent != "agent/1" {
		t.Errorf("records[1].UserAgent = %v, want agent/1", records[1].UserAgent)
	}
}

// TestWrite_rollsBackWithTransaction proves the core durability guarantee: an
// audit row written through a caller's transaction commits with it on success
// and vanishes with it on rollback, so there is never an orphan audit entry nor
// a missing one. It exercises Write directly against an open pgx.Tx, the same
// mechanism every audited mutation path uses.
func TestWrite_rollsBackWithTransaction(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := audit.NewStore(db.Pool())
	ctx := t.Context()

	// A transaction that rolls back must leave no audit row behind.
	withTx(t, db, func(tx pgx.Tx) {
		if err := audit.Write(ctx, tx, audit.Entry{Action: "rollme", TargetType: "photos"}); err != nil {
			t.Fatalf("Write(rollme): %v", err)
		}
	}, false)

	// A committed transaction must persist its audit row.
	withTx(t, db, func(tx pgx.Tx) {
		if err := audit.Write(ctx, tx, audit.Entry{Action: "keepme", TargetType: "photos"}); err != nil {
			t.Fatalf("Write(keepme): %v", err)
		}
	}, true)

	records, err := store.List(ctx, audit.Filter{Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("List returned %d records, want 1 (only the committed entry)", len(records))
	}
	if records[0].Action != "keepme" {
		t.Errorf("surviving entry action = %q, want keepme", records[0].Action)
	}
}

// withTx runs fn inside a transaction on db's pool and then commits when commit
// is true or rolls back otherwise, so a test can assert what survives.
func withTx(t *testing.T, db *database.DB, fn func(tx pgx.Tx), commit bool) {
	t.Helper()
	ctx := t.Context()
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	fn(tx)
	if commit {
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		return
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}

// TestStore_ListFilters verifies the filter and pagination behaviour: filtering
// by action, by entity (type+uid), by date range, and clamped paging with Count.
func TestStore_ListFilters(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := audit.NewStore(db.Pool())
	ctx := t.Context()

	entries := []audit.Entry{
		{Action: audit.ActionPhotoArchive, TargetType: "photos", TargetUID: "ph-1"},
		{Action: audit.ActionPhotoUpdate, TargetType: "photos", TargetUID: "ph-1"},
		{Action: audit.ActionAlbumCreate, TargetType: "albums", TargetUID: "al-1"},
	}
	for _, e := range entries {
		if err := store.Record(ctx, e); err != nil {
			t.Fatalf("Record(%s): %v", e.Action, err)
		}
	}

	byAction, err := store.List(ctx, audit.Filter{Action: audit.ActionAlbumCreate})
	if err != nil {
		t.Fatalf("List(action): %v", err)
	}
	if len(byAction) != 1 || byAction[0].Action != audit.ActionAlbumCreate {
		t.Errorf("action filter returned %d entries, want 1 album.create", len(byAction))
	}

	byEntity, err := store.List(ctx, audit.Filter{TargetType: "photos", TargetUID: "ph-1"})
	if err != nil {
		t.Fatalf("List(entity): %v", err)
	}
	if len(byEntity) != 2 {
		t.Errorf("entity filter returned %d entries, want 2", len(byEntity))
	}

	count, err := store.Count(ctx, audit.Filter{TargetType: "photos"})
	if err != nil {
		t.Fatalf("Count(photos): %v", err)
	}
	if count != 2 {
		t.Errorf("Count(photos) = %d, want 2", count)
	}

	// A future "since" excludes everything written above.
	future := time.Now().Add(time.Hour)
	sinceFuture, err := store.List(ctx, audit.Filter{Since: &future})
	if err != nil {
		t.Fatalf("List(since): %v", err)
	}
	if len(sinceFuture) != 0 {
		t.Errorf("since-future filter returned %d entries, want 0", len(sinceFuture))
	}

	// Pagination: limit 1 returns one row, offset walks the newest-first list.
	page, err := store.List(ctx, audit.Filter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("List(page): %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("paged list returned %d entries, want 1", len(page))
	}
	// Newest-first ordering: index 1 is the album.update (second written).
	if page[0].Action != audit.ActionPhotoUpdate {
		t.Errorf("page[0].Action = %q, want %s", page[0].Action, audit.ActionPhotoUpdate)
	}
}
