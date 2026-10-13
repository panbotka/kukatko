//go:build integration

package photos_test

import (
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/projecttest"
)

// TestCountPhotosAndListFilePaths exercises the post-restore integrity helpers:
// CountPhotos returns the total catalogue size (including archived photos) and
// ListFilePaths returns every photo_files key.
func TestCountPhotosAndListFilePaths(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if count, err := store.CountPhotos(ctx); err != nil || count != 0 {
		t.Fatalf("CountPhotos() on empty = (%d, %v), want (0, nil)", count, err)
	}
	if paths, err := store.ListFilePaths(ctx); err != nil || len(paths) != 0 {
		t.Fatalf("ListFilePaths() on empty = (%v, %v), want ([], nil)", paths, err)
	}

	created, err := store.Create(ctx, samplePhoto("count-a"))
	if err != nil {
		t.Fatalf("Create(a): %v", err)
	}
	if _, err := store.CreateFile(ctx, project.BootstrapUID, photos.PhotoFile{
		PhotoUID:  created.UID,
		FilePath:  "2023/06/count-a.jpg",
		FileHash:  "count-a",
		FileSize:  10,
		FileMime:  "image/jpeg",
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("CreateFile(primary a): %v", err)
	}
	if _, err := store.CreateFile(ctx, project.BootstrapUID, photos.PhotoFile{
		PhotoUID:  created.UID,
		FilePath:  "2023/06/sidecar-a.mov",
		FileHash:  "sidecar-a",
		FileSize:  20,
		FileMime:  "video/quicktime",
		IsPrimary: false,
		Role:      photos.RoleSidecar,
	}); err != nil {
		t.Fatalf("CreateFile(sidecar): %v", err)
	}
	secondPhoto, err := store.Create(ctx, samplePhoto("count-b"))
	if err != nil {
		t.Fatalf("Create(b): %v", err)
	}
	if _, err := store.CreateFile(ctx, project.BootstrapUID, photos.PhotoFile{
		PhotoUID:  secondPhoto.UID,
		FilePath:  "2023/06/count-b.jpg",
		FileHash:  "count-b",
		FileSize:  10,
		FileMime:  "image/jpeg",
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("CreateFile(primary b): %v", err)
	}
	// Archiving must not drop the photo from the count; its original still exists.
	if _, err := store.Archive(ctx, project.BootstrapUID, created.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	count, err := store.CountPhotos(ctx)
	if err != nil {
		t.Fatalf("CountPhotos(): %v", err)
	}
	if count != 2 {
		t.Errorf("CountPhotos() = %d, want 2 (archived included)", count)
	}

	paths, err := store.ListFilePaths(ctx)
	if err != nil {
		t.Fatalf("ListFilePaths(): %v", err)
	}
	sort.Strings(paths)
	want := []string{"2023/06/count-a.jpg", "2023/06/count-b.jpg", "2023/06/sidecar-a.mov"}
	if len(paths) != len(want) {
		t.Fatalf("ListFilePaths() = %v, want %v", paths, want)
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("ListFilePaths()[%d] = %q, want %q", i, paths[i], want[i])
		}
	}
}

// TestListCataloguedFiles verifies every photo_files row of every project comes
// back with the project of the photo that owns it, ordered by key — the shape the
// per-project maintenance scan reconciles against.
func TestListCataloguedFiles(t *testing.T) {
	store, db := newStore(t)
	f := projecttest.Seed(t, db.Pool())

	files, err := store.ListCataloguedFiles(t.Context())
	if err != nil {
		t.Fatalf("ListCataloguedFiles(): %v", err)
	}
	got := map[string]string{}
	for _, file := range files {
		got[file.FilePath] = file.ProjectUID
	}
	want := map[string]string{
		"2026/10/" + f.A.PhotoUID + ".jpg": f.A.ProjectUID,
		"2026/10/" + f.B.PhotoUID + ".jpg": f.B.ProjectUID,
	}
	if len(got) != len(want) {
		t.Fatalf("ListCataloguedFiles() = %+v, want %+v", files, want)
	}
	for key, projectUID := range want {
		if got[key] != projectUID {
			t.Errorf("file %s project = %q, want %q", key, got[key], projectUID)
		}
	}
	if !sort.SliceIsSorted(files, func(i, j int) bool { return files[i].FilePath < files[j].FilePath }) {
		t.Errorf("ListCataloguedFiles() not ordered by key: %+v", files)
	}
}
