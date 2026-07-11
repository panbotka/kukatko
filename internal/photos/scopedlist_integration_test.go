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

// TestList_multiMembershipScope verifies the AND semantics of a multi-album /
// multi-label scope: a photo is returned only when it is a member of EVERY
// selected album and carries EVERY selected label. A photo missing any one is
// excluded. It covers albums-only, labels-only and the combined scope.
func TestList_multiMembershipScope(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	newPhoto := func(hash string) photos.Photo {
		return mustCreate(t, store, photos.Photo{
			FileHash: hash, FilePath: "p/" + hash + ".jpg", FileName: hash + ".jpg",
			FileMime: "image/jpeg", Title: hash,
		})
	}
	// both is in albums A+B and carries labels X+Y (the only full match). Each of
	// the others is missing exactly one membership, so every AND scope excludes it.
	both := newPhoto("mm-both")
	onlyA := newPhoto("mm-onlyA")
	abX := newPhoto("mm-abX") // in A+B, only label X
	xyA := newPhoto("mm-xyA") // labels X+Y, only album A
	onlyX := newPhoto("mm-onlyX")

	albumA, err := org.CreateAlbum(ctx, organize.Album{Title: "A"})
	if err != nil {
		t.Fatalf("CreateAlbum(A): %v", err)
	}
	albumB, err := org.CreateAlbum(ctx, organize.Album{Title: "B"})
	if err != nil {
		t.Fatalf("CreateAlbum(B): %v", err)
	}
	labelX, err := org.CreateLabel(ctx, organize.Label{Name: "X"})
	if err != nil {
		t.Fatalf("CreateLabel(X): %v", err)
	}
	labelY, err := org.CreateLabel(ctx, organize.Label{Name: "Y"})
	if err != nil {
		t.Fatalf("CreateLabel(Y): %v", err)
	}

	addAlbum := func(albumUID string, uids ...string) {
		t.Helper()
		for _, uid := range uids {
			if err := org.AddPhoto(ctx, albumUID, uid); err != nil {
				t.Fatalf("AddPhoto(%s): %v", uid, err)
			}
		}
	}
	attach := func(labelUID string, uids ...string) {
		t.Helper()
		for _, uid := range uids {
			if err := org.AttachLabel(ctx, uid, labelUID, organize.SourceManual, 0); err != nil {
				t.Fatalf("AttachLabel(%s): %v", uid, err)
			}
		}
	}
	addAlbum(albumA.UID, both.UID, onlyA.UID, abX.UID, xyA.UID)
	addAlbum(albumB.UID, both.UID, abX.UID)
	attach(labelX.UID, both.UID, abX.UID, xyA.UID, onlyX.UID)
	attach(labelY.UID, both.UID, xyA.UID)

	t.Run("all selected albums (AND)", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{AlbumUIDs: []string{albumA.UID, albumB.UID}})
		if err != nil {
			t.Fatalf("List(A AND B): %v", err)
		}
		set := uidSet(list)
		// Only photos in both A and B: both, abX. onlyA/xyA are in A only.
		if len(set) != 2 || !set[both.UID] || !set[abX.UID] {
			t.Fatalf("album A∧B = %v, want {both, abX}", set)
		}
	})

	t.Run("all selected labels (AND)", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{LabelUIDs: []string{labelX.UID, labelY.UID}})
		if err != nil {
			t.Fatalf("List(X AND Y): %v", err)
		}
		set := uidSet(list)
		// Only photos with both X and Y: both, xyA. abX/onlyX lack Y.
		if len(set) != 2 || !set[both.UID] || !set[xyA.UID] {
			t.Fatalf("label X∧Y = %v, want {both, xyA}", set)
		}
	})

	t.Run("albums and labels combined (AND)", func(t *testing.T) {
		params := photos.ListParams{
			AlbumUIDs: []string{albumA.UID, albumB.UID},
			LabelUIDs: []string{labelX.UID, labelY.UID},
		}
		list, err := store.List(ctx, params)
		if err != nil {
			t.Fatalf("List(A∧B ∧ X∧Y): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[both.UID] {
			t.Fatalf("combined scope = %v, want only {both}", set)
		}
		total, err := store.Count(ctx, params)
		if err != nil || total != 1 {
			t.Fatalf("Count(combined) = %d, %v, want 1", total, err)
		}
	})
}
