//go:build integration

package photos_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStore returns a photos.Store over a freshly truncated integration database.
func newStore(t *testing.T) (*photos.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return photos.NewStore(db.Pool()), db
}

// samplePhoto builds a Photo with a distinct file hash and a representative set
// of populated optional fields.
func samplePhoto(hash string) photos.Photo {
	taken := time.Date(2023, 6, 1, 12, 0, 0, 0, time.UTC)
	return photos.Photo{
		FileHash:        hash,
		FilePath:        "2023/06/" + hash + ".jpg",
		FileName:        hash + ".jpg",
		FileSize:        1234,
		FileMime:        "image/jpeg",
		FileWidth:       4000,
		FileHeight:      3000,
		FileOrientation: 1,
		TakenAt:         &taken,
		TakenAtSource:   "exif",
		Title:           "Beach",
		Lat:             new(50.08),
		Lng:             new(14.42),
		Altitude:        new(220.5),
		CameraMake:      "Canon",
		CameraModel:     "EOS R6",
		ISO:             new(100),
		Aperture:        new(2.8),
		Exposure:        "1/250",
		FocalLength:     new(35.0),
		Exif:            json.RawMessage(`{"Make":"Canon","ISO":100}`),
	}
}

// TestPhotoLifecycle exercises create, read, metadata update and archive/unarchive.
func TestPhotoLifecycle(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	created, err := store.Create(ctx, samplePhoto("aaa1"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.UID == "" {
		t.Fatal("Create did not assign a UID")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Errorf("Create did not populate timestamps: %+v", created)
	}

	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.Title != "Beach" || got.Lat == nil || *got.Lat != 50.08 || got.ISO == nil || *got.ISO != 100 {
		t.Errorf("round-tripped photo mismatch: %+v", got)
	}
	if string(got.Exif) == "" {
		t.Errorf("exif not round-tripped: %q", got.Exif)
	}

	byHash, err := store.GetByFileHash(ctx, "aaa1")
	if err != nil || byHash.UID != created.UID {
		t.Fatalf("GetByFileHash = %+v, %v", byHash, err)
	}

	newTaken := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	updated, err := store.UpdateMetadata(ctx, created.UID, photos.MetadataUpdate{
		Title:         "Sunset",
		Description:   "On the coast",
		Notes:         "keeper",
		TakenAt:       &newTaken,
		TakenAtSource: "manual",
		Lat:           new(49.0),
		Lng:           nil,
		Private:       true,
	})
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if updated.Title != "Sunset" || !updated.Private || updated.Lng != nil {
		t.Errorf("UpdateMetadata result mismatch: %+v", updated)
	}
	if updated.Lat == nil || *updated.Lat != 49.0 {
		t.Errorf("UpdateMetadata lat = %v, want 49.0", updated.Lat)
	}

	archived, err := store.Archive(ctx, created.UID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Error("Archive did not set archived_at")
	}
	unarchived, err := store.Unarchive(ctx, created.UID)
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if unarchived.ArchivedAt != nil {
		t.Errorf("Unarchive did not clear archived_at: %v", unarchived.ArchivedAt)
	}
}

// TestPhotoNotFound verifies the sentinel for missing rows across the getters.
func TestPhotoNotFound(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.GetByUID(ctx, "missing"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("GetByUID error = %v, want ErrPhotoNotFound", err)
	}
	if _, err := store.UpdateMetadata(ctx, "missing", photos.MetadataUpdate{}); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("UpdateMetadata error = %v, want ErrPhotoNotFound", err)
	}
	if _, err := store.Archive(ctx, "missing"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Archive error = %v, want ErrPhotoNotFound", err)
	}
	if err := store.Delete(ctx, "missing"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("Delete error = %v, want ErrPhotoNotFound", err)
	}
}

// TestFileHashUnique verifies the dedup constraint on photos.file_hash.
func TestFileHashUnique(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, err := store.Create(ctx, samplePhoto("dup")); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := store.Create(ctx, samplePhoto("dup")); !errors.Is(err, photos.ErrFileHashTaken) {
		t.Errorf("second Create error = %v, want ErrFileHashTaken", err)
	}
}

// TestMediaTypeRoundTrip verifies the video columns persist and read back, and
// that an unset media type defaults to image.
func TestMediaTypeRoundTrip(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	// An image sample with no explicit media type defaults to image.
	img, err := store.Create(ctx, samplePhoto("img"))
	if err != nil {
		t.Fatalf("Create image: %v", err)
	}
	if img.MediaType != photos.MediaImage {
		t.Errorf("default MediaType = %q, want image", img.MediaType)
	}

	clip := samplePhoto("vid")
	clip.FilePath = "2023/06/vid.mp4"
	clip.FileName = "vid.mp4"
	clip.FileMime = "video/mp4"
	clip.MediaType = photos.MediaVideo
	clip.DurationMs = new(5312)
	clip.VideoCodec = "h264"
	clip.AudioCodec = "aac"
	clip.HasAudio = true
	clip.FPS = new(29.97)

	created, err := store.Create(ctx, clip)
	if err != nil {
		t.Fatalf("Create video: %v", err)
	}
	got, err := store.GetByUID(ctx, created.UID)
	if err != nil {
		t.Fatalf("GetByUID: %v", err)
	}
	if got.MediaType != photos.MediaVideo {
		t.Errorf("MediaType = %q, want video", got.MediaType)
	}
	if got.DurationMs == nil || *got.DurationMs != 5312 {
		t.Errorf("DurationMs = %v, want 5312", got.DurationMs)
	}
	if got.VideoCodec != "h264" || got.AudioCodec != "aac" || !got.HasAudio {
		t.Errorf("codec/audio mismatch: %+v", got)
	}
	if got.FPS == nil || *got.FPS != 29.97 {
		t.Errorf("FPS = %v, want 29.97", got.FPS)
	}
}

// TestExternalIDLookups verifies dedup/migration lookups by external IDs.
func TestExternalIDLookups(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	p := samplePhoto("ext")
	p.PhotoprismUID = new("pp0123456789abcd")
	p.PhotoprismFileHash = new("da39a3ee5e6b4b0d3255bfef95601890afd80709")
	p.PhotosorterUID = new("ps_legacy_001")
	created, err := store.Create(ctx, p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	byPP, err := store.GetByPhotoprismUID(ctx, "pp0123456789abcd")
	if err != nil || byPP.UID != created.UID {
		t.Errorf("GetByPhotoprismUID = %+v, %v", byPP, err)
	}
	if byPP.PhotoprismFileHash == nil || *byPP.PhotoprismFileHash != *p.PhotoprismFileHash {
		t.Errorf("photoprism_file_hash not round-tripped: %v", byPP.PhotoprismFileHash)
	}
	byPS, err := store.GetByPhotosorterUID(ctx, "ps_legacy_001")
	if err != nil || byPS.UID != created.UID {
		t.Errorf("GetByPhotosorterUID = %+v, %v", byPS, err)
	}

	if _, err := store.GetByPhotoprismUID(ctx, "nope"); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("GetByPhotoprismUID(nope) = %v, want ErrPhotoNotFound", err)
	}
}

// TestList exercises the filtering, sorting and pagination scaffold.
func TestList(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	for _, h := range []string{"l1", "l2", "l3"} {
		if _, err := store.Create(ctx, samplePhoto(h)); err != nil {
			t.Fatalf("Create %s: %v", h, err)
		}
	}
	archived, err := store.Create(ctx, samplePhoto("l4"))
	if err != nil {
		t.Fatalf("Create l4: %v", err)
	}
	if _, err := store.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive l4: %v", err)
	}

	live, err := store.List(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("List live: %v", err)
	}
	if len(live) != 3 {
		t.Errorf("List live count = %d, want 3", len(live))
	}

	onlyArchived, err := store.List(ctx, photos.ListParams{OnlyArchived: true})
	if err != nil {
		t.Fatalf("List archived: %v", err)
	}
	if len(onlyArchived) != 1 || onlyArchived[0].UID != archived.UID {
		t.Errorf("List archived = %+v, want only %s", onlyArchived, archived.UID)
	}

	page, err := store.List(ctx, photos.ListParams{IncludeArchived: true, Limit: 2})
	if err != nil {
		t.Fatalf("List paged: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("List paged count = %d, want 2", len(page))
	}
}

// TestPhotoFiles verifies file creation, listing and the one-primary constraint.
func TestPhotoFiles(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo, err := store.Create(ctx, samplePhoto("files"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	primary, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: "2023/06/files.jpg", FileHash: "files",
		FileMime: "image/jpeg", IsPrimary: true, Role: photos.RoleOriginal,
	})
	if err != nil {
		t.Fatalf("CreateFile primary: %v", err)
	}
	if primary.ID == 0 {
		t.Error("CreateFile did not assign an id")
	}

	if _, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: "2023/06/files.xmp", Role: photos.RoleSidecar,
	}); err != nil {
		t.Fatalf("CreateFile sidecar: %v", err)
	}

	// Same path again violates the (photo_uid, file_path) unique constraint.
	if _, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: "2023/06/files.jpg", Role: photos.RoleEdited,
	}); !errors.Is(err, photos.ErrFilePathTaken) {
		t.Errorf("duplicate path error = %v, want ErrFilePathTaken", err)
	}

	// A second primary violates the one-primary-per-photo index.
	if _, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: "2023/06/files-edited.jpg",
		IsPrimary: true, Role: photos.RoleEdited,
	}); !errors.Is(err, photos.ErrPrimaryFileExists) {
		t.Errorf("second primary error = %v, want ErrPrimaryFileExists", err)
	}

	files, err := store.ListFiles(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 2 || !files[0].IsPrimary {
		t.Errorf("ListFiles = %+v, want 2 files primary-first", files)
	}
}

// TestPhashAndEditRoundTrip verifies the phash and edit upsert/read paths,
// including the all-or-nothing crop.
func TestPhashAndEditRoundTrip(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo, err := store.Create(ctx, samplePhoto("ph"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: photo.UID, Phash: 111, Dhash: 222}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	// Upsert replaces the row.
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: photo.UID, Phash: 333, Dhash: 444}); err != nil {
		t.Fatalf("SetPhash update: %v", err)
	}
	gotPhash, err := store.GetPhash(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetPhash: %v", err)
	}
	if gotPhash.Phash != 333 || gotPhash.Dhash != 444 {
		t.Errorf("GetPhash = %+v, want phash 333 dhash 444", gotPhash)
	}

	// Crop coordinates use values exactly representable as float32 (REAL) so
	// they round-trip without precision loss.
	edit := photos.Edit{
		PhotoUID: photo.UID,
		CropX:    new(0.25), CropY: new(0.25), CropW: new(0.5), CropH: new(0.5),
		Rotation: 90, Brightness: 0.5, Contrast: -0.25,
	}
	if err := store.SetEdit(ctx, edit); err != nil {
		t.Fatalf("SetEdit: %v", err)
	}
	gotEdit, err := store.GetEdit(ctx, photo.UID)
	if err != nil {
		t.Fatalf("GetEdit: %v", err)
	}
	if gotEdit.Rotation != 90 || gotEdit.CropW == nil || *gotEdit.CropW != 0.5 {
		t.Errorf("GetEdit = %+v, want rotation 90 crop_w 0.5", gotEdit)
	}

	if _, err := store.GetPhash(ctx, "missing"); !errors.Is(err, photos.ErrPhashNotFound) {
		t.Errorf("GetPhash(missing) = %v, want ErrPhashNotFound", err)
	}
	if _, err := store.GetEdit(ctx, "missing"); !errors.Is(err, photos.ErrEditNotFound) {
		t.Errorf("GetEdit(missing) = %v, want ErrEditNotFound", err)
	}
}

// TestNearestPhash verifies the perceptual-hash nearest-neighbour query returns
// the closest photo by Hamming distance and reports ErrPhashNotFound on an empty
// table.
func TestNearestPhash(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	if _, _, err := store.NearestPhash(ctx, 0); !errors.Is(err, photos.ErrPhashNotFound) {
		t.Fatalf("NearestPhash on empty table = %v, want ErrPhashNotFound", err)
	}

	near, err := store.Create(ctx, samplePhoto("near"))
	if err != nil {
		t.Fatalf("Create near: %v", err)
	}
	far, err := store.Create(ctx, samplePhoto("far"))
	if err != nil {
		t.Fatalf("Create far: %v", err)
	}
	// near.phash = 0b000 (distance 1 from query 0b001); far.phash = 0b1111111
	// (distance 6 from query).
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: near.UID, Phash: 0, Dhash: 0}); err != nil {
		t.Fatalf("SetPhash near: %v", err)
	}
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: far.UID, Phash: 0b1111111, Dhash: 0}); err != nil {
		t.Fatalf("SetPhash far: %v", err)
	}

	uid, distance, err := store.NearestPhash(ctx, 0b1)
	if err != nil {
		t.Fatalf("NearestPhash: %v", err)
	}
	if uid != near.UID || distance != 1 {
		t.Errorf("NearestPhash = (%q, %d), want (%q, 1)", uid, distance, near.UID)
	}
}

// TestCascadeDelete verifies deleting a photo removes its files, phashes and
// edits via ON DELETE CASCADE.
func TestCascadeDelete(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	photo, err := store.Create(ctx, samplePhoto("casc"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.CreateFile(ctx, photos.PhotoFile{
		PhotoUID: photo.UID, FilePath: "2023/06/casc.jpg", IsPrimary: true, Role: photos.RoleOriginal,
	}); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if err := store.SetPhash(ctx, photos.Phash{PhotoUID: photo.UID, Phash: 1, Dhash: 2}); err != nil {
		t.Fatalf("SetPhash: %v", err)
	}
	if err := store.SetEdit(ctx, photos.Edit{PhotoUID: photo.UID, Rotation: 180}); err != nil {
		t.Fatalf("SetEdit: %v", err)
	}

	if err := store.Delete(ctx, photo.UID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := store.GetByUID(ctx, photo.UID); !errors.Is(err, photos.ErrPhotoNotFound) {
		t.Errorf("photo still present after delete: %v", err)
	}
	files, err := store.ListFiles(ctx, photo.UID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("files not cascaded: %+v", files)
	}
	if _, err := store.GetPhash(ctx, photo.UID); !errors.Is(err, photos.ErrPhashNotFound) {
		t.Errorf("phash not cascaded: %v", err)
	}
	if _, err := store.GetEdit(ctx, photo.UID); !errors.Is(err, photos.ErrEditNotFound) {
		t.Errorf("edit not cascaded: %v", err)
	}
}
