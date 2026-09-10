package duplicates

import (
	"context"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// makeVideo builds a catalogue video clip of the given length, with the
// comparison-relevant fields set.
func makeVideo(uid string, w, h int, size int64, durationMs int) photos.Photo {
	p := makePhoto(uid, w, h, size, baseTime)
	p.FileName = uid + ".mp4"
	p.MediaType = photos.MediaVideo
	p.DurationMs = new(durationMs)
	return p
}

// findGroups runs a service over the given hashes/catalogue with pHash grouping
// on and returns the resulting groups.
func findGroups(t *testing.T, hashes []photos.Phash, cat *fakePhotos, embed EmbeddingSource) []Group {
	t.Helper()
	cfg := Config{Photos: cat, Phashes: fakePhashes{hashes: hashes}, PhashMaxDiff: 8}
	if embed != nil {
		cfg.Embeddings = embed
		cfg.EmbeddingMaxDist = 0.1
	}
	res, err := New(cfg).FindGroups(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindGroups: %v", err)
	}
	return res.Groups
}

// TestFindGroups_videoAndStillNeverPair is the regression case in the shape the
// real data takes: a clip's perceptual hash is its poster frame's, so a video
// filmed in the room a photograph was taken in carries *the same* hash. Nothing
// about the two pictures can tell them apart, which is exactly why the pair must
// not be offered.
func TestFindGroups_videoAndStillNeverPair(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_still", Phash: 0x0f0f0f0f0f0f0f0f, MediaType: photos.MediaImage},
		{PhotoUID: "ph_clip", Phash: 0x0f0f0f0f0f0f0f0f, MediaType: photos.MediaVideo},
	}
	cat := catalogue(
		makePhoto("ph_still", 4000, 3000, 5_000_000, baseTime),
		makeVideo("ph_clip", 1920, 1080, 40_000_000, 120_000),
	)

	if groups := findGroups(t, hashes, cat, nil); len(groups) != 0 {
		t.Fatalf("a video and a still with an identical hash produced %d group(s), want 0", len(groups))
	}
}

// TestFindGroups_videoAndStillNeverPairByEmbedding checks the same rule on the
// other linking signal: an embedding pair is computed from the poster frame too,
// so it can cross the boundary just as easily.
func TestFindGroups_videoAndStillNeverPairByEmbedding(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_still", Phash: 0, MediaType: photos.MediaImage},
		{PhotoUID: "ph_clip", Phash: -1, MediaType: photos.MediaVideo},
	}
	cat := catalogue(
		makePhoto("ph_still", 4000, 3000, 5_000_000, baseTime),
		makeVideo("ph_clip", 1920, 1080, 40_000_000, 30_000),
	)
	embed := fakeEmbeddings{pairs: []vectors.DuplicatePair{{A: "ph_still", B: "ph_clip", Distance: 0.01}}}

	if groups := findGroups(t, hashes, cat, embed); len(groups) != 0 {
		t.Fatalf("a cross-kind embedding pair produced %d group(s), want 0", len(groups))
	}
}

// TestFindGroups_twoVideosStillPair keeps the case the rule is not about: two
// clips may legitimately be duplicates of each other, and the payload says what
// they are so the compare screen can too.
func TestFindGroups_twoVideosStillPair(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_clip1", Phash: 0, MediaType: photos.MediaVideo},
		{PhotoUID: "ph_clip2", Phash: 0b11, MediaType: photos.MediaVideo},
	}
	cat := catalogue(
		makeVideo("ph_clip1", 1920, 1080, 40_000_000, 30_000),
		makeVideo("ph_clip2", 1280, 720, 20_000_000, 31_500),
	)

	groups := findGroups(t, hashes, cat, nil)
	if len(groups) != 1 || len(groups[0].Members) != 2 {
		t.Fatalf("two similar videos produced %d group(s), want 1 of 2 members", len(groups))
	}
	for _, m := range groups[0].Members {
		if m.MediaType != string(photos.MediaVideo) {
			t.Errorf("member %s media_type = %q, want %q", m.UID, m.MediaType, photos.MediaVideo)
		}
		if m.DurationMs == nil {
			t.Fatalf("member %s carries no duration_ms; the compare screen cannot say how long it is", m.UID)
		}
	}
	if got := *groups[0].Members[0].DurationMs; got != 30_000 && got != 31_500 {
		t.Errorf("duration_ms = %d, want one of the clips' lengths", got)
	}
}

// TestFindGroups_liveCountsAsStill checks the boundary is still/video and not
// image/everything-else: a live photo is a photograph carrying two seconds of
// motion, its hash is the still's, and it groups with a photograph as it always
// did.
func TestFindGroups_liveCountsAsStill(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_live", Phash: 0, MediaType: photos.MediaLive},
		{PhotoUID: "ph_still", Phash: 0b11, MediaType: photos.MediaImage},
	}
	live := makePhoto("ph_live", 4032, 3024, 3_000_000, baseTime)
	live.MediaType = photos.MediaLive
	cat := catalogue(live, makePhoto("ph_still", 4032, 3024, 2_900_000, baseTime))

	if groups := findGroups(t, hashes, cat, nil); len(groups) != 1 {
		t.Fatalf("a live photo and a still produced %d group(s), want 1", len(groups))
	}
}

// TestFindGroups_videoDoesNotJoinAStillGroup checks the rule is enforced per edge
// rather than over finished groups. Two photographs are duplicates of each other
// and a clip's poster frame matches both; the group must come back as the two
// photographs, with the clip left out entirely — a component cannot be taken
// apart after the union has drawn the edge.
func TestFindGroups_videoDoesNotJoinAStillGroup(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0, MediaType: photos.MediaImage},
		{PhotoUID: "ph_b", Phash: 0b1, MediaType: photos.MediaImage},
		{PhotoUID: "ph_v", Phash: 0b11, MediaType: photos.MediaVideo},
	}
	cat := catalogue(
		makePhoto("ph_a", 4000, 3000, 5_000_000, baseTime),
		makePhoto("ph_b", 4000, 3000, 4_000_000, baseTime),
		makeVideo("ph_v", 1920, 1080, 40_000_000, 12_000),
	)

	groups := findGroups(t, hashes, cat, nil)
	if len(groups) != 1 {
		t.Fatalf("got %d groups, want 1", len(groups))
	}
	for _, m := range groups[0].Members {
		if m.UID == "ph_v" {
			t.Fatalf("the clip joined the photographs' group: %+v", groups[0].Members)
		}
	}
	if len(groups[0].Members) != 2 {
		t.Errorf("group has %d members, want the two photographs", len(groups[0].Members))
	}
}

// TestFindGroups_unknownMediaTypeIsAStill checks a hash row whose media type is
// empty — a payload from before the column was read — is treated as a
// photograph, so nothing that used to group stops grouping.
func TestFindGroups_unknownMediaTypeIsAStill(t *testing.T) {
	t.Parallel()
	hashes := []photos.Phash{
		{PhotoUID: "ph_a", Phash: 0},
		{PhotoUID: "ph_b", Phash: 0b11, MediaType: photos.MediaImage},
	}
	cat := catalogue(
		makePhoto("ph_a", 100, 100, 10, baseTime),
		makePhoto("ph_b", 200, 200, 40, baseTime),
	)

	if groups := findGroups(t, hashes, cat, nil); len(groups) != 1 {
		t.Fatalf("an untyped hash row produced %d group(s), want 1", len(groups))
	}
}
