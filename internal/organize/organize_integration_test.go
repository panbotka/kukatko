//go:build integration

package organize_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStores returns an organize.Store plus the photos and auth stores it leans on,
// over a freshly truncated integration database.
func newStores(t *testing.T) (*organize.Store, *photos.Store, *auth.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return organize.NewStore(db.Pool()), photos.NewStore(db.Pool()), auth.NewStore(db.Pool()), db
}

// makePhoto inserts a minimal photo with the given file hash and returns its uid.
func makePhoto(t *testing.T, store *photos.Store, hash string) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash: hash,
		FilePath: "2024/01/" + hash + ".jpg",
		FileName: hash + ".jpg",
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// makePhotoAt inserts a minimal photo captured at takenAt and returns its uid.
func makePhotoAt(t *testing.T, store *photos.Store, hash string, takenAt time.Time) string {
	t.Helper()
	created, err := store.Create(context.Background(), photos.Photo{
		FileHash: hash,
		FilePath: "2024/01/" + hash + ".jpg",
		FileName: hash + ".jpg",
		TakenAt:  &takenAt,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// addPhotos puts every given photo into the album, failing the test on the first
// membership that cannot be written.
func addPhotos(t *testing.T, store *organize.Store, albumUID string, photoUIDs ...string) {
	t.Helper()
	for _, photoUID := range photoUIDs {
		if err := store.AddPhoto(context.Background(), albumUID, photoUID); err != nil {
			t.Fatalf("adding photo %s to album %s: %v", photoUID, albumUID, err)
		}
	}
}

// albumByUID returns the listed summary of the album with the given uid, failing
// the test when the listing does not carry it. Tests that care about an album's
// columns rather than its position look it up this way, so the listing order
// stays the ordering tests' concern alone.
func albumByUID(t *testing.T, list []organize.AlbumSummary, uid string) organize.AlbumSummary {
	t.Helper()
	for _, album := range list {
		if album.UID == uid {
			return album
		}
	}
	t.Fatalf("album %q missing from the listing", uid)
	return organize.AlbumSummary{}
}

// makeUser inserts a viewer account with the given uid/username and returns the uid.
func makeUser(t *testing.T, store *auth.Store, uid, username string) string {
	t.Helper()
	if err := store.CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// TestAlbumCRUD exercises create, lookups, update (with re-slug) and delete.
func TestAlbumCRUD(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	created, err := store.CreateAlbum(ctx, organize.Album{Title: "Léto u Řeky", Description: "summer"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if created.UID == "" || created.Slug != "leto-u-reky" || created.Type != organize.AlbumManual {
		t.Fatalf("unexpected created album: %+v", created)
	}

	byUID, err := store.GetAlbumByUID(ctx, created.UID)
	if err != nil || byUID.UID != created.UID {
		t.Fatalf("GetAlbumByUID = %+v, %v", byUID, err)
	}
	bySlug, err := store.GetAlbumBySlug(ctx, "leto-u-reky")
	if err != nil || bySlug.UID != created.UID {
		t.Fatalf("GetAlbumBySlug = %+v, %v", bySlug, err)
	}

	updated, err := store.UpdateAlbum(ctx, created.UID, organize.AlbumUpdate{
		Title: "Hory", Type: organize.AlbumFolder, Private: true,
	})
	if err != nil {
		t.Fatalf("UpdateAlbum: %v", err)
	}
	if updated.Title != "Hory" || updated.Slug != "hory" || updated.Type != organize.AlbumFolder ||
		!updated.Private {
		t.Fatalf("unexpected updated album: %+v", updated)
	}

	if err := store.DeleteAlbum(ctx, created.UID); err != nil {
		t.Fatalf("DeleteAlbum: %v", err)
	}
	if _, err := store.GetAlbumByUID(ctx, created.UID); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Fatalf("GetAlbumByUID after delete = %v, want ErrAlbumNotFound", err)
	}
	if err := store.DeleteAlbum(ctx, created.UID); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Fatalf("DeleteAlbum missing = %v, want ErrAlbumNotFound", err)
	}
}

// TestAlbumInvalidType checks album type validation on create and update.
func TestAlbumInvalidType(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	if _, err := store.CreateAlbum(ctx, organize.Album{Title: "X", Type: "playlist"}); !errors.Is(err, organize.ErrInvalidType) {
		t.Fatalf("CreateAlbum bad type = %v, want ErrInvalidType", err)
	}
	created, err := store.CreateAlbum(ctx, organize.Album{Title: "Y"})
	if err != nil {
		t.Fatalf("CreateAlbum: %v", err)
	}
	if _, err := store.UpdateAlbum(ctx, created.UID, organize.AlbumUpdate{Title: "Y", Type: "mixtape"}); !errors.Is(err, organize.ErrInvalidType) {
		t.Fatalf("UpdateAlbum bad type = %v, want ErrInvalidType", err)
	}
}

// TestAlbumUniqueSlug checks that albums sharing a title get distinct slugs.
func TestAlbumUniqueSlug(t *testing.T) {
	store, _, _, _ := newStores(t)
	ctx := t.Context()

	want := []string{"trip", "trip-2", "trip-3"}
	titles := []string{"Trip", "Trip", "Trip!"}
	for i, title := range titles {
		got, err := store.CreateAlbum(ctx, organize.Album{Title: title})
		if err != nil {
			t.Fatalf("CreateAlbum %d: %v", i, err)
		}
		if got.Slug != want[i] {
			t.Errorf("album %d slug = %q, want %q", i, got.Slug, want[i])
		}
	}
}

// TestAlbumPhotoMembership exercises add (idempotent), chronological listing
// and remove. Albums are always presented oldest first, with the upload time
// standing in for a photo with no capture time, so the listing ignores the
// order in which photos were added.
func TestAlbumPhotoMembership(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	oldest := makePhotoAt(t, photoStore, "ap1", time.Date(2020, 6, 1, 10, 0, 0, 0, time.UTC))
	middle := makePhotoAt(t, photoStore, "ap2", time.Date(2022, 6, 1, 10, 0, 0, 0, time.UTC))
	// No capture time: falls back to its upload (created_at) time, which is
	// "now" — after every capture time above, so it sorts last.
	undated := makePhoto(t, photoStore, "ap3")

	// Add in an order that matches neither insertion nor chronology.
	for _, p := range []string{undated, oldest, middle} {
		if err := store.AddPhoto(ctx, album.UID, p); err != nil {
			t.Fatalf("AddPhoto %s: %v", p, err)
		}
	}
	// Re-adding an existing member is an idempotent no-op.
	if err := store.AddPhoto(ctx, album.UID, oldest); err != nil {
		t.Fatalf("AddPhoto idempotent: %v", err)
	}

	got, err := store.ListPhotoUIDs(ctx, album.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDs: %v", err)
	}
	if len(got) != 3 || got[0] != oldest || got[1] != middle || got[2] != undated {
		t.Fatalf("ListPhotoUIDs = %v, want chronological [%s %s %s]", got, oldest, middle, undated)
	}

	if err := store.RemovePhoto(ctx, album.UID, oldest); err != nil {
		t.Fatalf("RemovePhoto: %v", err)
	}
	// Removing again is idempotent.
	if err := store.RemovePhoto(ctx, album.UID, oldest); err != nil {
		t.Fatalf("RemovePhoto idempotent: %v", err)
	}
	got, _ = store.ListPhotoUIDs(ctx, album.UID)
	if len(got) != 2 || got[0] != middle || got[1] != undated {
		t.Fatalf("after remove = %v, want [%s %s]", got, middle, undated)
	}
}

// TestAlbumMembershipMissing checks the not-found mappings for membership writes.
func TestAlbumMembershipMissing(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	photoUID := makePhoto(t, photoStore, "mm1")

	if err := store.AddPhoto(ctx, "almissing", photoUID); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("AddPhoto missing album = %v, want ErrAlbumNotFound", err)
	}
	if err := store.AddPhoto(ctx, album.UID, "phmissing"); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("AddPhoto missing photo = %v, want ErrPhotoNotFound", err)
	}
}

// TestAlbumListCounts checks that ListAlbums reports the photo count per album.
func TestAlbumListCounts(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	p1 := makePhoto(t, photoStore, "lc1")
	p2 := makePhoto(t, photoStore, "lc2")

	a1, _ := store.CreateAlbum(ctx, organize.Album{Title: "Alps"})
	b1, _ := store.CreateAlbum(ctx, organize.Album{Title: "Beach"})
	_ = store.AddPhoto(ctx, a1.UID, p1)
	_ = store.AddPhoto(ctx, a1.UID, p2)

	list, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListAlbums len = %d, want 2", len(list))
	}
	// Neither album carries a capture time, so both rank NULL and the uid tiebreak
	// decides their order. That is TestAlbumListOrder's business; look them up by
	// uid so this test only asserts the counts.
	if got := albumByUID(t, list, a1.UID); got.PhotoCount != 2 {
		t.Errorf("Alps count = %d, want 2", got.PhotoCount)
	}
	if got := albumByUID(t, list, b1.UID); got.PhotoCount != 0 {
		t.Errorf("Beach count = %d, want 0", got.PhotoCount)
	}
}

// TestAlbumListOrder checks that ListAlbums ranks albums by their newest photo,
// newest first: an archived photo never lifts an album, and albums with nothing
// to date — undated photos only, or no photos at all — sort last.
func TestAlbumListOrder(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	oldest := makePhotoAt(t, photoStore, "or1", time.Date(2001, 3, 4, 9, 0, 0, 0, time.UTC))
	newest := makePhotoAt(t, photoStore, "or2", time.Date(2020, 6, 7, 8, 0, 0, 0, time.UTC))
	middle := makePhotoAt(t, photoStore, "or3", time.Date(2005, 1, 2, 3, 0, 0, 0, time.UTC))
	future := makePhotoAt(t, photoStore, "or4", time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := photoStore.Archive(ctx, future); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	undatedPhoto := makePhoto(t, photoStore, "or5")

	// The titles run counter to the wanted order, so a leftover alphabetical sort
	// cannot pass this test by accident.
	oldAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "A Old"})
	recentAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "B Recent"})
	decoyAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "C Archived Decoy"})
	undatedAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "D Undated"})
	emptyAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "E Empty"})

	addPhotos(t, store, oldAlbum.UID, oldest)
	addPhotos(t, store, recentAlbum.UID, newest)
	// The 2050 photo is the decoy's newest, but it is archived: the album must rank
	// on its live 2005 photo and land between the 2020 and 2001 albums.
	addPhotos(t, store, decoyAlbum.UID, middle, future)
	addPhotos(t, store, undatedAlbum.UID, undatedPhoto)

	list, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(list) != 5 {
		t.Fatalf("ListAlbums len = %d, want 5", len(list))
	}

	gotDated := []string{list[0].UID, list[1].UID, list[2].UID}
	wantDated := []string{recentAlbum.UID, decoyAlbum.UID, oldAlbum.UID}
	if !slices.Equal(gotDated, wantDated) {
		t.Errorf("dated albums ordered %v, want newest first %v", gotDated, wantDated)
	}

	// Both tail albums rank NULL, so only the uid tiebreak separates them.
	gotTail := []string{list[3].UID, list[4].UID}
	wantTail := []string{undatedAlbum.UID, emptyAlbum.UID}
	slices.Sort(wantTail)
	if !slices.Equal(gotTail, wantTail) {
		t.Errorf("undated tail = %v, want the undated and empty albums last, in uid order %v",
			gotTail, wantTail)
	}
}

// TestAlbumListOrderTieBreak checks that two albums whose newest photo shares a
// capture time fall back to the uid tiebreak, identically on every call.
func TestAlbumListOrderTieBreak(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	shot := time.Date(2012, 12, 12, 12, 0, 0, 0, time.UTC)
	first := makePhotoAt(t, photoStore, "tb1", shot)
	second := makePhotoAt(t, photoStore, "tb2", shot)

	one, _ := store.CreateAlbum(ctx, organize.Album{Title: "Z Title"})
	two, _ := store.CreateAlbum(ctx, organize.Album{Title: "A Title"})
	addPhotos(t, store, one.UID, first)
	addPhotos(t, store, two.UID, second)

	want := []string{one.UID, two.UID}
	slices.Sort(want)

	for call := range 2 {
		list, err := store.ListAlbums(ctx)
		if err != nil {
			t.Fatalf("ListAlbums call %d: %v", call, err)
		}
		if len(list) != 2 {
			t.Fatalf("ListAlbums call %d len = %d, want 2", call, len(list))
		}
		got := []string{list[0].UID, list[1].UID}
		if !slices.Equal(got, want) {
			t.Fatalf("call %d order = %v, want uid order %v", call, got, want)
		}
	}
}

// TestAlbumListCoverAndRange checks the derived columns of ListAlbums: the
// fallback cover (the album's newest live photo, the same one on every request),
// the hand-picked cover taking precedence over it, the aggregated capture-time
// span, and an archived photo supplying neither.
func TestAlbumListCoverAndRange(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	oldest := makePhotoAt(t, photoStore, "cr1", time.Date(1998, 5, 1, 10, 0, 0, 0, time.UTC))
	newest := makePhotoAt(t, photoStore, "cr2", time.Date(1999, 8, 2, 11, 0, 0, 0, time.UTC))
	archived := makePhotoAt(t, photoStore, "cr3", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if _, err := photoStore.Archive(ctx, archived); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	undated := makePhoto(t, photoStore, "cr4")

	ranged, _ := store.CreateAlbum(ctx, organize.Album{Title: "A Ranged"})
	pinned, _ := store.CreateAlbum(ctx, organize.Album{Title: "B Pinned"})
	emptyAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "C Empty"})
	undatedAlbum, _ := store.CreateAlbum(ctx, organize.Album{Title: "D Undated"})
	for _, uid := range []string{oldest, newest, archived} {
		if err := store.AddPhoto(ctx, ranged.UID, uid); err != nil {
			t.Fatalf("AddPhoto: %v", err)
		}
		if err := store.AddPhoto(ctx, pinned.UID, uid); err != nil {
			t.Fatalf("AddPhoto: %v", err)
		}
	}
	if err := store.AddPhoto(ctx, undatedAlbum.UID, undated); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}
	if _, err := store.SetCover(ctx, pinned.UID, &oldest); err != nil {
		t.Fatalf("SetCover: %v", err)
	}

	list, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("ListAlbums len = %d, want 4", len(list))
	}

	// A Ranged: no hand-picked cover, so the newest live photo stands in. The
	// archived photo neither becomes the cover nor stretches the range to 2030.
	rangedGot := albumByUID(t, list, ranged.UID)
	if rangedGot.CoverUID == nil || *rangedGot.CoverUID != newest {
		t.Errorf("ranged cover = %v, want fallback %q", rangedGot.CoverUID, newest)
	}
	assertRange(t, "ranged", rangedGot,
		time.Date(1998, 5, 1, 10, 0, 0, 0, time.UTC),
		time.Date(1999, 8, 2, 11, 0, 0, 0, time.UTC))

	// B Pinned: same photos, but a hand-picked cover wins over the fallback.
	if got := albumByUID(t, list, pinned.UID); got.CoverUID == nil || *got.CoverUID != oldest {
		t.Errorf("pinned cover = %v, want hand-picked %q", got.CoverUID, oldest)
	}

	// C Empty: nothing to draw and nothing to date.
	if got := albumByUID(t, list, emptyAlbum.UID); got.CoverUID != nil ||
		got.TakenFrom != nil || got.TakenTo != nil {
		t.Errorf("empty album = %+v, want no cover and no range", got)
	}

	// D Undated: a photo with an unknown capture time still covers the album, but
	// contributes no range.
	undatedGot := albumByUID(t, list, undatedAlbum.UID)
	if undatedGot.CoverUID == nil || *undatedGot.CoverUID != undated {
		t.Errorf("undated cover = %v, want %q", undatedGot.CoverUID, undated)
	}
	if undatedGot.TakenFrom != nil || undatedGot.TakenTo != nil {
		t.Errorf("undated album range = %v–%v, want none", undatedGot.TakenFrom, undatedGot.TakenTo)
	}

	// The fallback must be deterministic: the same album, the same cover, always.
	again, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums again: %v", err)
	}
	if got := albumByUID(t, again, ranged.UID); *got.CoverUID != *rangedGot.CoverUID {
		t.Errorf("fallback cover changed between calls: %q then %q",
			*rangedGot.CoverUID, *got.CoverUID)
	}
}

// assertRange fails when the album's capture-time span is not exactly from–to.
func assertRange(t *testing.T, name string, got organize.AlbumSummary, from, to time.Time) {
	t.Helper()
	if got.TakenFrom == nil || !got.TakenFrom.Equal(from) {
		t.Errorf("%s taken_from = %v, want %v", name, got.TakenFrom, from)
	}
	if got.TakenTo == nil || !got.TakenTo.Equal(to) {
		t.Errorf("%s taken_to = %v, want %v", name, got.TakenTo, to)
	}
}

// TestAlbumCoverSetNullOnPhotoDelete checks SetCover and the cover_photo_uid SET
// NULL foreign key when the cover photo is deleted.
func TestAlbumCoverSetNullOnPhotoDelete(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "cover")

	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	withCover, err := store.SetCover(ctx, album.UID, &photoUID)
	if err != nil {
		t.Fatalf("SetCover: %v", err)
	}
	if withCover.CoverPhotoUID == nil || *withCover.CoverPhotoUID != photoUID {
		t.Fatalf("cover not stored: %+v", withCover)
	}

	if err := photoStore.Delete(ctx, photoUID); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	got, err := store.GetAlbumByUID(ctx, album.UID)
	if err != nil {
		t.Fatalf("GetAlbumByUID: %v", err)
	}
	if got.CoverPhotoUID != nil {
		t.Errorf("cover_photo_uid = %v, want nil after photo delete", got.CoverPhotoUID)
	}
}

// TestAlbumSetCoverMissing checks the not-found paths of SetCover.
func TestAlbumSetCoverMissing(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	photoUID := makePhoto(t, photoStore, "sc")

	if _, err := store.SetCover(ctx, "almissing", &photoUID); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("SetCover missing album = %v, want ErrAlbumNotFound", err)
	}
	bad := "phmissing"
	if _, err := store.SetCover(ctx, album.UID, &bad); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("SetCover missing photo = %v, want ErrPhotoNotFound", err)
	}
}

// TestAlbumPhotoCascadeOnPhotoDelete checks membership rows vanish when the photo
// is deleted.
func TestAlbumPhotoCascadeOnPhotoDelete(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	photoUID := makePhoto(t, photoStore, "casc")
	if err := store.AddPhoto(ctx, album.UID, photoUID); err != nil {
		t.Fatalf("AddPhoto: %v", err)
	}

	if err := photoStore.Delete(ctx, photoUID); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	got, _ := store.ListPhotoUIDs(ctx, album.UID)
	if len(got) != 0 {
		t.Errorf("album membership = %v, want empty after photo delete", got)
	}
}

// TestLabelCRUDAndAttach exercises label CRUD, attach/detach (idempotent) and the
// per-label photo counts.
func TestLabelCRUDAndAttach(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	p1 := makePhoto(t, photoStore, "lp1")
	p2 := makePhoto(t, photoStore, "lp2")

	beach, err := store.CreateLabel(ctx, organize.Label{Name: "Pláž", Priority: 5})
	if err != nil {
		t.Fatalf("CreateLabel: %v", err)
	}
	if beach.Slug != "plaz" || beach.Priority != 5 {
		t.Fatalf("unexpected label: %+v", beach)
	}
	sky, _ := store.CreateLabel(ctx, organize.Label{Name: "Sky"})

	bySlug, err := store.GetLabelBySlug(ctx, "plaz")
	if err != nil || bySlug.UID != beach.UID {
		t.Fatalf("GetLabelBySlug = %+v, %v", bySlug, err)
	}

	if err := store.AttachLabel(ctx, p1, beach.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel p1: %v", err)
	}
	if err := store.AttachLabel(ctx, p2, beach.UID, organize.SourceAI, 30); err != nil {
		t.Fatalf("AttachLabel p2: %v", err)
	}
	// Re-attaching updates source/uncertainty without erroring.
	if err := store.AttachLabel(ctx, p1, beach.UID, organize.SourceImport, 10); err != nil {
		t.Fatalf("AttachLabel idempotent: %v", err)
	}

	list, err := store.ListLabels(ctx)
	if err != nil {
		t.Fatalf("ListLabels: %v", err)
	}
	// Ordered by priority DESC: beach (5) before sky (0).
	if len(list) != 2 || list[0].UID != beach.UID || list[0].PhotoCount != 2 {
		t.Fatalf("labels[0] = %+v, want beach count 2", list[0])
	}
	if list[1].UID != sky.UID || list[1].PhotoCount != 0 {
		t.Fatalf("labels[1] = %+v, want sky count 0", list[1])
	}

	if err := store.DetachLabel(ctx, p1, beach.UID); err != nil {
		t.Fatalf("DetachLabel: %v", err)
	}
	if err := store.DetachLabel(ctx, p1, beach.UID); err != nil {
		t.Fatalf("DetachLabel idempotent: %v", err)
	}
	uids, _ := store.ListPhotoUIDsByLabel(ctx, beach.UID)
	if len(uids) != 1 || uids[0] != p2 {
		t.Fatalf("ListPhotoUIDsByLabel = %v, want [%s]", uids, p2)
	}

	updated, err := store.UpdateLabel(ctx, sky.UID, organize.LabelUpdate{Name: "Obloha", Priority: 9})
	if err != nil || updated.Slug != "obloha" || updated.Priority != 9 {
		t.Fatalf("UpdateLabel = %+v, %v", updated, err)
	}

	if err := store.DeleteLabel(ctx, beach.UID); err != nil {
		t.Fatalf("DeleteLabel: %v", err)
	}
	if _, err := store.GetLabelByUID(ctx, beach.UID); !errors.Is(err, organize.ErrLabelNotFound) {
		t.Fatalf("GetLabelByUID after delete = %v, want ErrLabelNotFound", err)
	}
}

// TestLabelAttachInvalidSourceAndMissing checks source validation and the
// not-found mappings for attachment writes.
func TestLabelAttachInvalidSourceAndMissing(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "lsrc")
	label, _ := store.CreateLabel(ctx, organize.Label{Name: "Tag"})

	if err := store.AttachLabel(ctx, photoUID, label.UID, "robot", 0); !errors.Is(err, organize.ErrInvalidSource) {
		t.Errorf("AttachLabel bad source = %v, want ErrInvalidSource", err)
	}
	if err := store.AttachLabel(ctx, "phmissing", label.UID, organize.SourceManual, 0); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("AttachLabel missing photo = %v, want ErrPhotoNotFound", err)
	}
	if err := store.AttachLabel(ctx, photoUID, "lbmissing", organize.SourceManual, 0); !errors.Is(err, organize.ErrLabelNotFound) {
		t.Errorf("AttachLabel missing label = %v, want ErrLabelNotFound", err)
	}
}

// TestPhotoLabelCascadeOnPhotoDelete checks attachment rows vanish when the photo
// is deleted.
func TestPhotoLabelCascadeOnPhotoDelete(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	photoUID := makePhoto(t, photoStore, "plc")
	label, _ := store.CreateLabel(ctx, organize.Label{Name: "Tag"})
	if err := store.AttachLabel(ctx, photoUID, label.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel: %v", err)
	}

	if err := photoStore.Delete(ctx, photoUID); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	uids, _ := store.ListPhotoUIDsByLabel(ctx, label.UID)
	if len(uids) != 0 {
		t.Errorf("label attachments = %v, want empty after photo delete", uids)
	}
}

// TestFavoritesIdempotentAndIsolated checks per-user favorite add/remove
// idempotency, the is-favorite check and isolation between users.
func TestFavoritesIdempotentAndIsolated(t *testing.T) {
	store, photoStore, userStore, _ := newStores(t)
	ctx := t.Context()
	alice := makeUser(t, userStore, "ufav_alice", "alice")
	bob := makeUser(t, userStore, "ufav_bob", "bob")
	p1 := makePhoto(t, photoStore, "fav1")
	p2 := makePhoto(t, photoStore, "fav2")

	if err := store.AddFavorite(ctx, alice, p1); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	// Adding again is idempotent.
	if err := store.AddFavorite(ctx, alice, p1); err != nil {
		t.Fatalf("AddFavorite idempotent: %v", err)
	}
	if err := store.AddFavorite(ctx, alice, p2); err != nil {
		t.Fatalf("AddFavorite p2: %v", err)
	}
	// Bob favorites only p1; favorites are per-user.
	if err := store.AddFavorite(ctx, bob, p1); err != nil {
		t.Fatalf("AddFavorite bob: %v", err)
	}

	isFav, err := store.IsFavorite(ctx, alice, p1)
	if err != nil || !isFav {
		t.Fatalf("IsFavorite alice/p1 = %v, %v, want true", isFav, err)
	}
	if isFav, _ := store.IsFavorite(ctx, bob, p2); isFav {
		t.Errorf("IsFavorite bob/p2 = true, want false (isolation)")
	}

	aliceFavs, err := store.ListFavorites(ctx, alice)
	if err != nil {
		t.Fatalf("ListFavorites: %v", err)
	}
	if len(aliceFavs) != 2 {
		t.Errorf("alice favorites = %v, want 2", aliceFavs)
	}
	bobFavs, _ := store.ListFavorites(ctx, bob)
	if len(bobFavs) != 1 || bobFavs[0] != p1 {
		t.Errorf("bob favorites = %v, want [%s]", bobFavs, p1)
	}

	// FavoritedAmong resolves a page's flags in one query and stays per-user.
	aliceSet, err := store.FavoritedAmong(ctx, alice, []string{p1, p2})
	if err != nil {
		t.Fatalf("FavoritedAmong: %v", err)
	}
	if !aliceSet[p1] || !aliceSet[p2] {
		t.Errorf("alice FavoritedAmong = %v, want both true", aliceSet)
	}
	bobSet, _ := store.FavoritedAmong(ctx, bob, []string{p1, p2})
	if !bobSet[p1] || bobSet[p2] {
		t.Errorf("bob FavoritedAmong = %v, want {p1:true, p2 absent}", bobSet)
	}
	if empty, _ := store.FavoritedAmong(ctx, alice, nil); len(empty) != 0 {
		t.Errorf("FavoritedAmong(nil) = %v, want empty", empty)
	}

	if err := store.RemoveFavorite(ctx, alice, p1); err != nil {
		t.Fatalf("RemoveFavorite: %v", err)
	}
	// Removing again is idempotent.
	if err := store.RemoveFavorite(ctx, alice, p1); err != nil {
		t.Fatalf("RemoveFavorite idempotent: %v", err)
	}
	if isFav, _ := store.IsFavorite(ctx, alice, p1); isFav {
		t.Errorf("IsFavorite alice/p1 after remove = true, want false")
	}
	// Bob's favorite of p1 is untouched by alice's removal.
	if isFav, _ := store.IsFavorite(ctx, bob, p1); !isFav {
		t.Errorf("IsFavorite bob/p1 = false, want true (isolation)")
	}
}

// TestFavoriteMissing checks the not-found mappings for favorite writes.
func TestFavoriteMissing(t *testing.T) {
	store, photoStore, userStore, _ := newStores(t)
	ctx := t.Context()
	user := makeUser(t, userStore, "fmiss_u", "fmiss")
	photoUID := makePhoto(t, photoStore, "fmiss")

	if err := store.AddFavorite(ctx, "usermissing", photoUID); !errors.Is(err, organize.ErrUserNotFound) {
		t.Errorf("AddFavorite missing user = %v, want ErrUserNotFound", err)
	}
	if err := store.AddFavorite(ctx, user, "phmissing"); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("AddFavorite missing photo = %v, want ErrPhotoNotFound", err)
	}
}

// TestFavoriteCascadeOnUserDelete checks a user's favorites vanish when the user
// is deleted (ON DELETE CASCADE), without affecting other users.
func TestFavoriteCascadeOnUserDelete(t *testing.T) {
	store, photoStore, userStore, db := newStores(t)
	ctx := t.Context()
	alice := makeUser(t, userStore, "cdel_alice", "calice")
	bob := makeUser(t, userStore, "cdel_bob", "cbob")
	photoUID := makePhoto(t, photoStore, "cdel")

	if err := store.AddFavorite(ctx, alice, photoUID); err != nil {
		t.Fatalf("AddFavorite alice: %v", err)
	}
	if err := store.AddFavorite(ctx, bob, photoUID); err != nil {
		t.Fatalf("AddFavorite bob: %v", err)
	}

	if _, err := db.Pool().Exec(ctx, "DELETE FROM users WHERE uid = $1", alice); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if favs, _ := store.ListFavorites(ctx, alice); len(favs) != 0 {
		t.Errorf("alice favorites = %v, want empty after user delete", favs)
	}
	if isFav, _ := store.IsFavorite(ctx, bob, photoUID); !isFav {
		t.Errorf("bob favorite removed by alice's deletion, want preserved")
	}
}

// TestAlbumsForPhoto returns the albums a photo belongs to, ordered by title.
func TestAlbumsForPhoto(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	photoUID := makePhoto(t, photoStore, "ph-mem")
	zebra, err := store.CreateAlbum(ctx, organize.Album{Title: "Zebra"})
	if err != nil {
		t.Fatalf("CreateAlbum Zebra: %v", err)
	}
	alps, err := store.CreateAlbum(ctx, organize.Album{Title: "Alps"})
	if err != nil {
		t.Fatalf("CreateAlbum Alps: %v", err)
	}
	if _, err := store.CreateAlbum(ctx, organize.Album{Title: "Empty"}); err != nil {
		t.Fatalf("CreateAlbum Empty: %v", err)
	}
	if err := store.AddPhoto(ctx, zebra.UID, photoUID); err != nil {
		t.Fatalf("AddPhoto Zebra: %v", err)
	}
	if err := store.AddPhoto(ctx, alps.UID, photoUID); err != nil {
		t.Fatalf("AddPhoto Alps: %v", err)
	}

	got, err := store.AlbumsForPhoto(ctx, photoUID)
	if err != nil {
		t.Fatalf("AlbumsForPhoto: %v", err)
	}
	if len(got) != 2 || got[0].Title != "Alps" || got[1].Title != "Zebra" {
		t.Fatalf("AlbumsForPhoto = %+v, want [Alps, Zebra]", got)
	}

	none, err := store.AlbumsForPhoto(ctx, "ph-nope")
	if err != nil {
		t.Fatalf("AlbumsForPhoto(unknown): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("AlbumsForPhoto(unknown) = %+v, want empty", none)
	}
}

// TestLabelsForPhoto returns the labels attached to a photo, highest priority first.
func TestLabelsForPhoto(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()

	photoUID := makePhoto(t, photoStore, "ph-lbl")
	low, err := store.CreateLabel(ctx, organize.Label{Name: "beach", Priority: 1})
	if err != nil {
		t.Fatalf("CreateLabel beach: %v", err)
	}
	high, err := store.CreateLabel(ctx, organize.Label{Name: "sunset", Priority: 9})
	if err != nil {
		t.Fatalf("CreateLabel sunset: %v", err)
	}
	if _, err := store.CreateLabel(ctx, organize.Label{Name: "unused", Priority: 5}); err != nil {
		t.Fatalf("CreateLabel unused: %v", err)
	}
	if err := store.AttachLabel(ctx, photoUID, low.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel beach: %v", err)
	}
	if err := store.AttachLabel(ctx, photoUID, high.UID, organize.SourceManual, 0); err != nil {
		t.Fatalf("AttachLabel sunset: %v", err)
	}

	got, err := store.LabelsForPhoto(ctx, photoUID)
	if err != nil {
		t.Fatalf("LabelsForPhoto: %v", err)
	}
	if len(got) != 2 || got[0].Name != "sunset" || got[1].Name != "beach" {
		t.Fatalf("LabelsForPhoto = %+v, want [sunset, beach]", got)
	}
}
