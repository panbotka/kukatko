//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They cover the sidecar export bookkeeping added by
// 0035_photos_sidecar_written: the pending predicate the backfill converges on,
// and the marker that clears it.

// TestListPhotosMissingSidecar_neverExported asserts a fresh photo is pending: it
// has never had a sidecar, which is the whole of a first backfill's workload.
func TestListPhotosMissingSidecar_neverExported(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("sidecarpending1"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	uids, err := store.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	if !contains(uids, created.UID) {
		t.Errorf("pending = %v, want it to contain the never-exported photo %s", uids, created.UID)
	}
}

// TestMarkSidecarWritten_clearsPending is the property the backfill's convergence
// rests on: once the file is written the photo stops being scheduled, so a re-run
// over a drained library enqueues nothing.
func TestMarkSidecarWritten_clearsPending(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("sidecarmark1"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := store.MarkSidecarWritten(ctx, created.UID); err != nil {
		t.Fatalf("MarkSidecarWritten returned error: %v", err)
	}
	uids, err := store.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	if contains(uids, created.UID) {
		t.Errorf("photo %s is still pending after its sidecar was written; the backfill would never drain",
			created.UID)
	}
}

// TestMarkSidecarWritten_doesNotBumpUpdatedAt pins the rule that makes the
// staleness predicate work at all. Stamping the marker records the export, not an
// edit of the photo — if it bumped updated_at, every write would mark its own
// photo stale again and the backfill would loop forever.
func TestMarkSidecarWritten_doesNotBumpUpdatedAt(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("sidecarnobump1"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	// The marker's now() must land strictly after the row's updated_at, or the
	// comparison proves nothing.
	time.Sleep(10 * time.Millisecond)
	if err := store.MarkSidecarWritten(ctx, created.UID); err != nil {
		t.Fatalf("MarkSidecarWritten returned error: %v", err)
	}
	after, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID returned error: %v", err)
	}
	if !after.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("updated_at moved from %v to %v; stamping the sidecar marker must not count as an edit",
			created.UpdatedAt, after.UpdatedAt)
	}
	uids, err := store.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	if contains(uids, created.UID) {
		t.Error("the photo re-entered the pending set immediately after its own export")
	}
}

// TestListPhotosMissingSidecar_staleAfterEdit asserts an edited photo becomes
// pending again. It is the safety net that recovers an enqueue lost to a crash
// between the commit and the enqueue.
func TestListPhotosMissingSidecar_staleAfterEdit(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("sidecarstale1"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if err := store.MarkSidecarWritten(ctx, created.UID); err != nil {
		t.Fatalf("MarkSidecarWritten returned error: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	if _, err := store.UpdateMetadata(ctx, created.UID, photos.MetadataUpdate{Title: "Nový titulek"}); err != nil {
		t.Fatalf("UpdateMetadata returned error: %v", err)
	}
	uids, err := store.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	if !contains(uids, created.UID) {
		t.Errorf("pending = %v, want the edited photo %s to be stale again", uids, created.UID)
	}
}

// TestListPhotosMissingSidecar_excludesArchived asserts an archived photo is not
// scheduled: it is in the trash awaiting purge, and the backfill's job is the
// live library.
func TestListPhotosMissingSidecar_excludesArchived(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("sidecararchived1"))
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if _, err := store.Archive(ctx, created.UID); err != nil {
		t.Fatalf("Archive returned error: %v", err)
	}
	uids, err := store.ListPhotosMissingSidecar(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	if contains(uids, created.UID) {
		t.Errorf("pending = %v, want the archived photo %s excluded", uids, created.UID)
	}
}

// TestListPhotosMissingSidecar_limit caps the result.
func TestListPhotosMissingSidecar_limit(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	for _, hash := range []string{"sidecarlimit1", "sidecarlimit2", "sidecarlimit3"} {
		if _, err := store.Create(ctx, samplePhoto(hash)); err != nil {
			t.Fatalf("Create returned error: %v", err)
		}
	}
	uids, err := store.ListPhotosMissingSidecar(ctx, 2)
	if err != nil {
		t.Fatalf("ListPhotosMissingSidecar returned error: %v", err)
	}
	if len(uids) != 2 {
		t.Errorf("got %d uids, want the limit of 2", len(uids))
	}
}

// TestMarkSidecarWritten_unknownPhoto reports the photo is gone rather than
// silently succeeding, so a purge race surfaces instead of hiding.
func TestMarkSidecarWritten_unknownPhoto(t *testing.T) {
	store, _ := newStore(t)

	if err := store.MarkSidecarWritten(t.Context(), "nosuchphotouid00"); err == nil {
		t.Error("MarkSidecarWritten returned nil for an unknown photo, want an error")
	}
}

// contains reports whether uids holds uid.
func contains(uids []string, uid string) bool {
	for _, u := range uids {
		if u == uid {
			return true
		}
	}
	return false
}
