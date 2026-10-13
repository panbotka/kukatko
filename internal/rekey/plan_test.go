package rekey

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/storekeys"
	"github.com/panbotka/kukatko/internal/thumb"
)

// testPrefix is a well-formed project storage prefix.
const testPrefix = "projects/pjaaaaaaaaaaaaaaaaaaaaaaaa/"

// testHash is a well-formed file hash.
const testHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

// testPlaylist is a media playlist as ffmpeg writes it.
const testPlaylist = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-MAP:URI="init.mp4"
#EXTINF:6.0,
00000.m4s
#EXTINF:6.0,
00001.m4s
#EXTINF:2.5,
00002.m4s
#EXT-X-ENDLIST
`

// TestPlan_listsEveryObjectOfAPhoto verifies the plan holds the original first,
// then the sidecar, every thumbnail size and every streaming object, each moved
// under the prefix with the rest of its key unchanged.
func TestPlan_listsEveryObjectOfAPhoto(t *testing.T) {
	t.Parallel()
	photo := PhotoFiles{
		UID: "ph1", FilePath: "2024/05/clip.mp4", FileHash: testHash,
		Renditions: []Rendition{{Name: "1080p", Playlist: testPlaylist, SegmentCount: 3}},
	}

	moves := Plan(photo, testPrefix)

	if len(moves) == 0 || moves[0] != (Move{
		From: "2024/05/clip.mp4", To: testPrefix + "2024/05/clip.mp4", Kind: storekeys.KindOriginal,
	}) {
		t.Fatalf("first move = %+v, want the original", moves)
	}
	counts := map[storekeys.Kind]int{}
	for _, move := range moves {
		counts[move.Kind]++
		if move.To != testPrefix+move.From {
			t.Errorf("move %s → %s, want the same key under the prefix", move.From, move.To)
		}
		if storekeys.Classify(move.From) != move.Kind || storekeys.Classify(move.To) != move.Kind {
			t.Errorf("move %s → %s is classified otherwise than %s", move.From, move.To, move.Kind)
		}
	}
	want := map[storekeys.Kind]int{
		storekeys.KindOriginal: 1, storekeys.KindSidecar: 1,
		storekeys.KindThumbnail: len(thumb.SizeNames()), storekeys.KindHLS: 4,
	}
	for kind, n := range want {
		if counts[kind] != n {
			t.Errorf("%s moves = %d, want %d", kind, counts[kind], n)
		}
	}
	if !slices.ContainsFunc(moves, func(m Move) bool { return m.From == "sidecars/2024/05/clip.mp4.yml" }) {
		t.Errorf("plan lacks the sidecar: %+v", moves)
	}
}

// TestPlan_degenerateInput verifies an unusable prefix or key plans nothing and
// an unusable hash plans only the keys that do not depend on it.
func TestPlan_degenerateInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		photo  PhotoFiles
		prefix string
		want   int
	}{
		{name: "malformed prefix", photo: PhotoFiles{FilePath: "2024/05/a.jpg", FileHash: testHash},
			prefix: "elsewhere/", want: 0},
		{name: "empty file path", photo: PhotoFiles{FileHash: testHash}, prefix: testPrefix, want: 0},
		{name: "malformed hash", photo: PhotoFiles{FilePath: "2024/05/a.jpg", FileHash: "zz"},
			prefix: testPrefix, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Plan(tt.photo, tt.prefix); len(got) != tt.want {
				t.Errorf("Plan = %d moves %+v, want %d", len(got), got, tt.want)
			}
		})
	}
}

// TestRenditionNames_unionOfPlaylistAndCount verifies the names of a rendition
// are the union of what its playlist lists and what its count implies, with a
// name the layout cannot hold dropped.
func TestRenditionNames_unionOfPlaylistAndCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rendition Rendition
		want      []string
	}{
		{name: "playlist and count agree",
			rendition: Rendition{Playlist: testPlaylist, SegmentCount: 3},
			want:      []string{"00000.m4s", "00001.m4s", "00002.m4s", "init.mp4"}},
		{name: "count short of the playlist",
			rendition: Rendition{Playlist: testPlaylist, SegmentCount: 1},
			want:      []string{"00000.m4s", "00001.m4s", "00002.m4s", "init.mp4"}},
		{name: "no playlist", rendition: Rendition{SegmentCount: 2},
			want: []string{"00000.m4s", "00001.m4s", "init.mp4"}},
		{name: "foreign names dropped",
			rendition: Rendition{Playlist: "#EXT-X-MAP:URI=\"../x\"\nevil.ts\n", SegmentCount: 0},
			want:      []string{"init.mp4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := renditionNames(tt.rendition); !slices.Equal(got, tt.want) {
				t.Errorf("renditionNames = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMapURI reads the URI of an EXT-X-MAP tag.
func TestMapURI(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		`#EXT-X-MAP:URI="init.mp4"`:            "init.mp4",
		`#EXT-X-MAP:URI="sub/init.mp4",BYTE=1`: "sub/init.mp4",
		`#EXT-X-MAP:BYTERANGE=1`:               "",
		`#EXT-X-MAP:URI="unterminated`:         "",
		strings.Repeat("#", 3) + `URI="x.mp4"`: "x.mp4",
		`#EXT-X-MAP:URI=""`:                    "",
	}
	for line, want := range tests {
		if got := mapURI(line); got != want {
			t.Errorf("mapURI(%q) = %q, want %q", line, got, want)
		}
	}
}

// TestVerify covers the copy verification: an ETag match, a multipart source
// read back by SHA-256, a size mismatch, a digest mismatch, and a store without
// entity tags.
func TestVerify(t *testing.T) {
	t.Parallel()
	content := []byte("the original bytes")
	const from, to = "2024/05/a.jpg", testPrefix + "2024/05/a.jpg"
	tests := []struct {
		name    string
		arrange func(m *memStore)
		hash    string
		size    int64
		plain   bool
		wantErr error
	}{
		{name: "etag match", arrange: func(*memStore) {}, hash: "unused", size: int64(len(content))},
		{name: "multipart source reads back", arrange: func(m *memStore) { m.multipart[from] = true },
			hash: sha256Hex(content), size: int64(len(content))},
		{name: "multipart digest mismatch", arrange: func(m *memStore) { m.multipart[from] = true },
			hash: sha256Hex([]byte("other")), size: int64(len(content)), wantErr: ErrVerifyFailed},
		{name: "size mismatch", arrange: func(m *memStore) { m.put(to, []byte("short")) },
			hash: sha256Hex(content), size: int64(len(content)), wantErr: ErrVerifyFailed},
		{name: "corrupt copy of equal size", arrange: func(m *memStore) { m.put(to, []byte("THE ORIGINAL BYTES")) },
			hash: sha256Hex(content), size: int64(len(content)), wantErr: ErrVerifyFailed},
		{name: "no entity tags", arrange: func(*memStore) {}, plain: true,
			hash: sha256Hex(content), size: int64(len(content))},
		{name: "no entity tags, empty catalogue hash", arrange: func(*memStore) {}, plain: true,
			size: int64(len(content)), wantErr: ErrVerifyFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mem := newMemStore()
			mem.put(from, content)
			mem.put(to, content)
			tt.arrange(mem)
			svc := &Service{store: mem}
			if tt.plain {
				svc.store = plainStore{mem}
			}
			photo := PhotoFiles{FilePath: from, FileHash: tt.hash, FileSize: tt.size}
			err := svc.verify(t.Context(), photo, Move{From: from, To: to, Kind: storekeys.KindOriginal})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("verify = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// plainStore hides the fake's entity tags, as the filesystem backend has none.
type plainStore struct{ ObjectStore }
