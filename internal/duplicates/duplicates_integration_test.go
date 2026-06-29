//go:build integration

package duplicates_test

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They share one database and truncate between
// cases, so they intentionally do not run in parallel.

// fixture bundles the real stores and a Service over a freshly truncated DB.
type fixture struct {
	svc     *duplicates.Service
	photos  *photos.Store
	vectors *vectors.Store
}

// newFixture wires real photos/vectors stores and a duplicates.Service with the
// default thresholds.
func newFixture(t *testing.T) fixture {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	ps := photos.NewStore(db.Pool())
	vs := vectors.NewStore(db.Pool())
	svc := duplicates.New(duplicates.Config{
		Photos:           ps,
		Phashes:          ps,
		Embeddings:       vs,
		PhashMaxDiff:     8,
		EmbeddingMaxDist: 0.05,
	})
	return fixture{svc: svc, photos: ps, vectors: vs}
}

// addPhoto creates a photo with the given dimensions and returns its uid.
func (f fixture) addPhoto(t *testing.T, hash string, w, h int, size int64) string {
	t.Helper()
	taken := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	created, err := f.photos.Create(t.Context(), photos.Photo{
		FileHash:   hash,
		FilePath:   "2024/01/" + hash + ".jpg",
		FileName:   hash + ".jpg",
		FileWidth:  w,
		FileHeight: h,
		FileSize:   size,
		TakenAt:    &taken,
	})
	if err != nil {
		t.Fatalf("creating photo %s: %v", hash, err)
	}
	return created.UID
}

// setPhash stores a perceptual hash for a photo.
func (f fixture) setPhash(t *testing.T, uid string, phash int64) {
	t.Helper()
	if err := f.photos.SetPhash(t.Context(), photos.Phash{PhotoUID: uid, Phash: phash, Dhash: phash}); err != nil {
		t.Fatalf("SetPhash(%s): %v", uid, err)
	}
}

// setEmbedding stores an image embedding from sparse index overrides.
func (f fixture) setEmbedding(t *testing.T, uid string, set map[int]float32) {
	t.Helper()
	vec := make([]float32, vectors.ImageDim)
	for i, v := range set {
		vec[i] = v
	}
	if _, err := f.vectors.SaveEmbedding(t.Context(), vectors.Embedding{PhotoUID: uid, Vector: vec}); err != nil {
		t.Fatalf("SaveEmbedding(%s): %v", uid, err)
	}
}

// TestService_phashGrouping plants two near-pHash photos (grouped) and one
// distant photo (excluded), and checks the keeper is the higher-resolution photo.
func TestService_phashGrouping(t *testing.T) {
	f := newFixture(t)
	small := f.addPhoto(t, "g-small", 100, 100, 10)
	big := f.addPhoto(t, "g-big", 400, 400, 80)
	distinct := f.addPhoto(t, "g-distinct", 100, 100, 10)

	f.setPhash(t, small, 0)
	f.setPhash(t, big, 0b111)                   // 3 bits from small -> grouped
	f.setPhash(t, distinct, 0x7FFFFFFFFFFFFFFF) // 63 bits -> distant

	res, err := f.svc.FindGroups(t.Context(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 1 || len(res.Groups) != 1 {
		t.Fatalf("got %d groups (total %d), want 1", len(res.Groups), res.Total)
	}
	g := res.Groups[0]
	if len(g.Members) != 2 {
		t.Fatalf("group has %d members, want 2", len(g.Members))
	}
	if g.KeeperUID != big {
		t.Errorf("keeper = %s, want %s (higher resolution)", g.KeeperUID, big)
	}
	if g.Reason != duplicates.ReasonPhash {
		t.Errorf("reason = %q, want phash", g.Reason)
	}
	for _, m := range g.Members {
		if m.UID == distinct {
			t.Errorf("distant photo %s was grouped", distinct)
		}
	}
}

// TestService_embeddingGrouping plants two near-embedding photos with distant
// pHashes; they group via the embedding signal.
func TestService_embeddingGrouping(t *testing.T) {
	f := newFixture(t)
	a := f.addPhoto(t, "e-a", 200, 200, 20)
	b := f.addPhoto(t, "e-b", 200, 200, 20)

	// Distant pHashes so only the embedding can link them.
	f.setPhash(t, a, 0)
	f.setPhash(t, b, 0x7FFFFFFFFFFFFFFF)
	f.setEmbedding(t, a, map[int]float32{0: 1, 1: 0.01})
	f.setEmbedding(t, b, map[int]float32{0: 1, 1: 0.02})

	res, err := f.svc.FindGroups(t.Context(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(res.Groups))
	}
	g := res.Groups[0]
	if g.Reason != duplicates.ReasonEmbedding {
		t.Errorf("reason = %q, want embedding", g.Reason)
	}
	var sawEmbedDistance bool
	for _, m := range g.Members {
		if !m.IsKeeper && m.EmbeddingDistance != nil {
			sawEmbedDistance = true
		}
	}
	if !sawEmbedDistance {
		t.Errorf("expected an embedding distance on the non-keeper member")
	}
}

// TestService_pagination plants three independent pHash pairs and checks paging.
func TestService_pagination(t *testing.T) {
	f := newFixture(t)
	// Three well-separated group bases (32+ bits apart), 1 bit internal spread.
	bases := []int64{0x0, 0xFFFF, 0xFFFF0000}
	for gi, base := range bases {
		x := f.addPhoto(t, pairHash(gi, "x"), 100, 100, 10)
		y := f.addPhoto(t, pairHash(gi, "y"), 100, 100, 10)
		f.setPhash(t, x, base)
		f.setPhash(t, y, base|0x1)
	}

	first, err := f.svc.FindGroups(t.Context(), 2, 0)
	if err != nil {
		t.Fatalf("FindGroups page 1: %v", err)
	}
	if first.Total != 3 || len(first.Groups) != 2 {
		t.Fatalf("page 1: total=%d len=%d, want 3/2", first.Total, len(first.Groups))
	}
	if first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("page 1 next_offset = %v, want 2", first.NextOffset)
	}

	second, err := f.svc.FindGroups(t.Context(), 2, 2)
	if err != nil {
		t.Fatalf("FindGroups page 2: %v", err)
	}
	if len(second.Groups) != 1 || second.NextOffset != nil {
		t.Fatalf("page 2: len=%d next=%v, want 1/nil", len(second.Groups), second.NextOffset)
	}
}

// pairHash builds a distinct file hash for member m of group gi.
func pairHash(gi int, m string) string {
	return "pg-" + string(rune('a'+gi)) + "-" + m
}
