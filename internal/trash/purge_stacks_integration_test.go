//go:build integration

package trash_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestPurgeExpired_repairsStack covers the regression where the retention purge
// deleted an archived stack member but left its still-live sibling carrying a
// stack_uid whose primary no longer existed. The default listing gate is
// (stack_uid IS NULL OR stack_primary), so that sibling disappeared from every
// view permanently — it could not even be re-stacked, because the stack-candidate
// query excludes rows that already carry a stack_uid.
func TestPurgeExpired_repairsStack(t *testing.T) {
	env := newPurgeEnv(t)
	ctx := t.Context()
	expired := time.Now().Add(-72 * time.Hour)

	// Build the stack while both members are live (stacking rejects archived
	// rows), then archive the primary long enough ago to be past retention.
	primary, _, _ := env.seedPhoto(t, "stack-primary", nil)
	sibling, _, _ := env.seedPhoto(t, "stack-sibling", nil)
	if _, err := env.store.CreateStack(ctx, primary.UID, []string{primary.UID, sibling.UID}); err != nil {
		t.Fatalf("CreateStack: %v", err)
	}
	if _, err := env.db.Pool().Exec(ctx,
		"UPDATE photos SET archived_at = $2 WHERE uid = $1", primary.UID, expired); err != nil {
		t.Fatalf("stamping archived_at: %v", err)
	}

	res, err := env.svc.PurgeExpired(ctx)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:1 Failed:0}", res)
	}

	survivor, err := env.store.GetByUID(ctx, sibling.UID)
	if err != nil {
		t.Fatalf("sibling gone after purge: %v", err)
	}
	if survivor.StackUID != nil {
		t.Errorf("sibling still carries stack_uid = %q, want NULL", *survivor.StackUID)
	}
	if survivor.StackPrimary {
		t.Error("dissolved sibling is still flagged stack_primary")
	}

	list, err := env.store.List(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].UID != sibling.UID {
		t.Errorf("default listing = %+v, want only the live sibling %s", list, sibling.UID)
	}
	candidates, err := env.store.ListStackCandidates(ctx)
	if err != nil {
		t.Fatalf("ListStackCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].UID != sibling.UID {
		t.Errorf("sibling is not re-stackable: candidates = %+v", candidates)
	}
}
