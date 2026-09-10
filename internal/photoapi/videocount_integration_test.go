//go:build integration

package photoapi_test

import (
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// TestVideoTotal_reportedByEveryPagePath is what lets the client stop calling a
// page of clips "photos": the list endpoint, a pure filter query, a full-text
// search and a ranked one all report how much of their total is video. A live
// photo counts with the stills — it is a photograph.
func TestVideoTotal_reportedByEveryPagePath(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "video-total", auth.RoleEditor)
	base := env.server.URL

	env.seedPhoto(t, photos.Photo{
		Title: "sunset garden", MediaType: photos.MediaImage,
	}, "still.jpg", 200, 10, 10)
	env.seedPhoto(t, photos.Photo{
		Title: "garden party", MediaType: photos.MediaLive,
	}, "live.jpg", 10, 200, 10)
	clip := env.seedPhoto(t, photos.Photo{
		Title: "sunset walk", MediaType: photos.MediaVideo,
	}, "walk.jpg", 10, 10, 200)
	env.seedPhoto(t, photos.Photo{
		Title: "harvest", MediaType: photos.MediaVideo,
	}, "harvest.jpg", 40, 40, 40)

	t.Run("list reports the mixed library's split", func(t *testing.T) {
		got := getList(t, client, base, "")
		if got.Total != 4 || got.VideoTotal != 2 {
			t.Fatalf("list total=%d video_total=%d, want 4/2", got.Total, got.VideoTotal)
		}
	})
	t.Run("a stills-only page reports no videos", func(t *testing.T) {
		got := getList(t, client, base, "q=garden")
		if got.Total != 2 || got.VideoTotal != 0 {
			t.Fatalf("list total=%d video_total=%d, want 2/0", got.Total, got.VideoTotal)
		}
	})
	t.Run("a filter-only search reports the split", func(t *testing.T) {
		got := getSearch(t, client, base, "q="+"uid%3A"+clip.UID)
		if got.Mode != "filter" {
			t.Fatalf("mode = %q, want filter", got.Mode)
		}
		if got.Total != 1 || got.VideoTotal != 1 {
			t.Fatalf("filter total=%d video_total=%d, want 1/1", got.Total, got.VideoTotal)
		}
	})
	t.Run("full-text search reports the split", func(t *testing.T) {
		got := getSearch(t, client, base, "q=sunset&mode=fulltext")
		if got.Total != 2 || got.VideoTotal != 1 {
			t.Fatalf("fulltext total=%d video_total=%d, want 2/1", got.Total, got.VideoTotal)
		}
	})
	t.Run("a ranked search counts the videos in its ranked set", func(t *testing.T) {
		env.embedder.byQuery["sunset"] = imageVecAt(map[int]float32{0: 1})
		saveVec(t, env, clip.UID, imageVecAt(map[int]float32{0: 1}))

		got := getSearch(t, client, base, "q=sunset&mode=semantic")
		if !got.RankedTotal {
			t.Fatalf("ranked_total = false, want true for a semantic search")
		}
		if got.Total != 1 || got.VideoTotal != 1 {
			t.Fatalf("semantic total=%d video_total=%d, want 1/1", got.Total, got.VideoTotal)
		}
	})
}
