//go:build integration

package importverify_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/importverify"
)

// hash64 pads name into a 64-char hex-looking file_hash so it satisfies the
// photos.file_hash NOT NULL UNIQUE column without collisions.
func hash64(name string) string {
	return name + strings.Repeat("0", 64-len(name))
}

// insertPhoto inserts a photos row with the given catalogue uid and optional
// PhotoPrism external ids (empty strings become SQL NULL).
func insertPhoto(t *testing.T, pool *pgxpool.Pool, uid, ppUID, ppHash string) {
	t.Helper()
	var ppUIDArg, ppHashArg *string
	if ppUID != "" {
		ppUIDArg = &ppUID
	}
	if ppHash != "" {
		ppHashArg = &ppHash
	}
	const q = `INSERT INTO photos (uid, file_hash, file_path, photoprism_uid, photoprism_file_hash)
		VALUES ($1, $2, $3, $4, $5)`
	if _, err := pool.Exec(context.Background(), q, uid, hash64(uid), uid+".jpg", ppUIDArg, ppHashArg); err != nil {
		t.Fatalf("insert photo %s: %v", uid, err)
	}
}

// insertOriginalFile inserts one role='original' photo_files row for a photo.
func insertOriginalFile(t *testing.T, pool *pgxpool.Pool, photoUID, path string) {
	t.Helper()
	const q = `INSERT INTO photo_files (photo_uid, file_path, role) VALUES ($1, $2, 'original')`
	if _, err := pool.Exec(context.Background(), q, photoUID, path); err != nil {
		t.Fatalf("insert photo_file %s/%s: %v", photoUID, path, err)
	}
}

// insertEmbedding inserts a zero-vector embeddings row for a photo. The vector is
// built server-side so the test does not depend on the pgvector param codec.
func insertEmbedding(t *testing.T, pool *pgxpool.Pool, photoUID string) {
	t.Helper()
	const q = `INSERT INTO embeddings (photo_uid, embedding)
		SELECT $1, array_fill(0::real, ARRAY[768])::halfvec`
	if _, err := pool.Exec(context.Background(), q, photoUID); err != nil {
		t.Fatalf("insert embedding %s: %v", photoUID, err)
	}
}

// insertFace inserts one zero-vector faces row for a photo.
func insertFace(t *testing.T, pool *pgxpool.Pool, photoUID string, faceIndex int) {
	t.Helper()
	const q = `INSERT INTO faces (photo_uid, face_index, embedding, bbox)
		SELECT $1, $2, array_fill(0::real, ARRAY[512])::halfvec, '{0,0,0,0}'::double precision[]`
	if _, err := pool.Exec(context.Background(), q, photoUID, faceIndex); err != nil {
		t.Fatalf("insert face %s/%d: %v", photoUID, faceIndex, err)
	}
}

// insertFaceDetection inserts a face-detection record for a photo.
func insertFaceDetection(t *testing.T, pool *pgxpool.Pool, photoUID string) {
	t.Helper()
	const q = `INSERT INTO face_detections (photo_uid) VALUES ($1)`
	if _, err := pool.Exec(context.Background(), q, photoUID); err != nil {
		t.Fatalf("insert face_detection %s: %v", photoUID, err)
	}
}

// insertNamed inserts a single row into a uid/slug/name-or-title table.
func insertNamed(t *testing.T, pool *pgxpool.Pool, query, uid, slug, value string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), query, uid, slug, value); err != nil {
		t.Fatalf("insert into structure table (%s): %v", uid, err)
	}
}

// TestStore_reconciliationReads seeds a small catalogue and asserts every Catalog
// method reads it back correctly.
func TestStore_reconciliationReads(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	ctx := context.Background()
	store := importverify.NewStore(pool)

	// photoA: PhotoPrism-imported, 2 originals, has embedding + face detection + face.
	insertPhoto(t, pool, "photoA", "ppA", "sha1a")
	insertOriginalFile(t, pool, "photoA", "a/1.jpg")
	insertOriginalFile(t, pool, "photoA", "a/2.cr2")
	insertEmbedding(t, pool, "photoA")
	insertFaceDetection(t, pool, "photoA")
	insertFace(t, pool, "photoA", 0)

	// photoB: PhotoPrism-imported, 1 original, NO embedding, NO face detection.
	insertPhoto(t, pool, "photoB", "ppB", "sha1b")
	insertOriginalFile(t, pool, "photoB", "b/1.jpg")

	// photoC: NOT PhotoPrism-imported, has an embedding (must not count towards the PP population).
	insertPhoto(t, pool, "photoC", "", "")
	insertOriginalFile(t, pool, "photoC", "c/1.jpg")
	insertEmbedding(t, pool, "photoC")

	// One structural row of each kind.
	insertNamed(t, pool, `INSERT INTO albums (uid, slug, title) VALUES ($1, $2, $3)`,
		"al1", "trip", "Trip")
	insertNamed(t, pool, `INSERT INTO labels (uid, slug, name) VALUES ($1, $2, $3)`,
		"lb1", "cat", "cat")
	insertNamed(t, pool, `INSERT INTO subjects (uid, slug, name) VALUES ($1, $2, $3)`,
		"su1", "alice", "Alice")

	t.Run("ImportedRefs", func(t *testing.T) {
		uids, hashes, err := store.ImportedRefs(ctx)
		if err != nil {
			t.Fatalf("ImportedRefs: %v", err)
		}
		if _, ok := uids["ppA"]; !ok {
			t.Errorf("uids missing ppA: %v", uids)
		}
		if _, ok := uids["ppB"]; !ok {
			t.Errorf("uids missing ppB: %v", uids)
		}
		if len(uids) != 2 {
			t.Errorf("len(uids) = %d, want 2 (%v)", len(uids), uids)
		}
		if _, ok := hashes["sha1a"]; !ok {
			t.Errorf("hashes missing sha1a: %v", hashes)
		}
		if len(hashes) != 2 {
			t.Errorf("len(hashes) = %d, want 2 (%v)", len(hashes), hashes)
		}
	})

	t.Run("OriginalFileCounts", func(t *testing.T) {
		counts, err := store.OriginalFileCounts(ctx)
		if err != nil {
			t.Fatalf("OriginalFileCounts: %v", err)
		}
		if counts["ppA"] != 2 {
			t.Errorf("counts[ppA] = %d, want 2", counts["ppA"])
		}
		if counts["ppB"] != 1 {
			t.Errorf("counts[ppB] = %d, want 1", counts["ppB"])
		}
		if len(counts) != 2 {
			t.Errorf("len(counts) = %d, want 2 (photoC has no photoprism_uid): %v", len(counts), counts)
		}
	})

	t.Run("Counts", func(t *testing.T) {
		got, err := store.Counts(ctx)
		if err != nil {
			t.Fatalf("Counts: %v", err)
		}
		want := importverify.CatalogCounts{
			Photos:             3,
			PhotoprismImported: 2,
			Embeddings:         1, // photoA only; photoC is not PP-imported
			FacePhotos:         1,
			Faces:              1,
			Albums:             1,
			Labels:             1,
			Subjects:           1,
		}
		if got != want {
			t.Errorf("Counts = %+v, want %+v", got, want)
		}
	})

	t.Run("PhotosMissingEmbeddings", func(t *testing.T) {
		sample, total, err := store.PhotosMissingEmbeddings(ctx, 10)
		if err != nil {
			t.Fatalf("PhotosMissingEmbeddings: %v", err)
		}
		if total != 1 || !slices.Equal(sample, []string{"ppB"}) {
			t.Errorf("missing embeddings = %d/%v, want 1/[ppB]", total, sample)
		}
	})

	t.Run("PhotosMissingEmbeddings zero limit", func(t *testing.T) {
		sample, total, err := store.PhotosMissingEmbeddings(ctx, 0)
		if err != nil {
			t.Fatalf("PhotosMissingEmbeddings(0): %v", err)
		}
		if total != 1 || len(sample) != 0 || sample == nil {
			t.Errorf("zero-limit = %d/%v, want total 1 and a non-nil empty sample", total, sample)
		}
	})

	t.Run("PhotosMissingFaces", func(t *testing.T) {
		sample, total, err := store.PhotosMissingFaces(ctx, 10)
		if err != nil {
			t.Fatalf("PhotosMissingFaces: %v", err)
		}
		if total != 1 || !slices.Equal(sample, []string{"ppB"}) {
			t.Errorf("missing faces = %d/%v, want 1/[ppB]", total, sample)
		}
	})

	t.Run("name sets", func(t *testing.T) {
		albums, err := store.AlbumTitles(ctx)
		if err != nil {
			t.Fatalf("AlbumTitles: %v", err)
		}
		if _, ok := albums["Trip"]; !ok || len(albums) != 1 {
			t.Errorf("AlbumTitles = %v, want {Trip}", albums)
		}
		labels, err := store.LabelNames(ctx)
		if err != nil {
			t.Fatalf("LabelNames: %v", err)
		}
		if _, ok := labels["cat"]; !ok || len(labels) != 1 {
			t.Errorf("LabelNames = %v, want {cat}", labels)
		}
		subjects, err := store.SubjectNames(ctx)
		if err != nil {
			t.Fatalf("SubjectNames: %v", err)
		}
		if _, ok := subjects["Alice"]; !ok || len(subjects) != 1 {
			t.Errorf("SubjectNames = %v, want {Alice}", subjects)
		}
	})
}
