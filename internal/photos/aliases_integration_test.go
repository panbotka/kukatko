//go:build integration

package photos_test

import (
	"errors"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They cover the photoprism_aliases table (migration
// 0046): the record of a source photo that collapsed onto a catalogue row already
// holding its exact content under another source uid.

// TestPhotoprismAlias_resolvesToTheHoldingPhoto verifies the round trip an alias
// exists for: a source uid with no row of its own still resolves to the row that
// holds its content, while an unaliased uid stays not-found.
func TestPhotoprismAlias_resolvesToTheHoldingPhoto(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	winner := blankPhoto(t, store, "aliashash1")
	if err := store.AddPhotoprismAlias(ctx, "ppDup", winner.UID, "sha1-of-dup"); err != nil {
		t.Fatalf("AddPhotoprismAlias: %v", err)
	}

	got, err := store.GetByPhotoprismAlias(ctx, "ppDup")
	if err != nil {
		t.Fatalf("GetByPhotoprismAlias: %v", err)
	}
	if got.UID != winner.UID {
		t.Errorf("alias resolved to %q, want %q", got.UID, winner.UID)
	}
	if _, err := store.GetByPhotoprismAlias(ctx, "ppUnknown"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("unaliased uid error = %v, want ErrPhotoNotFound", err)
	}

	aliases, err := store.ListPhotoprismAliases(ctx)
	if err != nil {
		t.Fatalf("ListPhotoprismAliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].PhotoprismUID != "ppDup" || aliases[0].PhotoUID != winner.UID {
		t.Errorf("aliases = %+v, want one ppDup -> %s", aliases, winner.UID)
	}
	if aliases[0].PhotoprismFileHash != "sha1-of-dup" {
		t.Errorf("alias file hash = %q, want sha1-of-dup", aliases[0].PhotoprismFileHash)
	}
}

// TestPhotoprismAlias_isIdempotentAndRepointable verifies re-recording the same
// alias is a no-op — the import re-derives it on every pass — and that an alias
// whose content ended up on another row is re-pointed rather than rejected.
func TestPhotoprismAlias_isIdempotentAndRepointable(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	first := blankPhoto(t, store, "aliashash2")
	second := blankPhoto(t, store, "aliashash3")
	for range 2 {
		if err := store.AddPhotoprismAlias(ctx, "ppDup", first.UID, "sha1-a"); err != nil {
			t.Fatalf("AddPhotoprismAlias: %v", err)
		}
	}
	if err := store.AddPhotoprismAlias(ctx, "ppDup", second.UID, "sha1-a"); err != nil {
		t.Fatalf("re-pointing alias: %v", err)
	}

	aliases, err := store.ListPhotoprismAliases(ctx)
	if err != nil {
		t.Fatalf("ListPhotoprismAliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].PhotoUID != second.UID {
		t.Errorf("aliases = %+v, want one pointing at %s", aliases, second.UID)
	}
}

// TestPhotoprismAlias_cascadesWithTheHoldingPhoto verifies the alias dies with the
// row it points at: the content is gone, so the source photo must become
// importable again rather than resolving to a photo that no longer exists.
func TestPhotoprismAlias_cascadesWithTheHoldingPhoto(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	winner := blankPhoto(t, store, "aliashash4")
	if err := store.AddPhotoprismAlias(ctx, "ppDup", winner.UID, ""); err != nil {
		t.Fatalf("AddPhotoprismAlias: %v", err)
	}
	if err := store.Delete(ctx, winner.UID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.GetByPhotoprismAlias(ctx, "ppDup"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("alias error after delete = %v, want ErrPhotoNotFound", err)
	}
}
