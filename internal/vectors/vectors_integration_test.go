//go:build integration

package vectors_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// newStore returns a vectors.Store plus a photos.Store over a freshly truncated
// integration database. The photos.Store is used to create the parent rows the
// embeddings/faces foreign keys require.
func newStore(t *testing.T) (*vectors.Store, *photos.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return vectors.NewStore(db.Pool()), photos.NewStore(db.Pool()), db
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

// imageVec builds an ImageDim vector with the supplied index→value overrides and
// zeros elsewhere.
func imageVec(set map[int]float32) []float32 {
	return buildVec(vectors.ImageDim, set)
}

// faceVec builds a FaceDim vector with the supplied index→value overrides and
// zeros elsewhere.
func faceVec(set map[int]float32) []float32 {
	return buildVec(vectors.FaceDim, set)
}

// buildVec returns a zero vector of length dim with the given overrides applied.
func buildVec(dim int, set map[int]float32) []float32 {
	v := make([]float32, dim)
	for i, x := range set {
		v[i] = x
	}
	return v
}

// TestEmbeddingLifecycle exercises save, overwrite and read of image embeddings.
func TestEmbeddingLifecycle(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "emb1")

	saved, err := store.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID:   uid,
		Vector:     imageVec(map[int]float32{0: 1}),
		Model:      "ViT-B-32",
		Pretrained: "laion2b",
	})
	if err != nil {
		t.Fatalf("SaveEmbedding: %v", err)
	}
	if saved.Dim != vectors.ImageDim || saved.CreatedAt.IsZero() {
		t.Errorf("SaveEmbedding did not populate Dim/CreatedAt: %+v", saved)
	}

	got, err := store.GetEmbedding(ctx, uid)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if got.Model != "ViT-B-32" || got.Pretrained != "laion2b" || len(got.Vector) != vectors.ImageDim {
		t.Errorf("round-tripped embedding mismatch: %+v", got)
	}

	// Re-saving overwrites in place (no duplicate-key error).
	if _, err := store.SaveEmbedding(ctx, vectors.Embedding{
		PhotoUID: uid, Vector: imageVec(map[int]float32{1: 1}), Model: "ViT-L-14",
	}); err != nil {
		t.Fatalf("SaveEmbedding overwrite: %v", err)
	}
	got, err = store.GetEmbedding(ctx, uid)
	if err != nil || got.Model != "ViT-L-14" {
		t.Fatalf("overwrite not applied: %+v, %v", got, err)
	}
}

// TestGetEmbedding_notFound checks the sentinel for a missing embedding.
func TestGetEmbedding_notFound(t *testing.T) {
	store, _, _ := newStore(t)
	if _, err := store.GetEmbedding(t.Context(), "ph_missing"); !errors.Is(err, vectors.ErrEmbeddingNotFound) {
		t.Fatalf("GetEmbedding missing = %v, want ErrEmbeddingNotFound", err)
	}
}

// legacyImageDim is the image-embedding width Kukátko stored before migration
// 0057 swapped the sidecar's image tower from CLIP ViT-L-14 to SigLIP 2. It is
// written out rather than derived so the "wrong width" cases below stay wrong
// even if ImageDim moves again.
const legacyImageDim = 768

// TestSaveEmbedding_dimMismatch checks vector-length validation, including the
// case a model swap actually produces: a vector of the *previous* model's width.
// A stale sidecar (or a stale embedding.image_dim) must be refused outright — a
// 768-dim vector says nothing about where the photo sits in the 1152-dim space,
// so storing it would poison every later similarity search rather than fail loudly.
func TestSaveEmbedding_dimMismatch(t *testing.T) {
	store, photoStore, _ := newStore(t)
	tests := []struct {
		name string
		vec  []float32
	}{
		{"short vector", []float32{1, 2, 3}},
		{"previous model's width", make([]float32, legacyImageDim)},
		{"one element too many", make([]float32, vectors.ImageDim+1)},
		{"face width", make([]float32, vectors.FaceDim)},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid := makePhoto(t, photoStore, "embdim"+string(rune('a'+i)))
			_, err := store.SaveEmbedding(t.Context(), vectors.Embedding{PhotoUID: uid, Vector: tt.vec})
			if !errors.Is(err, vectors.ErrDimMismatch) {
				t.Fatalf("SaveEmbedding %d-element vector = %v, want ErrDimMismatch", len(tt.vec), err)
			}
		})
	}
}

// TestEmbeddingsColumnWidth pins what migration 0057 left behind: the stored
// column really is halfvec(1152) and the HNSW cosine index over it still exists.
// Without this the width lives only in Go constants, and a migration that half
// applied would show up as a runtime failure per photo instead of a failed test.
func TestEmbeddingsColumnWidth(t *testing.T) {
	_, _, db := newStore(t)
	ctx := t.Context()

	// pgvector encodes the declared dimension in atttypmod.
	var typmod int
	if err := db.Pool().QueryRow(ctx, `
		SELECT a.atttypmod FROM pg_attribute a
		WHERE a.attrelid = 'embeddings'::regclass AND a.attname = 'embedding'`).Scan(&typmod); err != nil {
		t.Fatalf("reading embeddings.embedding typmod: %v", err)
	}
	if typmod != vectors.ImageDim {
		t.Errorf("embeddings.embedding = halfvec(%d), want halfvec(%d)", typmod, vectors.ImageDim)
	}

	var indexDef string
	if err := db.Pool().QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE tablename = 'embeddings' AND indexname = 'idx_embeddings_hnsw'`).Scan(&indexDef); err != nil {
		t.Fatalf("reading idx_embeddings_hnsw: %v", err)
	}
	for _, want := range []string{"USING hnsw", "halfvec_cosine_ops", "m='16'", "ef_construction='200'"} {
		if !strings.Contains(indexDef, want) {
			t.Errorf("idx_embeddings_hnsw = %q, want it to contain %q", indexDef, want)
		}
	}
}

// TestFindSimilar checks cosine ordering (nearest first) and the maxDistance
// threshold filter for image embeddings.
func TestFindSimilar(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	near := makePhoto(t, photoStore, "near")
	mid := makePhoto(t, photoStore, "mid")
	far := makePhoto(t, photoStore, "far")

	saveEmbedding(t, store, near, imageVec(map[int]float32{0: 1}))
	saveEmbedding(t, store, mid, imageVec(map[int]float32{0: 1, 1: 1}))
	saveEmbedding(t, store, far, imageVec(map[int]float32{1: 1}))

	query := imageVec(map[int]float32{0: 1, 1: 0.1})

	matches, err := store.FindSimilar(ctx, query, 10, 0)
	if err != nil {
		t.Fatalf("FindSimilar: %v", err)
	}
	wantOrder := []string{near, mid, far}
	if got := uids(matches); !equalStrings(got, wantOrder) {
		t.Fatalf("FindSimilar order = %v, want %v", got, wantOrder)
	}
	if !ascendingDistances(matches) {
		t.Errorf("distances not ascending: %+v", matches)
	}

	// maxDistance excludes the far match (distance ~0.9) but keeps near and mid.
	filtered, err := store.FindSimilar(ctx, query, 10, 0.5)
	if err != nil {
		t.Fatalf("FindSimilar filtered: %v", err)
	}
	if got := uids(filtered); !equalStrings(got, []string{near, mid}) {
		t.Fatalf("FindSimilar filtered = %v, want [%s %s]", got, near, mid)
	}
}

// TestFindSimilar_dimMismatch checks query-vector validation on the search path,
// including a query at the previous model's width. That is the shape a stale
// sidecar hands semantic search, and it must be refused before it reaches SQL:
// pgvector would otherwise raise a raw type error, and a query embedded by a
// different model would rank the library by nothing at all.
func TestFindSimilar_dimMismatch(t *testing.T) {
	store, _, _ := newStore(t)
	tests := []struct {
		name string
		vec  []float32
	}{
		{"short query", []float32{1}},
		{"previous model's width", make([]float32, legacyImageDim)},
		{"one element too many", make([]float32, vectors.ImageDim+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.FindSimilar(t.Context(), tt.vec, 5, 0)
			if !errors.Is(err, vectors.ErrDimMismatch) {
				t.Fatalf("FindSimilar %d-element query = %v, want ErrDimMismatch", len(tt.vec), err)
			}
		})
	}
}

// TestEmbeddingCascadeDelete checks that deleting a photo removes its embedding.
func TestEmbeddingCascadeDelete(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()
	uid := makePhoto(t, photoStore, "cascade")
	saveEmbedding(t, store, uid, imageVec(map[int]float32{0: 1}))

	if err := photoStore.Delete(ctx, uid); err != nil {
		t.Fatalf("Delete photo: %v", err)
	}
	if _, err := store.GetEmbedding(ctx, uid); !errors.Is(err, vectors.ErrEmbeddingNotFound) {
		t.Fatalf("embedding survived photo delete: %v", err)
	}
}

// TestListPhotosMissingEmbedding verifies the backfill enumeration returns only
// photos without an embedding and honours the limit.
func TestListPhotosMissingEmbedding(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	withEmb := makePhoto(t, photoStore, "has-emb")
	missing1 := makePhoto(t, photoStore, "missing-1")
	missing2 := makePhoto(t, photoStore, "missing-2")
	saveEmbedding(t, store, withEmb, imageVec(map[int]float32{0: 1}))

	all, err := store.ListPhotosMissingEmbedding(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingEmbedding: %v", err)
	}
	if !containsAll(all, missing1, missing2) || contains(all, withEmb) {
		t.Errorf("missing = %v, want %s and %s but not %s", all, missing1, missing2, withEmb)
	}

	limited, err := store.ListPhotosMissingEmbedding(ctx, 1)
	if err != nil {
		t.Fatalf("ListPhotosMissingEmbedding limited: %v", err)
	}
	if len(limited) != 1 {
		t.Errorf("limited length = %d, want 1", len(limited))
	}
}

// TestListPhotosMissingEmbedding_excludesArchived verifies archived photos are
// never enqueued for backfill.
func TestListPhotosMissingEmbedding_excludesArchived(t *testing.T) {
	store, photoStore, _ := newStore(t)
	ctx := t.Context()

	live := makePhoto(t, photoStore, "live")
	archived := makePhoto(t, photoStore, "archived")
	if _, err := photoStore.Archive(ctx, archived); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	missing, err := store.ListPhotosMissingEmbedding(ctx, 0)
	if err != nil {
		t.Fatalf("ListPhotosMissingEmbedding: %v", err)
	}
	if !contains(missing, live) || contains(missing, archived) {
		t.Errorf("missing = %v, want %s but not archived %s", missing, live, archived)
	}
}

// contains reports whether uid is present in uids.
func contains(uids []string, uid string) bool {
	for _, u := range uids {
		if u == uid {
			return true
		}
	}
	return false
}

// containsAll reports whether every want uid is present in uids.
func containsAll(uids []string, want ...string) bool {
	for _, w := range want {
		if !contains(uids, w) {
			return false
		}
	}
	return true
}

// saveEmbedding is a brief helper for tests that only need an embedding stored.
func saveEmbedding(t *testing.T, store *vectors.Store, uid string, vec []float32) {
	t.Helper()
	if _, err := store.SaveEmbedding(t.Context(), vectors.Embedding{PhotoUID: uid, Vector: vec}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", uid, err)
	}
}

// uids extracts the photo uids from a slice of matches in order.
func uids(matches []vectors.Match) []string {
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.PhotoUID
	}
	return out
}

// ascendingDistances reports whether match distances are non-decreasing.
func ascendingDistances(matches []vectors.Match) bool {
	for i := 1; i < len(matches); i++ {
		if matches[i].Distance < matches[i-1].Distance {
			return false
		}
	}
	return true
}

// equalStrings reports whether two string slices are element-wise equal.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
