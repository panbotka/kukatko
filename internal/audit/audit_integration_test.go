//go:build integration

package audit_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
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
		Details: map[string]any{"updated": float64(2)}}
	second := audit.Entry{Action: "test.action", TargetType: "photos",
		Details: map[string]any{"note": "hi"}}
	for _, entry := range []audit.Entry{first, second} {
		if err := store.Record(ctx, entry); err != nil {
			t.Fatalf("Record(%s): %v", entry.Action, err)
		}
	}

	records, err := store.List(ctx, 10, 0)
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
}
