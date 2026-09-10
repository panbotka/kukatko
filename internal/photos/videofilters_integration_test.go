//go:build integration

package photos_test

import (
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/hlsjob"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/query"
)

// videoLibrary is the fixture the video-filter assertions run against: the
// store, plus the name↔UID mapping that lets the table below speak in fixture
// names.
type videoLibrary struct {
	store *photos.Store
	names map[string]string
}

// seedVideoLibrary builds the smallest library that can tell the four video
// filters apart:
//
//	still     image   no length, no frame rate, no sound track, never streamed
//	live      live    a still with a motion clip: nothing about it was probed
//	silent    video   8 s, 30 fps, no audio, not encoded
//	talkie    video   90 s, 29.97 fps, audio, one recorded HLS rendition
//	longclip  video   5 min, 60 fps, audio, waiting to be encoded
//	unprobed  video   the container could not be read: no length, no frame rate
func seedVideoLibrary(t *testing.T) videoLibrary {
	t.Helper()
	store, db := newStore(t)
	ctx := t.Context()
	lib := videoLibrary{store: store, names: map[string]string{}}

	add := func(name string, p photos.Photo) photos.Photo {
		p.FileHash = "vf-" + name
		p.FilePath = "2026/03/" + name
		p.FileName = name
		created := mustCreate(t, store, p)
		lib.names[created.UID] = name
		return created
	}

	add("still", photos.Photo{
		FileMime: "image/jpeg", MediaType: photos.MediaImage,
		FileWidth: 4000, FileHeight: 3000, ImageCodec: "jpeg",
	})
	add("live", photos.Photo{
		FileMime: "image/heic", MediaType: photos.MediaLive,
		FileWidth: 4032, FileHeight: 3024,
	})
	add("silent", photos.Photo{
		FileMime: "video/mp4", MediaType: photos.MediaVideo, VideoCodec: "h264",
		DurationMs: ptrOf(8_000), FPS: ptrOf(30.0),
	})
	talkie := add("talkie", photos.Photo{
		FileMime: "video/mp4", MediaType: photos.MediaVideo, VideoCodec: "h264",
		DurationMs: ptrOf(90_000), FPS: ptrOf(29.97), AudioCodec: "aac", HasAudio: true,
	})
	add("longclip", photos.Photo{
		FileMime: "video/mp4", MediaType: photos.MediaVideo, VideoCodec: "hevc",
		DurationMs: ptrOf(300_000), FPS: ptrOf(60.0), AudioCodec: "aac", HasAudio: true,
	})
	add("unprobed", photos.Photo{
		FileMime: "video/mp4", MediaType: photos.MediaVideo,
	})

	if _, err := hlsjob.NewStore(db.Pool()).Save(ctx, hlsjob.Encoded{
		PhotoUID: talkie.UID, Rendition: "1080p", Playlist: "#EXTM3U\n",
		Width: 1920, Height: 1080, Bandwidth: 6_128_000,
		Codecs: "avc1.640028,mp4a.40.2", SegmentCount: 9, DurationMs: 90_000,
	}); err != nil {
		t.Fatalf("recording a rendition: %v", err)
	}
	return lib
}

// runVideoQuery parses input through the query language, maps it onto
// ListParams the way the API layer does and returns the sorted fixture names
// List yields — the whole parse→compile→SQL round trip in one call.
func runVideoQuery(t *testing.T, lib videoLibrary, input string) []string {
	t.Helper()
	parsed := query.Parse(input)
	if len(parsed.Unknown) != 0 {
		t.Fatalf("Parse(%q) did not understand %v", input, parsed.Unknown)
	}
	list, err := lib.store.List(t.Context(), photos.ListParams{
		Search:       parsed.PlainText(),
		SearchNot:    parsed.NotTerms(),
		QueryFilters: parsed.Filters,
	})
	if err != nil {
		t.Fatalf("List(%q): %v", input, err)
	}
	names := make([]string, 0, len(list))
	for _, p := range list {
		name, ok := lib.names[p.UID]
		if !ok {
			name = p.UID
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TestVideoFilters drives duration:, sound:, fps: and streaming: against the
// real database: the units and ranges resolve to the milliseconds the column
// stores, the NTSC frame rates answer to the whole numbers a camera prints,
// the streaming filter reads the recorded renditions — and nothing that is not
// a clip is ever part of an answer, in either direction.
func TestVideoFilters(t *testing.T) {
	lib := seedVideoLibrary(t)

	tests := []struct {
		query string
		want  []string
	}{
		// Duration: a single value covers the unit it names, a range runs to the
		// end of its upper bound, and an open end bounds one side only.
		{"duration:8s", []string{"silent"}},
		{"duration:8", []string{"silent"}},
		{"duration:9s", nil},
		{"duration:1m-", []string{"longclip", "talkie"}},
		{"duration:-10s", []string{"silent"}},
		{"duration:1m-2m", []string{"talkie"}},
		{"duration:5m", []string{"longclip"}},
		{"duration:1.5m", []string{"talkie"}},
		{"duration:0-1h", []string{"longclip", "silent", "talkie"}},
		{"duration:8s|5m", []string{"longclip", "silent"}},
		// A negated duration stays inside the clips whose length is known: the
		// guard is applied outside the negation, so a still is not "not 8 s".
		{"duration:!8s", []string{"longclip", "talkie"}},

		// Sound is a question only a standalone clip can answer.
		{"sound:yes", []string{"longclip", "talkie"}},
		{"sound:no", []string{"silent", "unprobed"}},
		{"sound:!yes", []string{"silent", "unprobed"}},

		// Frame rate, with the half-percent slack that makes 30 find 29.97.
		{"fps:30", []string{"silent", "talkie"}},
		{"fps:60", []string{"longclip"}},
		{"fps:50-", []string{"longclip"}},
		{"fps:24-30", []string{"silent", "talkie"}},
		{"fps:120", nil},

		// Streaming: what is ready to play, and the worklist of what is not.
		{"streaming:yes", []string{"talkie"}},
		{"streaming:no", []string{"longclip", "silent", "unprobed"}},
		{"streaming:!yes", []string{"longclip", "silent", "unprobed"}},

		// The filters compose with each other and with the existing ones, in one
		// query and one round trip.
		{"sound:yes streaming:no", []string{"longclip"}},
		{"duration:1m- fps:-30", []string{"talkie"}},
		{"type:video streaming:no duration:-10s", []string{"silent"}},
		{"codec:hevc duration:1m-", []string{"longclip"}},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := runVideoQuery(t, lib, tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("%q = %v, want %v", tt.query, got, tt.want)
			}
			for i, want := range tt.want {
				if got[i] != want {
					t.Fatalf("%q = %v, want %v", tt.query, got, tt.want)
				}
			}
		})
	}
}

// TestVideoFilters_neverAnswerForAStill is the property behind every guard: a
// still and a live photo have no length, no sound track and no rendition, so
// they must be absent from both directions of each filter rather than counted
// as a zero. It is asserted separately from the table above because it is one
// rule about all four filters, not an expectation about one of them.
func TestVideoFilters_neverAnswerForAStill(t *testing.T) {
	lib := seedVideoLibrary(t)

	queries := []string{
		"duration:0-1h", "duration:!8s", "duration:-1h",
		"sound:yes", "sound:no", "sound:!no",
		"fps:0-1000", "fps:!30",
		"streaming:yes", "streaming:no", "streaming:!no",
	}
	for _, q := range queries {
		for _, name := range runVideoQuery(t, lib, q) {
			if name == "still" || name == "live" {
				t.Errorf("%q returned %s, which has nothing to answer with", q, name)
			}
		}
	}
}
