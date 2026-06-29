//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestListActivePhashes returns hashes of live photos only, ordered by uid, and
// excludes archived ones.
func TestListActivePhashes(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	live1 := mustCreatePhoto(t, store, "active-1")
	live2 := mustCreatePhoto(t, store, "active-2")
	archived := mustCreatePhoto(t, store, "active-archived")

	for _, uid := range []string{live1, live2, archived} {
		if err := store.SetPhash(ctx, photos.Phash{PhotoUID: uid, Phash: 1, Dhash: 2}); err != nil {
			t.Fatalf("SetPhash(%s): %v", uid, err)
		}
	}
	if _, err := store.Archive(ctx, archived); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got, err := store.ListActivePhashes(ctx)
	if err != nil {
		t.Fatalf("ListActivePhashes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d phashes, want 2 (archived excluded)", len(got))
	}
	if got[0].PhotoUID >= got[1].PhotoUID {
		t.Errorf("phashes not ordered by uid: %s then %s", got[0].PhotoUID, got[1].PhotoUID)
	}
	for _, p := range got {
		if p.PhotoUID == archived {
			t.Errorf("archived photo %s returned", archived)
		}
	}
}

// mustCreatePhoto inserts the sample photo with the given hash and returns its
// uid, failing the test on error.
func mustCreatePhoto(t *testing.T, store *photos.Store, hash string) string {
	t.Helper()
	created, err := store.Create(t.Context(), samplePhoto(hash))
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}
