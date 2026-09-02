//go:build integration

package photos_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestBlurhashRoundTrip verifies a stored placeholder comes back on the photo and
// that clearing it restores the "not computed yet" state the backfill looks for.
func TestBlurhashRoundTrip(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("bh01"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Blurhash != "" {
		t.Errorf("a fresh photo has blurhash %q, want none", created.Blurhash)
	}

	const hash = "LEHV6nWB2yk8pyo0adR*.7kCMdnj"
	if err := store.SaveBlurhash(ctx, created.UID, hash); err != nil {
		t.Fatalf("SaveBlurhash: %v", err)
	}
	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Blurhash != hash {
		t.Errorf("blurhash = %q, want %q", got.Blurhash, hash)
	}

	// Clearing it must put the photo back among the backfill's candidates rather
	// than leave an empty string behind, which the column's CHECK forbids anyway.
	if err := store.SaveBlurhash(ctx, created.UID, ""); err != nil {
		t.Fatalf("SaveBlurhash(clear): %v", err)
	}
	cleared, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID after clear: %v", err)
	}
	if cleared.Blurhash != "" {
		t.Errorf("blurhash after clear = %q, want empty", cleared.Blurhash)
	}
	pending, err := store.ListPhotosMissingBlurhash(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingBlurhash: %v", err)
	}
	if !slices.Contains(pending, created.UID) {
		t.Errorf("cleared photo %s is not pending again (%v)", created.UID, pending)
	}
}

// TestBlurhashSurvivesAMetadataUpdate guards the read path: the column sits at the
// end of the canonical column list, so a scan that drifted out of step would show
// up as a placeholder that vanishes the moment anything else about the photo is
// written.
func TestBlurhashSurvivesAMetadataUpdate(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("bh02"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	const hash = "LKO2?U%2Tw=w]~RBVZRi};RPxuwH"
	if err := store.SaveBlurhash(ctx, created.UID, hash); err != nil {
		t.Fatalf("SaveBlurhash: %v", err)
	}

	updated, err := store.UpdateMetadata(ctx, created.UID, photos.MetadataUpdate{Title: "Renamed"})
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if updated.Blurhash != hash {
		t.Errorf("blurhash after update = %q, want %q", updated.Blurhash, hash)
	}
}

// TestSaveBlurhashUnknownPhoto verifies an unknown uid is reported rather than
// silently updating nothing.
func TestSaveBlurhashUnknownPhoto(t *testing.T) {
	store, _ := newStore(t)
	if err := store.SaveBlurhash(t.Context(), "ptnope", "LEHV6nWB2yk8pyo0adR*"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("SaveBlurhash err = %v, want photos.ErrPhotoNotFound", err)
	}
}

// TestListPhotosMissingBlurhash verifies the backfill's predicate: photos with a
// placeholder and archived photos are out, everything else is in, and the count
// answers the same question as the listing.
func TestListPhotosMissingBlurhash(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	pending, err := store.Create(ctx, samplePhoto("bh10"))
	if err != nil {
		t.Fatalf("Create pending: %v", err)
	}
	done, err := store.Create(ctx, samplePhoto("bh11"))
	if err != nil {
		t.Fatalf("Create done: %v", err)
	}
	if err := store.SaveBlurhash(ctx, done.UID, "LEHV6nWB2yk8pyo0adR*.7kCMdnj"); err != nil {
		t.Fatalf("SaveBlurhash: %v", err)
	}
	archived, err := store.Create(ctx, samplePhoto("bh12"))
	if err != nil {
		t.Fatalf("Create archived: %v", err)
	}
	if _, err := store.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	uids, err := store.ListPhotosMissingBlurhash(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingBlurhash: %v", err)
	}
	if len(uids) != 1 || uids[0] != pending.UID {
		t.Errorf("pending uids = %v, want [%s]", uids, pending.UID)
	}

	count, err := store.CountPhotosMissingBlurhash(ctx)
	if err != nil {
		t.Fatalf("CountPhotosMissingBlurhash: %v", err)
	}
	if count != len(uids) {
		t.Errorf("count = %d, want %d (what the listing returns)", count, len(uids))
	}

	limited, err := store.ListPhotosMissingBlurhash(ctx, 1)
	if err != nil {
		t.Fatalf("ListPhotosMissingBlurhash(limit): %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limited listing returned %d uids, want 1", len(limited))
	}
}

// TestBlurhashRejectsTheEmptyString verifies the column's CHECK: "not computed
// yet" is NULL and only NULL, so nothing downstream has to treat an empty string
// as a third state.
func TestBlurhashRejectsTheEmptyString(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("bh20"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = db.Pool().Exec(ctx, "UPDATE photos SET blurhash = '' WHERE uid = $1", created.UID)
	if err == nil {
		t.Error("writing an empty blurhash was accepted, want the CHECK to reject it")
	}
}
