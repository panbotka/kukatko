//go:build integration

package organize_test

import (
	"context"
	"errors"
	"testing"

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
	if created.OrderBy != "added" {
		t.Errorf("order_by = %q, want default \"added\"", created.OrderBy)
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
		Title: "Hory", Type: organize.AlbumFolder, Private: true, OrderBy: "oldest",
	})
	if err != nil {
		t.Fatalf("UpdateAlbum: %v", err)
	}
	if updated.Title != "Hory" || updated.Slug != "hory" || updated.Type != organize.AlbumFolder ||
		!updated.Private || updated.OrderBy != "oldest" {
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

// TestAlbumPhotoMembership exercises add (idempotent), list, reorder and remove.
func TestAlbumPhotoMembership(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	p1 := makePhoto(t, photoStore, "ap1")
	p2 := makePhoto(t, photoStore, "ap2")
	p3 := makePhoto(t, photoStore, "ap3")

	for i, p := range []string{p1, p2, p3} {
		if err := store.AddPhoto(ctx, album.UID, p, i); err != nil {
			t.Fatalf("AddPhoto %s: %v", p, err)
		}
	}
	// Re-adding is idempotent and updates position rather than erroring.
	if err := store.AddPhoto(ctx, album.UID, p1, 0); err != nil {
		t.Fatalf("AddPhoto idempotent: %v", err)
	}

	got, err := store.ListPhotoUIDs(ctx, album.UID)
	if err != nil {
		t.Fatalf("ListPhotoUIDs: %v", err)
	}
	if len(got) != 3 || got[0] != p1 || got[1] != p2 || got[2] != p3 {
		t.Fatalf("ListPhotoUIDs = %v, want [%s %s %s]", got, p1, p2, p3)
	}

	if err := store.ReorderPhotos(ctx, album.UID, []string{p3, p1, p2}); err != nil {
		t.Fatalf("ReorderPhotos: %v", err)
	}
	got, _ = store.ListPhotoUIDs(ctx, album.UID)
	if len(got) != 3 || got[0] != p3 || got[1] != p1 || got[2] != p2 {
		t.Fatalf("after reorder = %v, want [%s %s %s]", got, p3, p1, p2)
	}

	if err := store.RemovePhoto(ctx, album.UID, p1); err != nil {
		t.Fatalf("RemovePhoto: %v", err)
	}
	// Removing again is idempotent.
	if err := store.RemovePhoto(ctx, album.UID, p1); err != nil {
		t.Fatalf("RemovePhoto idempotent: %v", err)
	}
	got, _ = store.ListPhotoUIDs(ctx, album.UID)
	if len(got) != 2 || got[0] != p3 || got[1] != p2 {
		t.Fatalf("after remove = %v, want [%s %s]", got, p3, p2)
	}
}

// TestAlbumMembershipMissing checks the not-found mappings for membership writes.
func TestAlbumMembershipMissing(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	album, _ := store.CreateAlbum(ctx, organize.Album{Title: "Album"})
	photoUID := makePhoto(t, photoStore, "mm1")

	if err := store.AddPhoto(ctx, "almissing", photoUID, 0); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("AddPhoto missing album = %v, want ErrAlbumNotFound", err)
	}
	if err := store.AddPhoto(ctx, album.UID, "phmissing", 0); !errors.Is(err, organize.ErrPhotoNotFound) {
		t.Errorf("AddPhoto missing photo = %v, want ErrPhotoNotFound", err)
	}
	if err := store.ReorderPhotos(ctx, "almissing", nil); !errors.Is(err, organize.ErrAlbumNotFound) {
		t.Errorf("ReorderPhotos missing album = %v, want ErrAlbumNotFound", err)
	}
}

// TestAlbumListCounts checks that ListAlbums reports the photo count per album,
// ordered by title.
func TestAlbumListCounts(t *testing.T) {
	store, photoStore, _, _ := newStores(t)
	ctx := t.Context()
	p1 := makePhoto(t, photoStore, "lc1")
	p2 := makePhoto(t, photoStore, "lc2")

	a1, _ := store.CreateAlbum(ctx, organize.Album{Title: "Alps"})
	b1, _ := store.CreateAlbum(ctx, organize.Album{Title: "Beach"})
	_ = store.AddPhoto(ctx, a1.UID, p1, 0)
	_ = store.AddPhoto(ctx, a1.UID, p2, 1)

	list, err := store.ListAlbums(ctx)
	if err != nil {
		t.Fatalf("ListAlbums: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListAlbums len = %d, want 2", len(list))
	}
	if list[0].UID != a1.UID || list[0].PhotoCount != 2 {
		t.Errorf("album[0] = %+v, want Alps count 2", list[0])
	}
	if list[1].UID != b1.UID || list[1].PhotoCount != 0 {
		t.Errorf("album[1] = %+v, want Beach count 0", list[1])
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
	if err := store.AddPhoto(ctx, album.UID, photoUID, 0); err != nil {
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
