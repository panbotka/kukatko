//go:build integration

package photos_test

import (
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestListPrimaryFiles verifies ListPrimaryFiles returns one row per photo for
// its primary original, ignoring sidecar files.
func TestListPrimaryFiles(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if files, err := store.ListPrimaryFiles(ctx); err != nil || len(files) != 0 {
		t.Fatalf("ListPrimaryFiles() on empty = (%v, %v), want ([], nil)", files, err)
	}

	photoA, err := store.Create(ctx, samplePhoto("prim-a"))
	if err != nil {
		t.Fatalf("Create(a): %v", err)
	}
	if _, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photoA.UID, FilePath: "2023/06/prim-a.jpg", FileHash: "prim-a",
		FileSize: 10, FileMime: "image/jpeg", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("CreateFile(primary a): %v", err)
	}
	// A sidecar must not appear in the primary listing.
	if _, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photoA.UID, FilePath: "2023/06/prim-a.mov", FileHash: "prim-a-mov",
		FileSize: 20, FileMime: "video/quicktime", IsPrimary: false, Role: photos.RoleSidecar,
	}); err != nil {
		t.Fatalf("CreateFile(sidecar a): %v", err)
	}

	files, err := store.ListPrimaryFiles(ctx)
	if err != nil {
		t.Fatalf("ListPrimaryFiles(): %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ListPrimaryFiles() = %d rows, want 1", len(files))
	}
	got := files[0]
	if got.PhotoUID != photoA.UID || got.FilePath != "2023/06/prim-a.jpg" || got.FileHash != "prim-a" {
		t.Errorf("primary file = %+v, want uid %s / 2023/06/prim-a.jpg / prim-a", got, photoA.UID)
	}
}

// TestListPhotosMissingPhash verifies only non-archived photos without a
// photo_phashes row are returned, and the limit is honoured.
func TestListPhotosMissingPhash(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	withPhash, err := store.Create(ctx, samplePhoto("has-phash"))
	if err != nil {
		t.Fatalf("Create(has): %v", err)
	}
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: withPhash.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	missing1, err := store.Create(ctx, samplePhoto("no-phash-1"))
	if err != nil {
		t.Fatalf("Create(missing1): %v", err)
	}
	missing2, err := store.Create(ctx, samplePhoto("no-phash-2"))
	if err != nil {
		t.Fatalf("Create(missing2): %v", err)
	}
	// An archived photo without a pHash is excluded.
	archived, err := store.Create(ctx, samplePhoto("archived"))
	if err != nil {
		t.Fatalf("Create(archived): %v", err)
	}
	if _, err := store.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	uids, err := store.ListPhotosMissingPhash(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingPhash(0): %v", err)
	}
	sort.Strings(uids)
	want := []string{missing1.UID, missing2.UID}
	sort.Strings(want)
	if len(uids) != 2 || uids[0] != want[0] || uids[1] != want[1] {
		t.Errorf("ListPhotosMissingPhash(0) = %v, want %v", uids, want)
	}

	limited, err := store.ListPhotosMissingPhash(ctx, 1)
	if err != nil || len(limited) != 1 {
		t.Errorf("ListPhotosMissingPhash(1) = (%v, %v), want one uid", limited, err)
	}
}

// TestListActiveUIDs verifies every non-archived photo is returned regardless of
// whether it has a pHash, while archived photos are excluded — the candidate set
// for the forced full thumbnail backfill.
func TestListActiveUIDs(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if uids, err := store.ListActiveUIDs(ctx); err != nil || len(uids) != 0 {
		t.Fatalf("ListActiveUIDs() on empty = (%v, %v), want ([], nil)", uids, err)
	}

	hashed, err := store.Create(ctx, samplePhoto("active-hashed"))
	if err != nil {
		t.Fatalf("Create(hashed): %v", err)
	}
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: hashed.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	plain, err := store.Create(ctx, samplePhoto("active-plain"))
	if err != nil {
		t.Fatalf("Create(plain): %v", err)
	}
	archived, err := store.Create(ctx, samplePhoto("active-archived"))
	if err != nil {
		t.Fatalf("Create(archived): %v", err)
	}
	if _, err := store.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	uids, err := store.ListActiveUIDs(ctx)
	if err != nil {
		t.Fatalf("ListActiveUIDs(): %v", err)
	}
	sort.Strings(uids)
	want := []string{hashed.UID, plain.UID}
	sort.Strings(want)
	if len(uids) != 2 || uids[0] != want[0] || uids[1] != want[1] {
		t.Errorf("ListActiveUIDs() = %v, want %v (both non-archived, archived excluded)", uids, want)
	}
}
