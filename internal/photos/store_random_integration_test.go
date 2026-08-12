//go:build integration

package photos_test

import (
	"strconv"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// randomPageSize is the page size the shuffle paging test walks the library
// with. Small enough that the fixture spans several pages — which is the whole
// point: a random order that is not stable across requests shows up as an
// overlap or a gap between them, not within one page.
const randomPageSize = 4

// seedShuffleSet creates n plain photos and returns their uids, so a test can
// assert that a shuffled walk reaches exactly this set.
func seedShuffleSet(t *testing.T, store *photos.Store, n int) []string {
	t.Helper()
	uids := make([]string, 0, n)
	for i := range n {
		hash := "shuffle" + strconv.Itoa(i)
		photo := mustCreate(t, store, photos.Photo{
			FileHash: hash, FilePath: "p/" + hash + ".jpg",
			FileName: hash + ".jpg", FileMime: "image/jpeg",
			TakenAtSource: "unknown",
		})
		uids = append(uids, photo.UID)
	}
	return uids
}

// walkShuffled pages the whole library in the random order for seed and returns
// the uids in the order they came back, exactly as the slideshow's shuffle reads
// them: one page at a time, each request repeating the seed.
func walkShuffled(t *testing.T, store *photos.Store, seed string, pages int) []string {
	t.Helper()
	var out []string
	for page := range pages {
		list, err := store.List(t.Context(), photos.ListParams{
			Sort: photos.SortByRandom, Seed: seed,
			Limit: randomPageSize, Offset: page * randomPageSize,
		})
		if err != nil {
			t.Fatalf("List(random, page %d): %v", page, err)
		}
		for _, p := range list {
			out = append(out, p.UID)
		}
	}
	return out
}

// TestListRandom_pagesCoverTheWholeSetOnce is the guarantee the slideshow's
// shuffle rests on: paging through the random order reaches every photo exactly
// once. A per-request random order (ORDER BY random()) would fail this — pages
// would overlap and photos would go missing between them — which is why the
// order is a function of the uid and a seed the show holds for its whole life.
func TestListRandom_pagesCoverTheWholeSetOnce(t *testing.T) {
	store, _ := newStore(t)

	want := seedShuffleSet(t, store, 3*randomPageSize)
	got := walkShuffled(t, store, "seed-one", 3)

	if len(got) != len(want) {
		t.Fatalf("shuffled walk returned %d photos, want %d", len(got), len(want))
	}
	seen := make(map[string]int, len(got))
	for _, uid := range got {
		seen[uid]++
	}
	for _, uid := range want {
		if seen[uid] != 1 {
			t.Errorf("photo %s appeared %d times in the shuffled walk, want exactly once", uid, seen[uid])
		}
	}
}

// TestListRandom_seedDecidesTheOrder verifies the two halves of the seed's
// contract: the same seed replays the same order (so a show's later pages line
// up with its earlier ones) and a different seed plays a different one (so the
// next show is not the same shuffle).
func TestListRandom_seedDecidesTheOrder(t *testing.T) {
	store, _ := newStore(t)

	seedShuffleSet(t, store, 3*randomPageSize)

	first := walkShuffled(t, store, "seed-one", 3)
	again := walkShuffled(t, store, "seed-one", 3)
	if !equalUIDs(first, again) {
		t.Errorf("the same seed gave two orders:\n%v\n%v", first, again)
	}

	other := walkShuffled(t, store, "seed-two", 3)
	if equalUIDs(first, other) {
		t.Errorf("two seeds gave the same order %v, want the show reshuffled", first)
	}
}

// equalUIDs reports whether two uid sequences are identical, order included.
func equalUIDs(a, b []string) bool {
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
