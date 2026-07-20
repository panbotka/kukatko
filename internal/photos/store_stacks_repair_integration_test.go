//go:build integration

package photos_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/photos"
)

// makeStack creates two photos, stacks them with the first as the primary and
// returns (primaryUID, otherUID, stackUID). It is the fixture for the repair
// tests: the smallest stack that can lose a member.
func makeStack(t *testing.T, store *photos.Store, ctx context.Context, hashes [2]string) (string, string, string) {
	t.Helper()
	primary, err := store.Create(ctx, samplePhoto(hashes[0]))
	if err != nil {
		t.Fatalf("create %s: %v", hashes[0], err)
	}
	other, err := store.Create(ctx, samplePhoto(hashes[1]))
	if err != nil {
		t.Fatalf("create %s: %v", hashes[1], err)
	}
	stackUID, err := store.CreateStack(ctx, primary.UID, []string{primary.UID, other.UID})
	if err != nil {
		t.Fatalf("CreateStack: %v", err)
	}
	return primary.UID, other.UID, stackUID
}

// listedUIDs returns the uids the default (live, stack-collapsed) listing shows.
func listedUIDs(t *testing.T, store *photos.Store, ctx context.Context) []string {
	t.Helper()
	list, err := store.List(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	uids := make([]string, 0, len(list))
	for _, p := range list {
		uids = append(uids, p.UID)
	}
	return uids
}

// mustGet reads a photo by uid, failing the test when it is gone.
func mustGet(t *testing.T, store *photos.Store, ctx context.Context, uid string) photos.Photo {
	t.Helper()
	p, err := store.GetByUID(ctx, uid)
	if err != nil {
		t.Fatalf("Get %s: %v", uid, err)
	}
	return p
}

// TestArchiveRepairsStack covers the regression where archiving a stack's
// primary left the remaining live member with a stack_uid but no primary, so the
// (stack_uid IS NULL OR stack_primary) visibility gate hid it from every default
// view. The survivor of a two-member stack must come out standalone and visible.
func TestArchiveRepairsStack(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stk1", "stk2"})

	if _, err := store.Archive(ctx, primaryUID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	survivor := mustGet(t, store, ctx, otherUID)
	if survivor.StackUID != nil {
		t.Errorf("survivor still stacked: stack_uid = %q, want NULL", *survivor.StackUID)
	}
	if survivor.StackPrimary {
		t.Error("dissolved survivor is still flagged stack_primary")
	}
	archived := mustGet(t, store, ctx, primaryUID)
	if archived.StackUID != nil {
		t.Errorf("archived photo kept stack_uid = %q, want NULL", *archived.StackUID)
	}

	if got := listedUIDs(t, store, ctx); !slices.Equal(got, []string{otherUID}) {
		t.Errorf("default listing = %v, want only the live survivor %v", got, []string{otherUID})
	}
}

// TestArchiveReelectsPrimary checks the three-member case: archiving the primary
// must re-elect one of the two remaining members rather than dissolve the stack.
func TestArchiveReelectsPrimary(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stk3", "stk4"})
	third, err := store.Create(ctx, samplePhoto("stk5"))
	if err != nil {
		t.Fatalf("create third: %v", err)
	}
	if _, err := store.CreateStack(ctx, primaryUID, []string{primaryUID, otherUID, third.UID}); err != nil {
		t.Fatalf("CreateStack (3 members): %v", err)
	}

	if _, err := store.Archive(ctx, primaryUID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	listed := listedUIDs(t, store, ctx)
	if len(listed) != 1 {
		t.Fatalf("default listing = %v, want exactly the re-elected primary", listed)
	}
	if listed[0] != otherUID && listed[0] != third.UID {
		t.Errorf("listing shows %q, want one of the two surviving members", listed[0])
	}
	elected := mustGet(t, store, ctx, listed[0])
	if elected.StackUID == nil || !elected.StackPrimary {
		t.Errorf("re-elected member is not a stack primary: %+v", elected)
	}
}

// TestArchiveAuditedRepairsStack covers the same repair on the audited path used
// by the HTTP API.
func TestArchiveAuditedRepairsStack(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stk6", "stk7"})

	entry := audit.Entry{Action: audit.ActionPhotoArchive, TargetType: "photos"}
	if _, err := store.ArchiveAudited(ctx, primaryUID, entry); err != nil {
		t.Fatalf("ArchiveAudited: %v", err)
	}

	if survivor := mustGet(t, store, ctx, otherUID); survivor.StackUID != nil {
		t.Errorf("survivor still stacked: %q", *survivor.StackUID)
	}
	if got := listedUIDs(t, store, ctx); !slices.Equal(got, []string{otherUID}) {
		t.Errorf("default listing = %v, want %v", got, []string{otherUID})
	}
}

// TestDeleteAuditedRepairsStack covers the purge path: the remnant must become a
// plain standalone photo — visible, and re-stackable (i.e. back in the candidate
// set, which excludes rows carrying a stack_uid).
func TestDeleteAuditedRepairsStack(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stk8", "stk9"})

	entry := audit.Entry{Action: audit.ActionPhotoPurge, TargetType: "photos"}
	if err := store.DeleteAudited(ctx, primaryUID, entry); err != nil {
		t.Fatalf("DeleteAudited: %v", err)
	}

	if _, err := store.GetByUID(ctx, primaryUID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("purged photo still readable: err = %v", err)
	}
	survivor := mustGet(t, store, ctx, otherUID)
	if survivor.StackUID != nil {
		t.Errorf("remnant still carries stack_uid = %q", *survivor.StackUID)
	}
	if got := listedUIDs(t, store, ctx); !slices.Equal(got, []string{otherUID}) {
		t.Errorf("default listing = %v, want %v", got, []string{otherUID})
	}
	candidates, err := store.ListStackCandidates(ctx)
	if err != nil {
		t.Fatalf("ListStackCandidates: %v", err)
	}
	if len(candidates) != 1 || candidates[0].UID != otherUID {
		t.Errorf("remnant is not re-stackable: candidates = %+v", candidates)
	}
}

// TestDeleteRepairsStack covers the un-audited Delete used by the import
// rollback paths.
func TestDeleteRepairsStack(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stka", "stkb"})

	if err := store.Delete(ctx, primaryUID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if survivor := mustGet(t, store, ctx, otherUID); survivor.StackUID != nil {
		t.Errorf("remnant still carries stack_uid = %q", *survivor.StackUID)
	}
	if err := store.Delete(ctx, "missing"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Delete(missing) = %v, want ErrPhotoNotFound", err)
	}
}

// TestArchiveNonMemberIsUnaffected guards against over-reach: archiving a
// standalone photo must not touch anything, and archiving a non-primary member
// must leave the stack (and its primary) intact.
func TestArchiveNonMemberIsUnaffected(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stkc", "stkd"})
	third, err := store.Create(ctx, samplePhoto("stke"))
	if err != nil {
		t.Fatalf("create third: %v", err)
	}
	if _, err := store.CreateStack(ctx, primaryUID, []string{primaryUID, otherUID, third.UID}); err != nil {
		t.Fatalf("CreateStack (3 members): %v", err)
	}

	if _, err := store.Archive(ctx, otherUID); err != nil {
		t.Fatalf("Archive non-primary: %v", err)
	}

	primary := mustGet(t, store, ctx, primaryUID)
	if primary.StackUID == nil || !primary.StackPrimary {
		t.Errorf("archiving a non-primary disturbed the primary: %+v", primary)
	}
	if got := listedUIDs(t, store, ctx); !slices.Equal(got, []string{primaryUID}) {
		t.Errorf("default listing = %v, want %v", got, []string{primaryUID})
	}
}

// TestUnarchiveLeavesPhotoStandalone documents the chosen semantics: a photo
// restored from the trash comes back visible and standalone; rejoining a stack
// is a deliberate act, not an automatic one.
func TestUnarchiveLeavesPhotoStandalone(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	primaryUID, otherUID, _ := makeStack(t, store, ctx, [2]string{"stkf", "stkg"})

	if _, err := store.Archive(ctx, primaryUID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	restored, err := store.Unarchive(ctx, primaryUID)
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if restored.StackUID != nil {
		t.Errorf("restored photo rejoined a stack: %q", *restored.StackUID)
	}

	got := listedUIDs(t, store, ctx)
	slices.Sort(got)
	want := []string{primaryUID, otherUID}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("default listing = %v, want both photos %v", got, want)
	}
}
