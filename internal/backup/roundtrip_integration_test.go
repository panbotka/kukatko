//go:build integration

package backup_test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/project"
)

// TestBackupRestore_roundTripsBothLayouts backs up a store holding two
// projects' trees — the bootstrap project's root layout and a created project's
// projects/<uid>/ tree — restores the backup onto an empty originals root, and
// verifies the restored library against a catalogue naming photos of both.
//
// Every layout must come back key for key: the originals, the metadata sidecars
// and each project's genealogy export travel; thumbnails and streaming segments
// (regenerable) and the store-wide dumps and partial uploads do not, in either
// layout. The restored disk then reconciles cleanly against the catalogue,
// because neither the sidecars nor the exports are "extra" files.
func TestBackupRestore_roundTripsBothLayouts(t *testing.T) {
	fix := newBucketFixture(t, 0)
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	created, err := project.NewStore(db.Pool()).Create(t.Context(), audit.Meta{},
		project.CreateParams{Slug: "roundtrip", Name: "Round trip"})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	tree := created.StoragePrefix

	backedUp := map[string]string{
		"2026/01/a.jpg":                     "photo-a",
		"sidecars/2026/01/a.jpg.yml":        "sidecar-a",
		"families.yaml":                     "families-root",
		tree + "2026/03/b.jpg":              "photo-b",
		tree + "sidecars/2026/03/b.jpg.yml": "sidecar-b",
		tree + "families.yaml":              "families-b",
	}
	notBackedUp := []string{
		"thumb/ab/cd/ef/abcdef_tile_224.jpg",
		tree + "thumb/ab/cd/ef/abcdef_tile_224.jpg",
		tree + "hls/abcdef0123456789/1080p/init.mp4",
		".tmp/upload-123",
	}
	for key, content := range backedUp {
		seed(t, fix.primary, key, content)
	}
	for _, key := range notBackedUp {
		seed(t, fix.primary, key, "derived")
	}

	if _, err := fix.service.Run(t.Context(), time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("backup Run: %v", err)
	}

	photoStore := photos.NewStore(db.Pool())
	insertPhoto(t, photoStore, "h-root", "2026/01/a.jpg")
	insertPhotoIn(t, photoStore, created.UID, "h-tree", tree+"2026/03/b.jpg")

	disk := backup.NewDiskOriginals(t.TempDir())
	restore := backup.NewRestoreService(backup.RestoreConfig{
		Objects: fix.backupStore, Photos: photoStore, Originals: disk,
	})
	res, err := restore.RestoreOriginals(t.Context())
	if err != nil {
		t.Fatalf("RestoreOriginals: %v", err)
	}
	if res.Downloaded != len(backedUp) {
		t.Errorf("restored %d objects, want %d", res.Downloaded, len(backedUp))
	}

	restored, err := disk.List(t.Context())
	if err != nil {
		t.Fatalf("listing the restored disk: %v", err)
	}
	got := make([]string, 0, len(restored))
	for _, f := range restored {
		got = append(got, f.Key)
	}
	sort.Strings(got)
	want := make([]string, 0, len(backedUp))
	for key := range backedUp {
		want = append(want, key)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("restored keys = %v, want %v", got, want)
	}

	report, err := restore.Verify(t.Context())
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Consistent || report.OriginalsOnDisk != 2 || report.FilesInDB != 2 {
		t.Errorf("Verify() = %+v, want a consistent library of 2 originals in 2 layouts", report)
	}
}

// insertPhotoIn inserts a photo of project projectUID and its primary file at
// the given storage key.
func insertPhotoIn(t *testing.T, store *photos.Store, projectUID, hash, key string) {
	t.Helper()
	ctx := t.Context()
	photo, err := store.Create(ctx, photos.Photo{
		ProjectID:     projectUID,
		FileHash:      hash,
		FilePath:      key,
		FileName:      hash + ".jpg",
		FileSize:      10,
		FileMime:      "image/jpeg",
		MediaType:     photos.MediaImage,
		TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("Create(%s): %v", hash, err)
	}
	if _, err := store.CreateFile(ctx, projectUID, photos.PhotoFile{
		PhotoUID:  photo.UID,
		FilePath:  key,
		FileHash:  hash,
		FileSize:  10,
		FileMime:  "image/jpeg",
		IsPrimary: true,
		Role:      photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("CreateFile(%s): %v", hash, err)
	}
}
