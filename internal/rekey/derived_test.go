package rekey

import (
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/storekeys"
)

// storeOnly returns a Service over mem alone: enough for moveTracked, which
// never touches the database.
func storeOnly(mem *memStore) *Service {
	return &Service{store: mem, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// tileMoves puts n root tiles into mem and returns their moves under prefix.
func tileMoves(mem *memStore, prefix string, n int) []Move {
	moves := make([]Move, 0, n)
	for i := range n {
		key := "thumb/01/23/45/" + testHash + "_size" + string(rune('a'+i)) + ".jpg"
		mem.put(key, []byte("tile "+key))
		moves = append(moves, Move{From: key, To: prefix + key, Kind: storekeys.KindThumbnail})
	}
	return moves
}

func TestMoveTracked_copyFailureKeepsEveryRootObject(t *testing.T) {
	t.Parallel()
	mem := newMemStore()
	moves := tileMoves(mem, testPrefix, 3)
	errThrottled := errors.New("429 slow down")
	mem.failCopy[moves[1].To] = errThrottled
	svc := storeOnly(mem)

	outcome, err := svc.moveTracked(t.Context(), moves, false)
	if err != nil {
		t.Fatalf("moveTracked = %v, want the failure in the outcome", err)
	}
	if !slices.Equal(outcome.failed, []string{"thumbnail"}) || !errors.Is(outcome.problem, errThrottled) {
		t.Errorf("outcome = %+v, want the thumbnail kind failed by the throttling", outcome)
	}
	for _, move := range moves {
		if _, ok := mem.get(move.From); !ok {
			t.Errorf("root %s deleted although a copy of the photo failed", move.From)
		}
	}

	// A later pass, with the store recovered, moves all of them.
	delete(mem.failCopy, moves[1].To)
	outcome, err = svc.moveTracked(t.Context(), moves, true)
	if err != nil || len(outcome.failed) != 0 || outcome.problem != nil {
		t.Fatalf("retry = %+v, %v; want everything moved", outcome, err)
	}
	for _, move := range moves {
		if _, ok := mem.get(move.From); ok {
			t.Errorf("root %s survived the retry", move.From)
		}
		if _, ok := mem.get(move.To); !ok {
			t.Errorf("prefixed %s missing after the retry", move.To)
		}
	}
}

func TestMoveTracked_retryKeepsANewerPrefixedObject(t *testing.T) {
	t.Parallel()
	mem := newMemStore()
	moves := tileMoves(mem, testPrefix, 1)
	mem.put(moves[0].To, []byte("rendered after the move"))

	outcome, err := storeOnly(mem).moveTracked(t.Context(), moves, true)
	if err != nil || outcome.problem != nil {
		t.Fatalf("moveTracked = %+v, %v", outcome, err)
	}
	if data, _ := mem.get(moves[0].To); string(data) != "rendered after the move" {
		t.Errorf("prefixed tile = %q, want the newer one kept", data)
	}
	if _, ok := mem.get(moves[0].From); ok {
		t.Error("the root tile was kept although its prefixed copy exists")
	}
}

func TestMoveTracked_missingSourceIsNoWork(t *testing.T) {
	t.Parallel()
	mem := newMemStore()
	moves := []Move{{From: "thumb/aa/" + testHash + "_x.jpg", To: testPrefix + "thumb/aa/" + testHash + "_x.jpg",
		Kind: storekeys.KindThumbnail}}
	for _, keep := range []bool{false, true} {
		outcome, err := storeOnly(mem).moveTracked(t.Context(), moves, keep)
		if err != nil || outcome.problem != nil || len(outcome.failed) != 0 {
			t.Errorf("keepExisting %v: outcome %+v, %v; want nothing to do", keep, outcome, err)
		}
	}
}

func TestMoveTracked_deleteFailureIsRecorded(t *testing.T) {
	t.Parallel()
	mem := newMemStore()
	moves := tileMoves(mem, testPrefix, 2)
	mem.failDelete[moves[0].From] = errors.New("503 unavailable")

	outcome, err := storeOnly(mem).moveTracked(t.Context(), moves, false)
	if err != nil || !slices.Equal(outcome.failed, []string{"thumbnail"}) || outcome.problem == nil {
		t.Errorf("outcome = %+v, %v; want the undeleted tile recorded", outcome, err)
	}
}

func TestMoveTracked_systemicFailureIsFatal(t *testing.T) {
	t.Parallel()
	mem := newMemStore()
	moves := tileMoves(mem, testPrefix, 1)
	mem.failCopy[moves[0].To] = storage.ErrBucketNotFound

	if _, err := storeOnly(mem).moveTracked(t.Context(), moves, false); !errors.Is(err, storage.ErrBucketNotFound) {
		t.Errorf("moveTracked = %v, want the systemic failure returned", err)
	}
}

func TestKeyHashes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key, thumb, hls string
	}{
		{"thumb/01/23/45/" + testHash + "_tile_224.jpg", testHash, ""},
		{"hls/" + testHash + "/1080p/00001.m4s", "", testHash},
		{"hls/" + testHash, "", ""},
		{"thumb/odd.jpg", "", ""},
	}
	for _, tt := range tests {
		if got := thumbKeyHash(tt.key); tt.thumb != "" && got != tt.thumb {
			t.Errorf("thumbKeyHash(%q) = %q, want %q", tt.key, got, tt.thumb)
		}
		if got := hlsKeyHash(tt.key); got != tt.hls {
			t.Errorf("hlsKeyHash(%q) = %q, want %q", tt.key, got, tt.hls)
		}
	}
	if got := thumbKeyHash("thumb/odd.jpg"); got != "" {
		t.Errorf("thumbKeyHash of a key without a size = %q, want empty", got)
	}
}

func TestRootDerivedMoves(t *testing.T) {
	t.Parallel()
	if moves := rootDerivedMoves(PhotoFiles{FilePath: "2023/01/a.jpg", FileHash: testHash}); moves != nil {
		t.Errorf("a photo still in the root layout planned %v, want nil", moves)
	}
	photo := PhotoFiles{
		FilePath: testPrefix + "2023/01/clip.mp4", FileHash: testHash,
		Renditions: []Rendition{{Name: "720p", SegmentCount: 1}},
	}
	moves := rootDerivedMoves(photo)
	if len(moves) == 0 {
		t.Fatal("a moved photo planned no derived moves")
	}
	for _, move := range moves {
		if !trackedKind(move.Kind) {
			t.Errorf("planned %s, want thumbnails and segments only", move.Kind)
		}
		if prefix, _ := storekeys.Strip(move.From); prefix != "" {
			t.Errorf("from %s is not a root key", move.From)
		}
		if move.To != testPrefix+move.From {
			t.Errorf("move %s → %s, want the same key under the prefix", move.From, move.To)
		}
	}
	if !slices.ContainsFunc(moves, func(m Move) bool { return m.From == "hls/"+testHash+"/720p/init.mp4" }) {
		t.Errorf("moves %v lack the rendition's init segment", moves)
	}
}
