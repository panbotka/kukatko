//go:build integration

package trash_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/trash"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case,
// so they intentionally do not run in parallel.

// purgeEnv bundles the live collaborators a purge test needs over a freshly
// truncated database and isolated on-disk storage.
type purgeEnv struct {
	store   *photos.Store
	vectors *vectors.Store
	fs      *storage.FS
	thumb   *thumb.Thumbnailer
	svc     *trash.Service
	db      *database.DB
}

// newPurgeEnv wires the purge service over real storage, a real thumbnailer and
// the integration database, with a one-day retention.
func newPurgeEnv(t *testing.T) *purgeEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	thumbnailer := thumb.New(fs, t.TempDir())
	store := photos.NewStore(db.Pool())
	vec := vectors.NewStore(db.Pool())
	svc := trash.New(trash.Config{
		Photos: store, Storage: fs, Thumbnailer: thumbnailer, RetentionDays: 1, BatchSize: 2,
	})
	return &purgeEnv{store: store, vectors: vec, fs: fs, thumb: thumbnailer, svc: svc, db: db}
}

// seedPhoto stores a real JPEG, creates the photo plus a primary file, a phash, a
// thumbnail set and an embedding (to prove the cascade), then stamps archived_at
// to at (nil leaves it live). It returns the photo and its on-disk paths.
func (e *purgeEnv) seedPhoto(t *testing.T, name string, at *time.Time) (photos.Photo, string, string) {
	t.Helper()
	ctx := t.Context()

	// Give each named photo visibly distinct content (a different band of white
	// rows) so its SHA256 file hash is unique even for equal-length names.
	salt := 0
	for i := 0; i < len(name); i++ {
		salt += int(name[i])
	}
	whiteRows := salt%40 + 4
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		band := color.RGBA{R: uint8(salt), G: uint8(y), B: 64, A: 255}
		if y < whiteRows {
			band = color.RGBA{R: 255, G: 255, B: 255, A: 255}
		}
		for x := 0; x < 64; x++ {
			img.Set(x, y, band)
		}
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	stored, err := e.fs.Store(ctx, &buf, time.Time{}, name+".jpg")
	if err != nil {
		t.Fatalf("storage.Store: %v", err)
	}

	photo, err := e.store.Create(ctx, photos.Photo{
		FileHash: stored.Hash, FilePath: stored.RelPath, FileName: name + ".jpg",
		FileSize: stored.Size, FileMime: stored.MIME, FileOrientation: 1, TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}
	if _, err := e.store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: stored.RelPath, FileHash: stored.Hash,
		FileSize: stored.Size, FileMime: stored.MIME, IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("store.CreateFile: %v", err)
	}
	if err := e.store.SetPhash(ctx, photos.Phash{PhotoUID: photo.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("store.SetPhash: %v", err)
	}
	vec := make([]float32, vectors.ImageDim)
	vec[0] = 1
	if _, err := e.vectors.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID: photo.UID, Vector: vec, Model: "m", Pretrained: "p",
	}); err != nil {
		t.Fatalf("vectors.SaveEmbedding: %v", err)
	}
	tileAbs, err := e.thumb.Path(stored.Hash, "tile_224")
	if err != nil {
		t.Fatalf("thumb.Path: %v", err)
	}
	if _, err := e.thumb.GenerateAll(ctx, photo); err != nil {
		t.Fatalf("thumb.GenerateAll: %v", err)
	}

	if at != nil {
		if _, err := e.db.Pool().Exec(ctx,
			"UPDATE photos SET archived_at = $2 WHERE uid = $1", photo.UID, *at); err != nil {
			t.Fatalf("stamp archived_at: %v", err)
		}
	}
	// The returned path is asserted against for the rest of the test, so the
	// original stays materialized until the test ends rather than being released
	// here.
	originalAbs, release, err := e.fs.Materialize(ctx, stored.RelPath)
	if err != nil {
		t.Fatalf("fs.Materialize: %v", err)
	}
	t.Cleanup(release)
	return photo, originalAbs, tileAbs
}

// assertGone fails if any DB row or on-disk artifact for the purged photo
// survives — the no-orphans guarantee.
func (e *purgeEnv) assertGone(t *testing.T, photo photos.Photo, originalAbs, thumbAbs string) {
	t.Helper()
	ctx := t.Context()
	if _, err := e.store.GetByUID(ctx, photo.UID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("photo row survives: err = %v", err)
	}
	if _, err := e.vectors.GetEmbedding(ctx, photo.UID); !errors.Is(err, vectors.ErrEmbeddingNotFound) {
		t.Errorf("embedding row survives (cascade failed): err = %v", err)
	}
	if files, err := e.store.ListFiles(ctx, photo.UID); err != nil || len(files) != 0 {
		t.Errorf("photo_files survive: files=%v err=%v", files, err)
	}
	if _, err := os.Stat(originalAbs); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("original file survives at %s: %v", originalAbs, err)
	}
	if _, err := os.Stat(thumbAbs); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("thumbnail survives at %s: %v", thumbAbs, err)
	}
}

// TestPurgeExpired_removesOnlyExpired confirms the retention purge deletes the
// expired archived photo and all its dependents/files while leaving a recently
// archived photo and a live photo untouched.
func TestPurgeExpired_removesOnlyExpired(t *testing.T) {
	env := newPurgeEnv(t)
	now := time.Now()
	expired := now.Add(-72 * time.Hour)
	recent := now.Add(-time.Hour)

	oldPhoto, oldOrig, oldThumb := env.seedPhoto(t, "old", &expired)
	recentPhoto, _, _ := env.seedPhoto(t, "recent", &recent)
	livePhoto, _, _ := env.seedPhoto(t, "live", nil)

	res, err := env.svc.PurgeExpired(t.Context())
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:1 Failed:0}", res)
	}
	env.assertGone(t, oldPhoto, oldOrig, oldThumb)

	if _, err := env.store.GetByUID(t.Context(), recentPhoto.UID); err != nil {
		t.Errorf("recently archived photo was purged: %v", err)
	}
	if _, err := env.store.GetByUID(t.Context(), livePhoto.UID); err != nil {
		t.Errorf("live photo was purged: %v", err)
	}
}

// TestEmptyTrash_removesAllArchived confirms emptying the trash purges every
// archived photo regardless of age while leaving live photos alone.
func TestEmptyTrash_removesAllArchived(t *testing.T) {
	env := newPurgeEnv(t)
	now := time.Now()
	recent := now.Add(-time.Hour)

	a, aOrig, aThumb := env.seedPhoto(t, "a", &recent)
	b, bOrig, bThumb := env.seedPhoto(t, "b", &recent)
	livePhoto, _, _ := env.seedPhoto(t, "live", nil)

	res, err := env.svc.EmptyTrash(t.Context(), audit.Meta{})
	if err != nil {
		t.Fatalf("EmptyTrash: %v", err)
	}
	if res.Purged != 2 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:2 Failed:0}", res)
	}
	env.assertGone(t, a, aOrig, aThumb)
	env.assertGone(t, b, bOrig, bThumb)
	if _, err := env.store.GetByUID(t.Context(), livePhoto.UID); err != nil {
		t.Errorf("live photo was purged by EmptyTrash: %v", err)
	}
}

// TestPurgeOlderThan_removesOnlyOlder confirms the admin age-bounded purge
// deletes archived photos older than the cutoff (now - days) while leaving
// more-recently archived photos and live photos untouched.
func TestPurgeOlderThan_removesOnlyOlder(t *testing.T) {
	env := newPurgeEnv(t)
	now := time.Now()
	old := now.Add(-200 * 24 * time.Hour) // ~200 days ago → older than a 100-day cutoff
	recent := now.Add(-10 * 24 * time.Hour)

	oldPhoto, oldOrig, oldThumb := env.seedPhoto(t, "old", &old)
	recentPhoto, _, _ := env.seedPhoto(t, "recent", &recent)
	livePhoto, _, _ := env.seedPhoto(t, "live", nil)

	res, err := env.svc.PurgeOlderThan(t.Context(), 100, audit.Meta{})
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Purged != 1 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:1 Failed:0}", res)
	}
	env.assertGone(t, oldPhoto, oldOrig, oldThumb)

	if _, err := env.store.GetByUID(t.Context(), recentPhoto.UID); err != nil {
		t.Errorf("recently archived photo (within the cutoff) was purged: %v", err)
	}
	if _, err := env.store.GetByUID(t.Context(), livePhoto.UID); err != nil {
		t.Errorf("live photo was purged: %v", err)
	}
}

// TestPurgeOlderThan_zeroDaysEmptiesTrash confirms days=0 purges every archived
// photo (cutoff == now), matching the empty-trash behaviour, while leaving live
// photos alone.
func TestPurgeOlderThan_zeroDaysEmptiesTrash(t *testing.T) {
	env := newPurgeEnv(t)
	now := time.Now()
	recent := now.Add(-time.Hour)

	a, aOrig, aThumb := env.seedPhoto(t, "a", &recent)
	b, bOrig, bThumb := env.seedPhoto(t, "b", &recent)
	livePhoto, _, _ := env.seedPhoto(t, "live", nil)

	res, err := env.svc.PurgeOlderThan(t.Context(), 0, audit.Meta{})
	if err != nil {
		t.Fatalf("PurgeOlderThan: %v", err)
	}
	if res.Purged != 2 || res.Failed != 0 {
		t.Fatalf("result = %+v, want {Purged:2 Failed:0}", res)
	}
	env.assertGone(t, a, aOrig, aThumb)
	env.assertGone(t, b, bOrig, bThumb)
	if _, err := env.store.GetByUID(t.Context(), livePhoto.UID); err != nil {
		t.Errorf("live photo was purged by PurgeOlderThan(0): %v", err)
	}
}

// TestPurgePhoto_singleArchived confirms the manual single purge removes one
// archived photo and rejects a live one with ErrNotArchived.
func TestPurgePhoto_singleArchived(t *testing.T) {
	env := newPurgeEnv(t)
	now := time.Now()
	recent := now.Add(-time.Hour)

	archived, origAbs, thumbAbs := env.seedPhoto(t, "arch", &recent)
	livePhoto, _, _ := env.seedPhoto(t, "live", nil)

	if err := env.svc.PurgePhoto(t.Context(), livePhoto.UID, audit.Meta{}); !errors.Is(err, trash.ErrNotArchived) {
		t.Fatalf("PurgePhoto(live) error = %v, want ErrNotArchived", err)
	}
	if err := env.svc.PurgePhoto(t.Context(), archived.UID, audit.Meta{}); err != nil {
		t.Fatalf("PurgePhoto(archived): %v", err)
	}
	env.assertGone(t, archived, origAbs, thumbAbs)

	if err := env.svc.PurgePhoto(t.Context(), "ph_missing", audit.Meta{}); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("PurgePhoto(missing) error = %v, want ErrPhotoNotFound", err)
	}
}
