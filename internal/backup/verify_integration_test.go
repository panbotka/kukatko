//go:build integration

package backup_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/backup"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// nilObjectStore is a backup.ObjectStore that does nothing; Verify never touches
// the bucket, but NewRestoreService requires a non-nil Objects.
type nilObjectStore struct{}

func (nilObjectStore) Stat(context.Context, string) (backup.Object, bool, error) {
	return backup.Object{}, false, nil
}
func (nilObjectStore) Put(context.Context, string, io.Reader, int64, string) error { return nil }
func (nilObjectStore) CopyFrom(context.Context, string, string, string) error      { return nil }
func (nilObjectStore) Open(context.Context, string) (io.ReadCloser, error)         { return nil, nil }
func (nilObjectStore) List(context.Context, string) ([]backup.Object, error)       { return nil, nil }
func (nilObjectStore) Remove(context.Context, string) error                        { return nil }

// insertPhoto inserts a photo and its primary file at the given storage key.
func insertPhoto(t *testing.T, store *photos.Store, hash, key string) {
	t.Helper()
	ctx := t.Context()
	photo, err := store.Create(ctx, photos.Photo{
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
	if _, err := store.CreateFile(ctx, photos.PhotoFile{
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

// TestVerify_countsAndMismatches checks the post-restore integrity report
// against a real database and a real originals directory: it must count photos
// and files, and surface both a catalogued file missing on disk and a file on
// disk with no catalogue row.
func TestVerify_countsAndMismatches(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := photos.NewStore(db.Pool())

	insertPhoto(t, store, "h-present", "2026/01/present.jpg")
	insertPhoto(t, store, "h-missing", "2026/01/missing.jpg") // catalogued, absent on disk

	root := t.TempDir()
	disk := backup.NewDiskOriginals(root)
	if err := disk.Write(t.Context(), "2026/01/present.jpg", strings.NewReader("present")); err != nil {
		t.Fatalf("seed present.jpg: %v", err)
	}
	if err := disk.Write(t.Context(), "2026/01/stray.jpg", strings.NewReader("stray")); err != nil {
		t.Fatalf("seed stray.jpg: %v", err)
	}

	svc := backup.NewRestoreService(backup.RestoreConfig{
		Objects:   nilObjectStore{},
		Photos:    store,
		Originals: disk,
	})
	report, err := svc.Verify(t.Context())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}

	if report.PhotosInDB != 2 {
		t.Errorf("PhotosInDB = %d, want 2", report.PhotosInDB)
	}
	if report.FilesInDB != 2 {
		t.Errorf("FilesInDB = %d, want 2", report.FilesInDB)
	}
	if report.OriginalsOnDisk != 2 {
		t.Errorf("OriginalsOnDisk = %d, want 2", report.OriginalsOnDisk)
	}
	if len(report.MissingOnDisk) != 1 || report.MissingOnDisk[0] != "2026/01/missing.jpg" {
		t.Errorf("MissingOnDisk = %v, want [2026/01/missing.jpg]", report.MissingOnDisk)
	}
	if len(report.ExtraOnDisk) != 1 || report.ExtraOnDisk[0] != "2026/01/stray.jpg" {
		t.Errorf("ExtraOnDisk = %v, want [2026/01/stray.jpg]", report.ExtraOnDisk)
	}
	if report.Consistent {
		t.Error("Consistent = true despite mismatches")
	}
}

// TestVerify_consistent checks that a catalogue exactly matching the originals
// on disk reports as consistent with no mismatches.
func TestVerify_consistent(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	store := photos.NewStore(db.Pool())

	insertPhoto(t, store, "h-a", "2026/02/a.jpg")
	insertPhoto(t, store, "h-b", "2026/02/b.jpg")

	root := t.TempDir()
	disk := backup.NewDiskOriginals(root)
	for _, key := range []string{"2026/02/a.jpg", "2026/02/b.jpg"} {
		if err := disk.Write(t.Context(), key, strings.NewReader(key)); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	svc := backup.NewRestoreService(backup.RestoreConfig{
		Objects:   nilObjectStore{},
		Photos:    store,
		Originals: disk,
	})
	report, err := svc.Verify(t.Context())
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !report.Consistent {
		t.Errorf("Consistent = false, want true; report = %+v", report)
	}
	if len(report.MissingOnDisk) != 0 || len(report.ExtraOnDisk) != 0 {
		t.Errorf("unexpected mismatches: missing=%v extra=%v", report.MissingOnDisk, report.ExtraOnDisk)
	}
}
