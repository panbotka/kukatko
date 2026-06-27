//go:build integration

package psimport_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importer"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/photosorter"
	"github.com/panbotka/kukatko/internal/psimport"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They seed a fake photo-sorter schema (ps_fixture)
// alongside Kukátko's own tables and point a read-only photosorter.Reader at it.
// They share one database and truncate between cases, so they do not run in
// parallel.

// psEnv bundles a ready migration service over a freshly truncated database, a
// seeded (empty) fake photo-sorter schema, temp-backed storage and a temp dir for
// originals referenced by photo-sorter file_path values.
type psEnv struct {
	svc    *psimport.Service
	reader *photosorter.Reader
	db     *database.DB
	pool   *pgxpool.Pool
	tmpDir string
}

// newPSEnv builds a migration service wired to real Kukátko stores, storage and
// thumbnailer over the integration database, with a read-only photo-sorter reader
// scoped (via search_path) to the freshly created fixture schema.
func newPSEnv(t *testing.T) *psEnv {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	setupFixtureSchema(t, pool)

	reader, err := photosorter.New(t.Context(), photosorter.Config{
		DSN: os.Getenv(dbtest.EnvTestDatabaseURL), Schema: psSchema,
	})
	if err != nil {
		t.Fatalf("opening photo-sorter reader: %v", err)
	}
	t.Cleanup(reader.Close)

	store, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	svc := psimport.New(psimport.Config{
		Source: reader, Runs: importer.NewStore(pool), Photos: photos.NewStore(pool),
		Vectors: vectors.NewStore(pool), People: people.NewStore(pool),
		Albums: organize.NewStore(pool), Labels: organize.NewStore(pool),
		Storage: store, Thumbnailer: thumb.New(store, t.TempDir()),
		Enqueuer: jobs.NewEnqueuer(jobs.NewStore(pool)), PageSize: 50,
	})
	return &psEnv{svc: svc, reader: reader, db: db, pool: pool, tmpDir: t.TempDir()}
}

// writeOriginal encodes a distinct JPEG for shade, writes it under the env temp
// dir and returns its on-disk path and SHA256 hex (matching what the migration
// computes when it copies the original).
func (e *psEnv) writeOriginal(t *testing.T, name string, shade uint8) (string, string) {
	t.Helper()
	data := jpegOf(shade)
	path := filepath.Join(e.tmpDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing original: %v", err)
	}
	return path, sha256Hex(data)
}

// countRows returns the number of rows in a public-schema table.
func (e *psEnv) countRows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// jpegOf encodes a solid 8x8 JPEG of the given grey shade, giving each photo
// distinct bytes (and thus a distinct SHA256).
func jpegOf(shade uint8) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.RGBA{R: shade, G: shade, B: shade, A: 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// sha256Hex returns the hex SHA256 of b.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestIntegration_fullMigration verifies a first migration creates a new photo,
// matches an already-catalogued one by file_hash, transfers embeddings and faces
// 1:1, and maps subjects, markers, albums, labels, phashes and edits.
func TestIntegration_fullMigration(t *testing.T) {
	env := newPSEnv(t)
	ctx := t.Context()
	existingUID := seedFullScenario(t, env)

	result, err := env.svc.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if result.Counts.Imported != 1 || result.Counts.Skipped != 1 || result.Counts.Failed != 0 {
		t.Fatalf("counts = %+v, want imported 1 skipped 1 failed 0", result.Counts)
	}
	if got := env.countRows(t, "photos"); got != 2 {
		t.Fatalf("photos = %d, want 2 (one created, one matched)", got)
	}
	newUID := assertPhotoStamped(t, env, "psp1")
	assertMatchedPhoto(t, env, "psp2", existingUID)
	assertEmbeddingTransferred(t, env, newUID)
	subjectUID := assertSubjectMapped(t, env)
	assertFaceTransferred(t, env, newUID, subjectUID)
	assertMarkerPreserved(t, env, newUID)
	assertAlbumLabelMapped(t, env, newUID)
	assertPhashEdit(t, env, newUID)
}

// seedFullScenario populates the fixture with one new photo (psp1, with an
// embedding, a face, a subject/marker, album, label, phash and edit) and one
// photo (psp2) whose content already exists in Kukátko. It returns the existing
// Kukátko photo's UID so the matched-by-hash path can be asserted.
func seedFullScenario(t *testing.T, env *psEnv) string {
	t.Helper()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	path1, hash1 := env.writeOriginal(t, "psp1.jpg", 40)
	seedPSPhoto(t, env.pool, psPhoto{
		uid: "psp1", fileHash: hash1, filePath: path1, fileName: "psp1.jpg",
		fileMime: "image/jpeg", title: "New", takenAt: t0, updatedAt: t0,
	})
	seedPSEmbedding(t, env.pool, "psp1", unitVector(768, 1), "ViT-L-14", "openai")
	seedPSFace(t, env.pool, psFace{
		photoUID: "psp1", faceIndex: 0, vec: unitVector(512, 3),
		bbox: []float64{0.1, 0.1, 0.2, 0.2}, detScore: 0.95, model: "buffalo_l",
		markerUID: new("psmk1"), subjectUID: new("pssu1"), width: 8, height: 8,
	})
	seedPSFacesProcessed(t, env.pool, "psp1", 1)
	seedPSSubject(t, env.pool, "pssu1", "Alice", "person")
	seedPSMarker(t, env.pool, "psmk1", "psp1", "pssu1")
	seedPSAlbum(t, env.pool, "psal1", "Holiday", "psp1")
	seedPSLabel(t, env.pool, "pslb1", "Beach", "psp1")
	seedPSPhash(t, env.pool, "psp1", 111, 222)
	seedPSEdit(t, env.pool, "psp1", 90)
	seedIgnoredTables(t, env.pool)

	return seedExistingMatch(t, env, t0)
}

// seedExistingMatch pre-creates a Kukátko photo and a fixture photo (psp2)
// sharing its SHA256, so the migration matches by file_hash instead of copying.
func seedExistingMatch(t *testing.T, env *psEnv, t0 time.Time) string {
	t.Helper()
	bytes2 := jpegOf(80)
	hash2 := sha256Hex(bytes2)
	existing, err := photos.NewStore(env.pool).Create(t.Context(), photos.Photo{
		FileHash: hash2, FilePath: "2020/01/seed.jpg", FileName: "seed.jpg",
		FileMime: "image/jpeg", MediaType: photos.MediaImage, TakenAtSource: "unknown",
	})
	if err != nil {
		t.Fatalf("seeding existing photo: %v", err)
	}
	seedPSPhoto(t, env.pool, psPhoto{
		uid: "psp2", fileHash: hash2, filePath: "/does/not/exist.jpg", fileName: "seed.jpg",
		fileMime: "image/jpeg", title: "Matched", takenAt: t0, updatedAt: t0.Add(time.Hour),
	})
	seedPSEmbedding(t, env.pool, "psp2", unitVector(768, 2), "ViT-L-14", "openai")
	return existing.UID
}

// assertPhotoStamped checks the fixture photo psUID was catalogued with its
// photosorter_uid set and returns its Kukátko UID.
func assertPhotoStamped(t *testing.T, env *psEnv, psUID string) string {
	t.Helper()
	photo, err := photos.NewStore(env.pool).GetByPhotosorterUID(t.Context(), psUID)
	if err != nil {
		t.Fatalf("GetByPhotosorterUID(%s): %v", psUID, err)
	}
	if photo.PhotosorterUID == nil || *photo.PhotosorterUID != psUID {
		t.Errorf("photosorter_uid = %v, want %s", photo.PhotosorterUID, psUID)
	}
	return photo.UID
}

// assertMatchedPhoto checks the fixture photo psUID matched the pre-existing
// Kukátko photo (no new row) and stamped its photosorter_uid onto it.
func assertMatchedPhoto(t *testing.T, env *psEnv, psUID, existingUID string) {
	t.Helper()
	photo, err := photos.NewStore(env.pool).GetByPhotosorterUID(t.Context(), psUID)
	if err != nil {
		t.Fatalf("GetByPhotosorterUID(%s): %v", psUID, err)
	}
	if photo.UID != existingUID {
		t.Errorf("matched photo uid = %s, want existing %s", photo.UID, existingUID)
	}
}

// assertEmbeddingTransferred checks the CLIP embedding moved 1:1 (model preserved)
// and is queryable by cosine similarity.
func assertEmbeddingTransferred(t *testing.T, env *psEnv, kkUID string) {
	t.Helper()
	store := vectors.NewStore(env.pool)
	emb, err := store.GetEmbedding(t.Context(), kkUID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if emb.Model != "ViT-L-14" || emb.Pretrained != "openai" {
		t.Errorf("embedding tags = %q/%q, want ViT-L-14/openai", emb.Model, emb.Pretrained)
	}
	matches, err := store.FindSimilar(t.Context(), unitVector(768, 1), 5, 0)
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	if len(matches) == 0 || matches[0].PhotoUID != kkUID {
		t.Errorf("nearest embedding = %+v, want %s", matches, kkUID)
	}
}

// assertSubjectMapped checks the photo-sorter subject was find-or-created as a
// Kukátko subject and returns its UID.
func assertSubjectMapped(t *testing.T, env *psEnv) string {
	t.Helper()
	subject, err := people.NewStore(env.pool).GetSubjectBySlug(t.Context(), people.Slugify("Alice"))
	if err != nil {
		t.Fatalf("subject not mapped: %v", err)
	}
	return subject.UID
}

// assertFaceTransferred checks the face moved 1:1 (bbox, det_score), is queryable
// by similarity, and had its cached subject UID remapped onto Kukátko's subject.
func assertFaceTransferred(t *testing.T, env *psEnv, kkUID, subjectUID string) {
	t.Helper()
	store := vectors.NewStore(env.pool)
	faces, err := store.ListFaces(t.Context(), kkUID)
	if err != nil || len(faces) != 1 {
		t.Fatalf("ListFaces = %d faces (err %v), want 1", len(faces), err)
	}
	if faces[0].BBox != [4]float64{0.1, 0.1, 0.2, 0.2} || faces[0].DetScore != 0.95 {
		t.Errorf("face bbox/score = %v/%v", faces[0].BBox, faces[0].DetScore)
	}
	if faces[0].SubjectUID == nil || *faces[0].SubjectUID != subjectUID {
		t.Errorf("face subject = %v, want remapped %s", faces[0].SubjectUID, subjectUID)
	}
	matches, err := store.FindSimilarFaces(t.Context(), unitVector(512, 3), 5, 0)
	if err != nil {
		t.Fatalf("FindSimilarFaces: %v", err)
	}
	if len(matches) == 0 || matches[0].PhotoUID != kkUID {
		t.Errorf("nearest face = %+v, want %s", matches, kkUID)
	}
}

// assertMarkerPreserved checks the photo-sorter marker was migrated under its
// original UID (so the migrated faces' cached marker_uid stays valid).
func assertMarkerPreserved(t *testing.T, env *psEnv, kkUID string) {
	t.Helper()
	marker, err := people.NewStore(env.pool).GetMarkerByUID(t.Context(), "psmk1")
	if err != nil {
		t.Fatalf("marker not preserved: %v", err)
	}
	if marker.PhotoUID != kkUID {
		t.Errorf("marker photo = %s, want %s", marker.PhotoUID, kkUID)
	}
}

// assertAlbumLabelMapped checks the album and label were find-or-created and the
// migrated photo is a member of each.
func assertAlbumLabelMapped(t *testing.T, env *psEnv, kkUID string) {
	t.Helper()
	store := organize.NewStore(env.pool)
	album, err := store.GetAlbumBySlug(t.Context(), "holiday")
	if err != nil {
		t.Fatalf("album not mapped: %v", err)
	}
	assertMembership(t, "album", kkUID, func() ([]string, error) {
		return store.ListPhotoUIDs(t.Context(), album.UID)
	})
	label, err := store.GetLabelBySlug(t.Context(), "beach")
	if err != nil {
		t.Fatalf("label not mapped: %v", err)
	}
	assertMembership(t, "label", kkUID, func() ([]string, error) {
		return store.ListPhotoUIDsByLabel(t.Context(), label.UID)
	})
}

// assertMembership fails unless kkUID appears in the membership list produced by
// list, labelling failures with kind.
func assertMembership(t *testing.T, kind, kkUID string, list func() ([]string, error)) {
	t.Helper()
	uids, err := list()
	if err != nil {
		t.Fatalf("listing %s members: %v", kind, err)
	}
	if slices.Contains(uids, kkUID) {
		return
	}
	t.Errorf("%s does not contain %s (members %v)", kind, kkUID, uids)
}

// assertPhashEdit checks the perceptual hashes and non-destructive edits moved.
func assertPhashEdit(t *testing.T, env *psEnv, kkUID string) {
	t.Helper()
	store := photos.NewStore(env.pool)
	ph, err := store.GetPhash(t.Context(), kkUID)
	if err != nil || ph.Phash != 111 || ph.Dhash != 222 {
		t.Errorf("phash = %+v (err %v), want 111/222", ph, err)
	}
	edit, err := store.GetEdit(t.Context(), kkUID)
	if err != nil || edit.Rotation != 90 {
		t.Errorf("edit rotation = %d (err %v), want 90", edit.Rotation, err)
	}
}

// TestIntegration_idempotentRerun verifies a re-run (after a metadata change
// bumps a photo past the watermark) matches by photosorter_uid and creates no
// duplicate photos, embeddings, faces, subjects, markers, albums or labels.
func TestIntegration_idempotentRerun(t *testing.T) {
	env := newPSEnv(t)
	ctx := t.Context()
	seedFullScenario(t, env)

	if _, err := env.svc.Migrate(ctx); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	before := snapshotCounts(t, env)

	// Bump psp1 past the recorded watermark so the re-run re-lists and re-matches it.
	exec(t, env.pool, `UPDATE `+psSchema+`.photos SET updated_at = now() WHERE uid = 'psp1'`)

	result, err := env.svc.Migrate(ctx)
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if result.Counts.Skipped < 1 {
		t.Errorf("re-run skipped = %d, want >= 1 (matched by photosorter_uid)", result.Counts.Skipped)
	}
	after := snapshotCounts(t, env)
	for table, want := range before {
		if after[table] != want {
			t.Errorf("%s rows = %d, want %d (idempotent re-run)", table, after[table], want)
		}
	}
}

// snapshotCounts records row counts of every table the migration writes, for
// idempotency comparison.
func snapshotCounts(t *testing.T, env *psEnv) map[string]int {
	t.Helper()
	tables := []string{"photos", "embeddings", "faces", "subjects", "markers", "albums", "labels"}
	counts := make(map[string]int, len(tables))
	for _, table := range tables {
		counts[table] = env.countRows(t, table)
	}
	return counts
}

// TestIntegration_perPhotoFailure verifies a photo whose original is missing is
// tallied as failed without aborting the run, the healthy photo still migrates,
// and the run is recorded as done.
func TestIntegration_perPhotoFailure(t *testing.T) {
	env := newPSEnv(t)
	ctx := t.Context()
	t0 := time.Date(2023, 6, 1, 10, 0, 0, 0, time.UTC)
	goodPath, goodHash := env.writeOriginal(t, "good.jpg", 50)
	seedPSPhoto(t, env.pool, psPhoto{
		uid: "bad", fileHash: "deadbeef", filePath: "/missing/nope.jpg",
		fileMime: "image/jpeg", title: "Bad", takenAt: t0, updatedAt: t0,
	})
	seedPSPhoto(t, env.pool, psPhoto{
		uid: "good", fileHash: goodHash, filePath: goodPath, fileName: "good.jpg",
		fileMime: "image/jpeg", title: "Good", takenAt: t0, updatedAt: t0.Add(time.Hour),
	})

	result, err := env.svc.Migrate(ctx)
	if err != nil {
		t.Fatalf("Migrate returned error, want nil: %v", err)
	}
	if result.Counts.Failed != 1 || result.Counts.Imported != 1 {
		t.Errorf("counts = %+v, want failed 1 imported 1", result.Counts)
	}
	if _, err := photos.NewStore(env.pool).GetByPhotosorterUID(ctx, "good"); err != nil {
		t.Errorf("good photo not migrated: %v", err)
	}
	assertRunDone(t, env, result.RunID)
}

// assertRunDone checks the import_runs row for id was closed as done.
func assertRunDone(t *testing.T, env *psEnv, id int64) {
	t.Helper()
	var status string
	if err := env.pool.QueryRow(t.Context(),
		"SELECT status FROM import_runs WHERE id = $1", id).Scan(&status); err != nil {
		t.Fatalf("reading run: %v", err)
	}
	if status != string(importer.StatusDone) {
		t.Errorf("run status = %q, want done", status)
	}
}
