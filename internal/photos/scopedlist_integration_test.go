//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// uidSet collects the UIDs of a photo slice into a set for order-independent
// membership assertions.
func uidSet(list []photos.Photo) map[string]bool {
	set := make(map[string]bool, len(list))
	for _, p := range list {
		set[p.UID] = true
	}
	return set
}

// TestList_albumScope verifies that scoping List/Count by album restricts the
// result to the album's photos while still honouring the standard filters, the
// chosen sort and pagination — the contract the shared GET /photos?album= grid
// relies on.
func TestList_albumScope(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	jan := time.Date(2022, 1, 15, 12, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)
	dec := time.Date(2023, 12, 15, 12, 0, 0, 0, time.UTC)

	older := mustCreate(t, store, photos.Photo{
		FileHash: "a-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg",
		Title: "older", TakenAt: &jan, TakenAtSource: "exif",
	})
	mid := mustCreate(t, store, photos.Photo{
		FileHash: "a-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		Title: "mid", Private: true, TakenAt: &jun, TakenAtSource: "exif",
	})
	newer := mustCreate(t, store, photos.Photo{
		FileHash: "a-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg",
		Title: "newer", TakenAt: &dec, TakenAtSource: "exif",
	})
	// outsider is not added to the album, so the scope must exclude it.
	outsider := mustCreate(t, store, photos.Photo{
		FileHash: "a-4", FilePath: "p/4.jpg", FileName: "4.jpg", FileMime: "image/jpeg",
		Title: "outsider", TakenAt: &jun, TakenAtSource: "exif",
	})

	album, err := org.CreateAlbum(ctx, organize.Album{Title: "Trip"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	for _, uid := range []string{older.UID, mid.UID, newer.UID} {
		if err := org.AddPhoto(ctx, album.UID, uid); err != nil {
			t.Fatalf("AddPhoto(%s): %v", uid, err)
		}
	}

	t.Run("scope keeps only the album's photos", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{AlbumUIDs: []string{album.UID}})
		if err != nil {
			t.Fatalf("List(album): %v", err)
		}
		set := uidSet(list)
		if len(set) != 3 || set[outsider.UID] {
			t.Fatalf("album scope = %v, want the 3 members without the outsider", set)
		}
		total, err := store.Count(ctx, photos.ListParams{AlbumUIDs: []string{album.UID}})
		if err != nil || total != 3 {
			t.Fatalf("Count(album) = %d, %v, want 3", total, err)
		}
	})

	t.Run("scope combines with a metadata filter", func(t *testing.T) {
		no := false
		list, err := store.List(ctx, photos.ListParams{AlbumUIDs: []string{album.UID}, Private: &no})
		if err != nil {
			t.Fatalf("List(album, private=false): %v", err)
		}
		set := uidSet(list)
		if len(set) != 2 || set[mid.UID] {
			t.Fatalf("album+private scope = %v, want older+newer", set)
		}
	})

	t.Run("scope honours sort and pagination", func(t *testing.T) {
		// Oldest-first, first page of one: the album's earliest photo.
		list, err := store.List(ctx, photos.ListParams{
			AlbumUIDs: []string{album.UID}, Sort: photos.SortByTakenAt, Order: photos.OrderAsc, Limit: 1, Offset: 0,
		})
		if err != nil {
			t.Fatalf("List(album, sorted page): %v", err)
		}
		if len(list) != 1 || list[0].UID != older.UID {
			t.Fatalf("first oldest page = %v, want [older]", list)
		}
		// Second page advances to the next-oldest member.
		list, err = store.List(ctx, photos.ListParams{
			AlbumUIDs: []string{album.UID}, Sort: photos.SortByTakenAt, Order: photos.OrderAsc, Limit: 1, Offset: 1,
		})
		if err != nil {
			t.Fatalf("List(album, second page): %v", err)
		}
		if len(list) != 1 || list[0].UID != mid.UID {
			t.Fatalf("second oldest page = %v, want [mid]", list)
		}
	})
}

// TestList_multipleAlbumsAND verifies that scoping List/Count by several albums
// keeps only the photos that are members of every listed album — the AND
// semantics the multi-album filter relies on. A photo in just one of the two
// albums must be excluded.
func TestList_multipleAlbumsAND(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	inBoth := mustCreate(t, store, photos.Photo{
		FileHash: "m-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "both",
	})
	inFirst := mustCreate(t, store, photos.Photo{
		FileHash: "m-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg", Title: "first-only",
	})
	inSecond := mustCreate(t, store, photos.Photo{
		FileHash: "m-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg", Title: "second-only",
	})

	first, err := org.CreateAlbum(ctx, organize.Album{Title: "First"})
	if err != nil {
		t.Fatalf("CreateAlbum(first): %v", err)
	}
	second, err := org.CreateAlbum(ctx, organize.Album{Title: "Second"})
	if err != nil {
		t.Fatalf("CreateAlbum(second): %v", err)
	}
	for _, uid := range []string{inBoth.UID, inFirst.UID} {
		if err := org.AddPhoto(ctx, first.UID, uid); err != nil {
			t.Fatalf("AddPhoto(first, %s): %v", uid, err)
		}
	}
	for _, uid := range []string{inBoth.UID, inSecond.UID} {
		if err := org.AddPhoto(ctx, second.UID, uid); err != nil {
			t.Fatalf("AddPhoto(second, %s): %v", uid, err)
		}
	}

	params := photos.ListParams{AlbumUIDs: []string{first.UID, second.UID}}
	list, err := store.List(ctx, params)
	if err != nil {
		t.Fatalf("List(both albums): %v", err)
	}
	set := uidSet(list)
	if len(set) != 1 || !set[inBoth.UID] {
		t.Fatalf("multi-album AND = %v, want only the photo in both albums", set)
	}
	total, err := store.Count(ctx, params)
	if err != nil || total != 1 {
		t.Fatalf("Count(both albums) = %d, %v, want 1", total, err)
	}
}

// TestList_labelScope verifies that scoping List/Count by label restricts the
// result to the label's photos while honouring the standard filters and
// pagination — the contract the shared GET /photos?label= grid relies on.
func TestList_labelScope(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	tagged := mustCreate(t, store, photos.Photo{
		FileHash: "l-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "tagged",
	})
	taggedPriv := mustCreate(t, store, photos.Photo{
		FileHash: "l-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		Title: "tagged-private", Private: true,
	})
	untagged := mustCreate(t, store, photos.Photo{
		FileHash: "l-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg", Title: "untagged",
	})

	label, err := org.CreateLabel(ctx, organize.Label{Name: "Beach"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	for _, uid := range []string{tagged.UID, taggedPriv.UID} {
		if err := org.AttachLabel(ctx, uid, label.UID, organize.SourceManual, 0); err != nil {
			t.Fatalf("AttachLabel(%s): %v", uid, err)
		}
	}

	t.Run("scope keeps only the label's photos", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{LabelUIDs: []string{label.UID}})
		if err != nil {
			t.Fatalf("List(label): %v", err)
		}
		set := uidSet(list)
		if len(set) != 2 || set[untagged.UID] {
			t.Fatalf("label scope = %v, want the 2 tagged photos", set)
		}
		total, err := store.Count(ctx, photos.ListParams{LabelUIDs: []string{label.UID}})
		if err != nil || total != 2 {
			t.Fatalf("Count(label) = %d, %v, want 2", total, err)
		}
	})

	t.Run("scope combines with a metadata filter", func(t *testing.T) {
		no := false
		list, err := store.List(ctx, photos.ListParams{LabelUIDs: []string{label.UID}, Private: &no})
		if err != nil {
			t.Fatalf("List(label, private=false): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[tagged.UID] {
			t.Fatalf("label+private scope = %v, want only the public tagged photo", set)
		}
	})
}

// TestList_multipleLabelsAND verifies that scoping List by several labels keeps
// only the photos that carry every listed label — the AND semantics the
// multi-label filter relies on. A photo with just one of the two labels must be
// excluded.
func TestList_multipleLabelsAND(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	both := mustCreate(t, store, photos.Photo{
		FileHash: "ml-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "both",
	})
	onlyBeach := mustCreate(t, store, photos.Photo{
		FileHash: "ml-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg", Title: "beach-only",
	})

	beach, err := org.CreateLabel(ctx, organize.Label{Name: "Beach"})
	if err != nil {
		t.Fatalf("CreateLabel(beach): %v", err)
	}
	sunset, err := org.CreateLabel(ctx, organize.Label{Name: "Sunset"})
	if err != nil {
		t.Fatalf("CreateLabel(sunset): %v", err)
	}
	if err := org.AttachLabel(ctx, both.UID, beach.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel(both, beach): %v", err)
	}
	if err := org.AttachLabel(ctx, both.UID, sunset.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel(both, sunset): %v", err)
	}
	if err := org.AttachLabel(ctx, onlyBeach.UID, beach.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel(onlyBeach, beach): %v", err)
	}

	list, err := store.List(ctx, photos.ListParams{LabelUIDs: []string{beach.UID, sunset.UID}})
	if err != nil {
		t.Fatalf("List(both labels): %v", err)
	}
	set := uidSet(list)
	if len(set) != 1 || !set[both.UID] {
		t.Fatalf("multi-label AND = %v, want only the photo carrying both labels", set)
	}
}
