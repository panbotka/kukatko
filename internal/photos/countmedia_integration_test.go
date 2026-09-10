//go:build integration

package photos_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestCountMedia_splitsVideosFromStills is the guarantee the library listing's
// count line rests on: how many rows match, and how many of those are clips,
// from one pass over the same filters. A live photo is a photograph that happens
// to carry motion, so it counts with the stills — the split exists to stop a
// video being announced as a photo, not to reclassify photographs.
func TestCountMedia_splitsVideosFromStills(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	mustCreate(t, store, photos.Photo{
		FileHash: "cm-still", FilePath: "2024/05/a.jpg", FileName: "a.jpg",
		FileMime: "image/jpeg", MediaType: photos.MediaImage, Title: "sunset garden",
	})
	mustCreate(t, store, photos.Photo{
		FileHash: "cm-live", FilePath: "2024/05/b.heic", FileName: "b.heic",
		FileMime: "image/heic", MediaType: photos.MediaLive, Title: "garden party",
	})
	mustCreate(t, store, photos.Photo{
		FileHash: "cm-clip-1", FilePath: "2024/05/c.mp4", FileName: "c.mp4",
		FileMime: "video/mp4", MediaType: photos.MediaVideo, Title: "sunset walk",
	})
	mustCreate(t, store, photos.Photo{
		FileHash: "cm-clip-2", FilePath: "2024/05/d.mp4", FileName: "d.mp4",
		FileMime: "video/mp4", MediaType: photos.MediaVideo, Title: "harvest",
	})

	tests := []struct {
		name       string
		params     photos.ListParams
		wantTotal  int
		wantVideos int
	}{
		{name: "the whole mixed library", wantTotal: 4, wantVideos: 2},
		{
			name:       "filters narrow both numbers",
			params:     photos.ListParams{Search: "sunset"},
			wantTotal:  2,
			wantVideos: 1,
		},
		{
			name:       "a stills-only result counts no videos, a live photo included",
			params:     photos.ListParams{Search: "garden"},
			wantTotal:  2,
			wantVideos: 0,
		},
		{
			name:       "a video-only result is all videos",
			params:     photos.ListParams{Search: "harvest"},
			wantTotal:  1,
			wantVideos: 1,
		},
		{
			name:       "an empty result counts nothing at all",
			params:     photos.ListParams{Search: "nothing matches this"},
			wantTotal:  0,
			wantVideos: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts, err := store.CountMedia(ctx, tt.params)
			if err != nil {
				t.Fatalf("CountMedia: %v", err)
			}
			if counts.Total != tt.wantTotal || counts.Videos != tt.wantVideos {
				t.Fatalf("CountMedia = {Total:%d Videos:%d}, want {Total:%d Videos:%d}",
					counts.Total, counts.Videos, tt.wantTotal, tt.wantVideos)
			}
			total, err := store.Count(ctx, tt.params)
			if err != nil {
				t.Fatalf("Count: %v", err)
			}
			if total != counts.Total {
				t.Fatalf("Count = %d but CountMedia total = %d — the two must never disagree", total, counts.Total)
			}
		})
	}
}
