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

	// A positive limit caps the page, so a caller can schedule in batches.
	capped, err := store.ListVideosMissingHLS(ctx, 1)
	if err != nil {
		t.Fatalf("ListVideosMissingHLS(limit=1): %v", err)
	}
	if len(capped) != 1 {
		t.Errorf("limited listing = %v, want one uid", capped)
	}
}
