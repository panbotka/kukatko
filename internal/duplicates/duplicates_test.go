package duplicates

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// fakePhashes serves a fixed set of perceptual hashes (or an error).
type fakePhashes struct {
	hashes []photos.Phash
	err    error
}

// ListActivePhashes returns the canned hashes or error.
func (f fakePhashes) ListActivePhashes(_ context.Context) ([]photos.Phash, error) {
	return f.hashes, f.err
}

// fakePhotos serves a fixed catalogue keyed by uid (or an error), recording the
// uids requested.
type fakePhotos struct {
	byUID map[string]photos.Photo
	err   error
	asked []string
}

// ListByUIDs returns the known photos for uids, ignoring unknown ones.
func (f *fakePhotos) ListByUIDs(_ context.Context, uids []string) ([]photos.Photo, error) {
	f.asked = append(f.asked, uids...)
	if f.err != nil {
		return nil, f.err
	}
	out := make([]photos.Photo, 0, len(uids))
	for _, u := range uids {
		if p, ok := f.byUID[u]; ok {
			out = append(out, p)
		}
	}
	return out, nil
}

// fakeEmbeddings serves a fixed set of near-duplicate pairs (or an error).
type fakeEmbeddings struct {
	pairs []vectors.DuplicatePair
	err   error
}

// FindDuplicatePairs returns the canned pairs or error.
func (f fakeEmbeddings) FindDuplicatePairs(_ context.Context, _ int, _ float64) ([]vectors.DuplicatePair, error) {
	return f.pairs, f.err
}

// makePhoto builds a catalogue photo with the comparison-relevant fields set.
func makePhoto(uid string, w, h int, size int64, taken time.Time) photos.Photo {
	return photos.Photo{
		UID:        uid,
		FileName:   uid + ".jpg",
		FileWidth:  w,
		FileHeight: h,
		FileSize:   size,
		MediaType:  photos.MediaImage,
		TakenAt:    &taken,
		CreatedAt:  taken,
	}
}

// catalogue builds a fakePhotos from a list of photos.
func catalogue(list ...photos.Photo) *fakePhotos {
	byUID := make(map[string]photos.Photo, len(list))
	for _, p := range list {
		byUID[p.UID] = p
	}
	return &fakePhotos{byUID: byUID}
}

var baseTime = time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

// TestFindGroups_phashGrouping groups near-pHash photos, leaves a distinct photo
// out, and suggests the highest-resolution keeper.
func TestFindGroups_phashGrouping(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: 0b11}, // 2 bits from a -> duplicate
		{PhotoUID: "ph_c", Phash: -1},   // far -> distinct
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime), // bigger -> keeper
		makePhoto("ph_c", 100, 100, 10, baseTime),
	)
	svc := New(Config{Photos: photoCat, Phashes: fakePhashes{hashes: hashes}, PhashMaxDiff: 8})

	res, err := svc.FindGroups(context.Background(), 0, 0)
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
	if g.Reason != ReasonPhash {
		t.Errorf("reason = %q, want %q", g.Reason, ReasonPhash)
	}
	if g.KeeperUID != "ph_b" {
		t.Errorf("keeper = %q, want ph_b", g.KeeperUID)
	}
	if g.ID != "ph_a" {
		t.Errorf("group id = %q, want ph_a (smallest uid)", g.ID)
	}
	assertKeeperFlag(t, g)
	assertPhashDistance(t, g)
}

// assertKeeperFlag checks exactly one member is flagged as the keeper and it is
// the named keeper.
func assertKeeperFlag(t *testing.T, g Group) {
	t.Helper()
	keepers := 0
	for _, m := range g.Members {
		if m.IsKeeper {
			keepers++
			if m.UID != g.KeeperUID {
				t.Errorf("is_keeper set on %q, but keeper is %q", m.UID, g.KeeperUID)
			}
		}
	}
	if keepers != 1 {
		t.Errorf("found %d keepers, want exactly 1", keepers)
	}
}

// assertPhashDistance checks the keeper has no distance and the non-keeper has a
// computed pHash distance to it.
func assertPhashDistance(t *testing.T, g Group) {
	t.Helper()
	for _, m := range g.Members {
		switch {
		case m.IsKeeper && m.PhashDistance != nil:
			t.Errorf("keeper %q has a pHash distance %d", m.UID, *m.PhashDistance)
		case !m.IsKeeper && m.PhashDistance == nil:
			t.Errorf("non-keeper %q has no pHash distance", m.UID)
		}
	}
}

// TestFindGroups_embeddingGrouping groups by embedding pairs and reports the
// embedding distance to the keeper.
func TestFindGroups_embeddingGrouping(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: -1}, // far in pHash, but linked by embedding
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 100, 100, 10, baseTime),
	)
	embed := fakeEmbeddings{pairs: []vectors.DuplicatePair{{A: "ph_a", B: "ph_b", Distance: 0.02}}}
	svc := New(Config{
		Photos: photoCat, Phashes: fakePhashes{hashes: hashes}, Embeddings: embed,
		PhashMaxDiff: 8, EmbeddingMaxDist: 0.05,
	})

	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if len(res.Groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(res.Groups))
	}
	g := res.Groups[0]
	if g.Reason != ReasonEmbedding {
		t.Errorf("reason = %q, want %q", g.Reason, ReasonEmbedding)
	}
	var sawDist bool
	for _, m := range g.Members {
		if !m.IsKeeper && m.EmbeddingDistance != nil {
			sawDist = true
			if *m.EmbeddingDistance != 0.02 {
				t.Errorf("embedding distance = %v, want 0.02", *m.EmbeddingDistance)
			}
		}
	}
	if !sawDist {
		t.Errorf("no embedding distance reported on the non-keeper member")
	}
}

// TestFindGroups_bothReason marks a group linked by both signals.
func TestFindGroups_bothReason(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{{PhotoUID: "ph_a", Phash: 0}, {PhotoUID: "ph_b", Phash: 0b1}}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 100, 100, 10, baseTime),
	)
	embed := fakeEmbeddings{pairs: []vectors.DuplicatePair{{A: "ph_a", B: "ph_b", Distance: 0.01}}}
	svc := New(Config{
		Photos: photoCat, Phashes: fakePhashes{hashes: hashes}, Embeddings: embed,
		PhashMaxDiff: 8, EmbeddingMaxDist: 0.05,
	})
	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if len(res.Groups) != 1 || res.Groups[0].Reason != ReasonBoth {
		t.Fatalf("reason = %v, want both", res.Groups)
	}
}

// TestFindGroups_pagination checks slicing and the next-offset cursor.
func TestFindGroups_pagination(t *testing.T) {
	t.Parallel()
	// Three independent pairs -> three groups. Each pair's hashes are 1 bit apart
	// internally, but the group bases differ by 16+ bits, so no cross-group merge
	// happens at maxDiff=8.
	hashes := []photos.Phash{
		{PhotoUID: "ph_a1", Phash: 0x0}, {PhotoUID: "ph_a2", Phash: 0x1},
		{PhotoUID: "ph_b1", Phash: 0xFFFF}, {PhotoUID: "ph_b2", Phash: 0xFFFE},
		{PhotoUID: "ph_c1", Phash: 0xFFFF0000}, {PhotoUID: "ph_c2", Phash: 0xFFFF0001},
	}
	photoCat := catalogue(
		makePhoto("ph_a1", 100, 100, 10, baseTime), makePhoto("ph_a2", 100, 100, 10, baseTime),
		makePhoto("ph_b1", 100, 100, 10, baseTime), makePhoto("ph_b2", 100, 100, 10, baseTime),
		makePhoto("ph_c1", 100, 100, 10, baseTime), makePhoto("ph_c2", 100, 100, 10, baseTime),
	)
	svc := New(Config{Photos: photoCat, Phashes: fakePhashes{hashes: hashes}, PhashMaxDiff: 8})

	first, err := svc.FindGroups(context.Background(), 2, 0)
	if err != nil {
		t.Fatalf("FindGroups page 1: %v", err)
	}
	if first.Total != 3 || len(first.Groups) != 2 {
		t.Fatalf("page 1: total=%d len=%d, want 3/2", first.Total, len(first.Groups))
	}
	if first.NextOffset == nil || *first.NextOffset != 2 {
		t.Fatalf("page 1 next_offset = %v, want 2", first.NextOffset)
	}

	second, err := svc.FindGroups(context.Background(), 2, 2)
	if err != nil {
		t.Fatalf("FindGroups page 2: %v", err)
	}
	if len(second.Groups) != 1 || second.NextOffset != nil {
		t.Fatalf("page 2: len=%d next=%v, want 1/nil", len(second.Groups), second.NextOffset)
	}
}

// TestFindGroups_noDuplicates returns an empty page when nothing groups.
func TestFindGroups_noDuplicates(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: -1},
	}
	photoCat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 100, 100, 10, baseTime),
	)
	svc := New(Config{Photos: photoCat, Phashes: fakePhashes{hashes: hashes}, PhashMaxDiff: 8})
	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if res.Total != 0 || len(res.Groups) != 0 {
		t.Fatalf("got %d groups, want none", len(res.Groups))
	}
	if res.Groups == nil {
		t.Errorf("Groups should be an empty slice, not nil, for stable JSON")
	}
}

// TestFindGroups_missingMetadataDropsGroup drops a pair when one member's photo
// metadata is unavailable, so the surviving singleton is not a group.
func TestFindGroups_missingMetadataDropsGroup(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{{PhotoUID: "ph_a", Phash: 0}, {PhotoUID: "ph_b", Phash: 0b1}}
	// Only ph_a has catalogue metadata; ph_b is missing.
	photoCat := catalogue(makePhoto("ph_a", 100, 100, 10, baseTime))
	svc := New(Config{Photos: photoCat, Phashes: fakePhashes{hashes: hashes}, PhashMaxDiff: 8})
	res, err := svc.FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	if len(res.Groups) != 0 {
		t.Fatalf("got %d groups, want 0 (incomplete group dropped)", len(res.Groups))
	}
}

// TestFindGroups_propagatesErrors surfaces source failures.
func TestFindGroups_propagatesErrors(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	svc := New(Config{Photos: catalogue(), Phashes: fakePhashes{err: sentinel}, PhashMaxDiff: 8})
	if _, err := svc.FindGroups(context.Background(), 0, 0); !errors.Is(err, sentinel) {
		t.Fatalf("FindGroups error = %v, want wrap of sentinel", err)
	}
}

// TestNew_panicsWithoutSources checks the fail-fast wiring guard.
func TestNew_panicsWithoutSources(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Errorf("New did not panic with nil Photos")
		}
	}()
	New(Config{Phashes: fakePhashes{}})
}

// TestClampPaging checks limit/offset normalisation.
func TestClampPaging(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		limit, offset         int
		wantLimit, wantOffset int
	}{
		{name: "defaults", limit: 0, offset: 0, wantLimit: defaultLimit, wantOffset: 0},
		{name: "caps limit", limit: 5000, offset: 0, wantLimit: maxLimit, wantOffset: 0},
		{name: "negative offset", limit: 10, offset: -3, wantLimit: 10, wantOffset: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotLimit, gotOffset := clampPaging(tt.limit, tt.offset)
			if gotLimit != tt.wantLimit || gotOffset != tt.wantOffset {
				t.Errorf("clampPaging(%d,%d) = %d,%d want %d,%d",
					tt.limit, tt.offset, gotLimit, gotOffset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}
