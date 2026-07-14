//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// filterUIDSet runs FilterUIDs and returns the surviving uids as a set, for
// order-independent membership assertions (the method's order is unspecified).
func filterUIDSet(t *testing.T, store *photos.Store, uids []string, params photos.ListParams) map[string]bool {
	t.Helper()
	got, err := store.FilterUIDs(t.Context(), uids, params)
	if err != nil {
		t.Fatalf("FilterUIDs(%v, %+v): %v", uids, params, err)
	}
	set := make(map[string]bool, len(got))
	for _, p := range got {
		set[p.UID] = true
	}
	return set
}

// TestFilterUIDs_appliesFiltersAndIgnoresUnknown verifies FilterUIDs keeps only
// the candidate uids that pass the structural filters, drops unknown uids, and
// ignores the FullText query (semantic candidates must not require a text match).
func TestFilterUIDs_appliesFiltersAndIgnoresUnknown(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	jan := time.Date(2022, 1, 15, 12, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	pub := mustCreate(t, store, photos.Photo{
		FileHash: "f-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg",
		Title: "public", TakenAt: &jun, TakenAtSource: "exif",
	})
	recent := mustCreate(t, store, photos.Photo{
		FileHash: "f-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		Title: "recent", TakenAt: &jun, TakenAtSource: "exif",
	})
	old := mustCreate(t, store, photos.Photo{
		FileHash: "f-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg",
		Title: "old", TakenAt: &jan, TakenAtSource: "exif",
	})

	candidates := []string{pub.UID, recent.UID, old.UID, "ph_does_not_exist"}

	t.Run("no filter keeps all known candidates", func(t *testing.T) {
		set := filterUIDSet(t, store, candidates, photos.ListParams{})
		if len(set) != 3 || !set[pub.UID] || !set[recent.UID] || !set[old.UID] {
			t.Fatalf("FilterUIDs(no filter) = %v, want the 3 known uids", set)
		}
	})

	t.Run("date range filter narrows the set", func(t *testing.T) {
		after := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		set := filterUIDSet(t, store, candidates, photos.ListParams{TakenAfter: &after})
		if len(set) != 2 || set[old.UID] {
			t.Fatalf("FilterUIDs(taken_after 2023) = %v, want pub+recent", set)
		}
	})

	t.Run("FullText is ignored", func(t *testing.T) {
		// "public" would only full-text-match pub, but FilterUIDs must ignore it.
		set := filterUIDSet(t, store, candidates, photos.ListParams{FullText: "public"})
		if len(set) != 3 {
			t.Fatalf("FilterUIDs(FullText set) = %v, want all 3 (FullText ignored)", set)
		}
	})

	t.Run("empty input returns empty without error", func(t *testing.T) {
		got, err := store.FilterUIDs(ctx, nil, photos.ListParams{})
		if err != nil || len(got) != 0 {
			t.Fatalf("FilterUIDs(nil) = %v, %v, want empty/nil", got, err)
		}
	})
}

// mustCreate inserts a photo and fails the test on error, returning the created
// record.
func mustCreate(t *testing.T, store *photos.Store, p photos.Photo) photos.Photo {
	t.Helper()
	created, err := store.Create(t.Context(), p)
	if err != nil {
		t.Fatalf("Create(%s): %v", p.FileHash, err)
	}
	return created
}
