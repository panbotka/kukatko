//go:build integration

package photos_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
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
		Title: "mid", TakenAt: &jun, TakenAtSource: "exif",
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
		sep := time.Date(2023, 9, 1, 0, 0, 0, 0, time.UTC)
		list, err := store.List(ctx, photos.ListParams{AlbumUIDs: []string{album.UID}, TakenBefore: &sep})
		if err != nil {
			t.Fatalf("List(album, taken_before): %v", err)
		}
		set := uidSet(list)
		if len(set) != 2 || set[newer.UID] {
			t.Fatalf("album+date scope = %v, want older+mid", set)
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

// mustMarker attaches a marker on photoUID assigning subjectUID (marked invalid
// when rejected), failing the test on error. It links a subject to a photo the way
// the person filter joins them — through a named marker in the markers table.
func mustMarker(t *testing.T, ppl *people.Store, photoUID, subjectUID string, rejected bool) {
	t.Helper()
	if _, err := ppl.CreateMarker(t.Context(), people.Marker{
		PhotoUID: photoUID, SubjectUID: &subjectUID, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Invalid: rejected,
	}); err != nil {
		t.Fatalf("CreateMarker(%s->%s): %v", photoUID, subjectUID, err)
	}
}

// TestList_subjectScope verifies that scoping List/Count by subject (person)
// restricts the result to photos that contain the subject — a non-invalid marker
// links them — while still honouring the standard filters, the contract the shared
// GET /photos?person= grid relies on. Rejected (invalid) markers must not count,
// matching the subject photo gallery.
func TestList_subjectScope(t *testing.T) {
	store, db := newStore(t)
	ppl := people.NewStore(db.Pool())
	ctx := t.Context()

	jan := time.Date(2022, 1, 15, 12, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	withSubject := mustCreate(t, store, photos.Photo{
		FileHash: "s-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "with",
		TakenAt: &jun, TakenAtSource: "exif",
	})
	withSubjectOld := mustCreate(t, store, photos.Photo{
		FileHash: "s-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		Title: "with-old", TakenAt: &jan, TakenAtSource: "exif",
	})
	rejected := mustCreate(t, store, photos.Photo{
		FileHash: "s-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg", Title: "rejected",
	})
	// without carries no marker for the subject, so the scope must exclude it.
	without := mustCreate(t, store, photos.Photo{
		FileHash: "s-4", FilePath: "p/4.jpg", FileName: "4.jpg", FileMime: "image/jpeg", Title: "without",
	})

	subject, err := ppl.CreateSubject(ctx, people.Subject{Name: "Alice", Type: people.SubjectPerson})
	if err != nil {
		t.Fatalf("CreateSubject: %v", err)
	}
	for _, uid := range []string{withSubject.UID, withSubjectOld.UID} {
		mustMarker(t, ppl, uid, subject.UID, false)
	}
	// A rejected (invalid) marker must not make the photo match the person filter.
	mustMarker(t, ppl, rejected.UID, subject.UID, true)

	t.Run("scope keeps only photos containing the subject", func(t *testing.T) {
		list, err := store.List(ctx, photos.ListParams{SubjectUIDs: []string{subject.UID}})
		if err != nil {
			t.Fatalf("List(subject): %v", err)
		}
		set := uidSet(list)
		if len(set) != 2 || set[rejected.UID] || set[without.UID] {
			t.Fatalf("subject scope = %v, want the 2 photos with a valid marker", set)
		}
		total, err := store.Count(ctx, photos.ListParams{SubjectUIDs: []string{subject.UID}})
		if err != nil || total != 2 {
			t.Fatalf("Count(subject) = %d, %v, want 2", total, err)
		}
	})

	t.Run("scope combines with a metadata filter", func(t *testing.T) {
		after := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		list, err := store.List(ctx, photos.ListParams{SubjectUIDs: []string{subject.UID}, TakenAfter: &after})
		if err != nil {
			t.Fatalf("List(subject, taken_after): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[withSubject.UID] {
			t.Fatalf("subject+date scope = %v, want only the recent photo with the subject", set)
		}
	})
}

// TestList_multipleSubjectsAND verifies that scoping List by several subjects keeps
// only the photos that contain every listed subject — the AND semantics the
// multi-person filter relies on. A photo with just one of the two people must be
// excluded.
func TestList_multipleSubjectsAND(t *testing.T) {
	store, db := newStore(t)
	ppl := people.NewStore(db.Pool())
	ctx := t.Context()

	both := mustCreate(t, store, photos.Photo{
		FileHash: "ms-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "both",
	})
	onlyAlice := mustCreate(t, store, photos.Photo{
		FileHash: "ms-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg", Title: "alice-only",
	})

	alice, err := ppl.CreateSubject(ctx, people.Subject{Name: "Alice", Type: people.SubjectPerson})
	if err != nil {
		t.Fatalf("CreateSubject(alice): %v", err)
	}
	bob, err := ppl.CreateSubject(ctx, people.Subject{Name: "Bob", Type: people.SubjectPerson})
	if err != nil {
		t.Fatalf("CreateSubject(bob): %v", err)
	}
	mustMarker(t, ppl, both.UID, alice.UID, false)
	mustMarker(t, ppl, both.UID, bob.UID, false)
	mustMarker(t, ppl, onlyAlice.UID, alice.UID, false)

	params := photos.ListParams{SubjectUIDs: []string{alice.UID, bob.UID}}
	list, err := store.List(ctx, params)
	if err != nil {
		t.Fatalf("List(both subjects): %v", err)
	}
	set := uidSet(list)
	if len(set) != 1 || !set[both.UID] {
		t.Fatalf("multi-subject AND = %v, want only the photo containing both people", set)
	}
	total, err := store.Count(ctx, params)
	if err != nil || total != 1 {
		t.Fatalf("Count(both subjects) = %d, %v, want 1", total, err)
	}
}

// TestList_labelScope verifies that scoping List/Count by label restricts the
// result to the label's photos while honouring the standard filters and
// pagination — the contract the shared GET /photos?label= grid relies on.
func TestList_labelScope(t *testing.T) {
	store, db := newStore(t)
	org := organize.NewStore(db.Pool())
	ctx := t.Context()

	jan := time.Date(2022, 1, 15, 12, 0, 0, 0, time.UTC)
	jun := time.Date(2023, 6, 15, 12, 0, 0, 0, time.UTC)

	tagged := mustCreate(t, store, photos.Photo{
		FileHash: "l-1", FilePath: "p/1.jpg", FileName: "1.jpg", FileMime: "image/jpeg", Title: "tagged",
		TakenAt: &jun, TakenAtSource: "exif",
	})
	taggedOld := mustCreate(t, store, photos.Photo{
		FileHash: "l-2", FilePath: "p/2.jpg", FileName: "2.jpg", FileMime: "image/jpeg",
		Title: "tagged-old", TakenAt: &jan, TakenAtSource: "exif",
	})
	untagged := mustCreate(t, store, photos.Photo{
		FileHash: "l-3", FilePath: "p/3.jpg", FileName: "3.jpg", FileMime: "image/jpeg", Title: "untagged",
	})

	label, err := org.CreateLabel(ctx, organize.Label{Name: "Beach"})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	for _, uid := range []string{tagged.UID, taggedOld.UID} {
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
		after := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		list, err := store.List(ctx, photos.ListParams{LabelUIDs: []string{label.UID}, TakenAfter: &after})
		if err != nil {
			t.Fatalf("List(label, taken_after): %v", err)
		}
		set := uidSet(list)
		if len(set) != 1 || !set[tagged.UID] {
			t.Fatalf("label+date scope = %v, want only the recent tagged photo", set)
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
