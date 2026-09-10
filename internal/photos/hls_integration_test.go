//go:build integration

package photos_test

import (
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
)

// hlsVideo builds a catalogued standalone video with a distinct file hash.
func hlsVideo(hash string) photos.Photo {
	return photos.Photo{
		FileHash:  hash,
		FilePath:  "2026/02/" + hash + ".mp4",
		FileName:  hash + ".mp4",
		FileMime:  "video/mp4",
		MediaType: photos.MediaVideo,
	}
}

// TestHLS_backfillCandidates verifies what the streaming backfill schedules: a
// video is a candidate until it has been encoded into at least one rendition,
// while stills, live photos and archived videos are never candidates at all —
// and the forced full run takes every live video whatever it already has.
func TestHLS_backfillCandidates(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	pending, err := store.Create(ctx, hlsVideo("hlsbf-pending"))
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	encoded, err := store.Create(ctx, hlsVideo("hlsbf-encoded"))
	if err != nil {
		t.Fatalf("create encoded: %v", err)
	}
	archived, err := store.Create(ctx, hlsVideo("hlsbf-archived"))
	if err != nil {
		t.Fatalf("create archived: %v", err)
	}
	if _, err := store.Archive(ctx, archived.UID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	still := hlsVideo("hlsbf-still")
	still.MediaType = photos.MediaImage
	still.FileMime = "image/jpeg"
	if _, err := store.Create(ctx, still); err != nil {
		t.Fatalf("create still: %v", err)
	}
	live := hlsVideo("hlsbf-live")
	live.MediaType = photos.MediaLive
	live.FileMime = "image/heic"
	if _, err := store.Create(ctx, live); err != nil {
		t.Fatalf("create live photo: %v", err)
	}

	uids, err := store.ListVideosMissingHLS(ctx, 0)
	if err != nil {
		t.Fatalf("ListVideosMissingHLS: %v", err)
	}
	if len(uids) != 2 ||
		!slices.Contains(uids, pending.UID) || !slices.Contains(uids, encoded.UID) {
		t.Fatalf("pending = %v, want exactly the two live videos", uids)
	}

	// One recorded rendition takes a video out of the queue: the master playlist
	// advertises whatever was produced, so the video is streamable.
	if _, err := hlsjob.NewStore(db.Pool()).Save(ctx, hlsjob.Encoded{
		PhotoUID: encoded.UID, Rendition: "1080p", Playlist: "#EXTM3U\n",
		Width: 1920, Height: 1080, Bandwidth: 6_128_000,
		Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 2, DurationMs: 10_000,
	}); err != nil {
		t.Fatalf("recording a rendition: %v", err)
	}
	uids, err = store.ListVideosMissingHLS(ctx, 0)
	if err != nil {
		t.Fatalf("ListVideosMissingHLS after the encode: %v", err)
	}
	if len(uids) != 1 || uids[0] != pending.UID {
		t.Fatalf("pending after one encode = %v, want only [%s]", uids, pending.UID)
	}

	// The forced full run re-encodes every live video, encoded or not — and still
	// never a still, a live photo or an archived video.
	all, err := store.ListActiveVideoUIDs(ctx)
	if err != nil {
		t.Fatalf("ListActiveVideoUIDs: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListActiveVideoUIDs = %v, want the two live videos", all)
	}

	// The count must answer for exactly that listing — it is what sizes the
	// video-scoped thumbnail backfill before it re-decodes every video original.
	videoCount, err := store.CountActiveVideos(ctx)
	if err != nil {
		t.Fatalf("CountActiveVideos: %v", err)
	}
	if videoCount != len(all) {
		t.Errorf("CountActiveVideos = %d, want %d (what ListActiveVideoUIDs returns)",
			videoCount, len(all))
	}

	// CountVideos answers the other question — "can this library stream anything
	// at all?" — and counts the archived video too, because archiving hides a
	// video without removing its renditions or its segments. It is the guard the
	// integrity check's streaming half asks before it touches the store.
	total, err := store.CountVideos(ctx)
	if err != nil {
		t.Fatalf("CountVideos: %v", err)
	}
	if total != 3 {
		t.Errorf("CountVideos = %d, want 3 (the two live videos and the archived one)", total)
	}

	// A positive limit caps the page, so a caller can schedule in batches.
	capped, err := store.ListVideosMissingHLS(ctx, 1)
	if err != nil {
		t.Fatalf("ListVideosMissingHLS(limit=1): %v", err)
	}
	if len(capped) != 1 {
		t.Errorf("limited listing = %v, want one uid", capped)
	}
}

// TestList_carriesStreamingFlag covers the flag a grid tile reads: every listing
// path must say, for a standalone video, whether the catalogue holds an encoded
// rendition for it — and must say nothing at all for a still, where the question
// does not apply. It is asserted across List, Search and FilterUIDs because all
// three answer a page of tiles and all three compute the flag in their own query.
func TestList_carriesStreamingFlag(t *testing.T) {
	store, db := newStore(t)
	ctx := t.Context()

	encoded := hlsVideo("hlsflag-encoded")
	encoded.Title = "Pelhrimov clip"
	created, err := store.Create(ctx, encoded)
	if err != nil {
		t.Fatalf("create encoded: %v", err)
	}
	pending := hlsVideo("hlsflag-pending")
	pending.Title = "Pelhrimov clip"
	waiting, err := store.Create(ctx, pending)
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	still := hlsVideo("hlsflag-still")
	still.MediaType = photos.MediaImage
	still.FileMime = "image/jpeg"
	still.Title = "Pelhrimov clip"
	photo, err := store.Create(ctx, still)
	if err != nil {
		t.Fatalf("create still: %v", err)
	}
	if _, err := hlsjob.NewStore(db.Pool()).Save(ctx, hlsjob.Encoded{
		PhotoUID: created.UID, Rendition: "1080p", Playlist: "#EXTM3U\n",
		Width: 1920, Height: 1080, Bandwidth: 6_128_000,
		Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 2, DurationMs: 10_000,
	}); err != nil {
		t.Fatalf("recording a rendition: %v", err)
	}

	want := map[string]*bool{
		created.UID: new(true),
		waiting.UID: new(false),
		photo.UID:   nil,
	}
	uids := []string{created.UID, waiting.UID, photo.UID}

	listed, err := store.List(ctx, photos.ListParams{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	assertStreamingFlags(t, "List", listed, want)

	found, err := store.Search(ctx, photos.ListParams{FullText: "Pelhrimov"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	assertStreamingFlags(t, "Search", found, want)

	filtered, err := store.FilterUIDs(ctx, uids, photos.ListParams{})
	if err != nil {
		t.Fatalf("FilterUIDs: %v", err)
	}
	assertStreamingFlags(t, "FilterUIDs", filtered, want)
}

// assertStreamingFlags checks that the listing returned exactly the expected
// photos and that each carries the expected streaming flag, where a nil want
// means the field must be absent altogether.
func assertStreamingFlags(t *testing.T, path string, got []photos.Photo, want map[string]*bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s returned %d photos, want %d", path, len(got), len(want))
	}
	for _, p := range got {
		expected, ok := want[p.UID]
		if !ok {
			t.Errorf("%s returned an unexpected photo %s", path, p.UID)
			continue
		}
		switch {
		case expected == nil && p.HLS != nil:
			t.Errorf("%s: %s carries hls = %v, want no flag at all", path, p.UID, *p.HLS)
		case expected != nil && p.HLS == nil:
			t.Errorf("%s: %s carries no hls flag, want %v", path, p.UID, *expected)
		case expected != nil && *p.HLS != *expected:
			t.Errorf("%s: %s hls = %v, want %v", path, p.UID, *p.HLS, *expected)
		}
	}
}
