package candidates

import (
	"testing"

	"github.com/panbotka/kukatko/internal/mediaurl"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/vectors"
)

// TestComputeMinMatchCount checks the vote rule scales with the exemplar count and
// the threshold and stays clamped to 1..exemplarCount (capped at 5).
func TestComputeMinMatchCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		exemplarCount int
		threshold     float64
		baseThreshold float64
		want          int
	}{
		{"zero exemplars is zero", 0, 0.5, 0.5, 0},
		{"single exemplar clamps to one", 1, 0.5, 0.5, 1},
		{"single exemplar never exceeds count", 1, 2.0, 0.5, 1},
		{"four exemplars at baseline is one", 4, 0.5, 0.5, 1},
		{"eight exemplars at baseline is one", 8, 0.5, 0.5, 1},
		{"nine exemplars at baseline is two", 9, 0.5, 0.5, 2},
		{"sixteen exemplars at baseline is two", 16, 0.5, 0.5, 2},
		{"thirty-six exemplars at baseline is three", 36, 0.5, 0.5, 3},
		{"large set clamps to five", 400, 0.5, 0.5, 5},
		{"looser threshold raises the count", 9, 1.0, 0.5, 3},
		{"tighter threshold lowers the count", 36, 0.25, 0.5, 2},
		{"three exemplars cannot demand more than three", 3, 2.0, 0.5, 3},
		{"zero base threshold falls back to ratio one", 9, 0.5, 0, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := computeMinMatchCount(tt.exemplarCount, tt.threshold, tt.baseThreshold); got != tt.want {
				t.Errorf("computeMinMatchCount(%d, %v, %v) = %d, want %d",
					tt.exemplarCount, tt.threshold, tt.baseThreshold, got, tt.want)
			}
		})
	}
}

// TestDedupExemplars checks each photo yields exactly one exemplar, the highest
// det_score wins, and the order is deterministic.
func TestDedupExemplars(t *testing.T) {
	t.Parallel()

	faces := []vectors.Face{
		{PhotoUID: "p2", FaceIndex: 0, DetScore: 0.4},
		{PhotoUID: "p1", FaceIndex: 1, DetScore: 0.9},
		{PhotoUID: "p1", FaceIndex: 0, DetScore: 0.5},
		{PhotoUID: "p2", FaceIndex: 1, DetScore: 0.8},
	}
	got := dedupExemplars(faces)
	if len(got) != 2 {
		t.Fatalf("dedupExemplars len = %d, want 2", len(got))
	}
	if got[0].PhotoUID != "p1" || got[0].FaceIndex != 1 {
		t.Errorf("first exemplar = %s#%d, want p1#1 (highest det_score)", got[0].PhotoUID, got[0].FaceIndex)
	}
	if got[1].PhotoUID != "p2" || got[1].FaceIndex != 1 {
		t.Errorf("second exemplar = %s#%d, want p2#1 (highest det_score)", got[1].PhotoUID, got[1].FaceIndex)
	}
}

// TestFilterVoted checks the vote rule and the relative size floor both drop
// candidates.
func TestFilterVoted(t *testing.T) {
	t.Parallel()

	cands := []votedCandidate{
		{key: vectors.FaceKey{PhotoUID: "a"}, matchCount: 1, bbox: [4]float64{0, 0, 0.3, 0.3}},
		{key: vectors.FaceKey{PhotoUID: "b"}, matchCount: 2, bbox: [4]float64{0, 0, 0.3, 0.3}},
		{key: vectors.FaceKey{PhotoUID: "c"}, matchCount: 3, bbox: [4]float64{0, 0, 0.01, 0.01}},
	}
	got := filterVoted(cands, 2, 0.02)
	if len(got) != 1 || got[0].key.PhotoUID != "b" {
		t.Fatalf("filterVoted = %+v, want only b (a fails votes, c fails size)", got)
	}
}

// TestFilterByPixel checks the absolute pixel floor, the missing-photo drop, and
// that a photo with unknown dimensions skips the pixel check instead of being
// dropped on a zero width.
func TestFilterByPixel(t *testing.T) {
	t.Parallel()

	cands := []votedCandidate{
		{key: vectors.FaceKey{PhotoUID: "big"}, bbox: [4]float64{0, 0, 0.3, 0.3}},      // 300px, kept
		{key: vectors.FaceKey{PhotoUID: "small"}, bbox: [4]float64{0, 0, 0.02, 0.02}},  // 20px, dropped
		{key: vectors.FaceKey{PhotoUID: "gone"}, bbox: [4]float64{0, 0, 0.3, 0.3}},     // no photo, dropped
		{key: vectors.FaceKey{PhotoUID: "nodims"}, bbox: [4]float64{0, 0, 0.02, 0.02}}, // 0 dims, kept
	}
	photoByUID := map[string]photos.Photo{
		"big":    {UID: "big", FileWidth: 1000, FileHeight: 800},
		"small":  {UID: "small", FileWidth: 1000, FileHeight: 800},
		"nodims": {UID: "nodims"},
	}
	got := filterByPixel(cands, photoByUID, 32)
	kept := map[string]bool{}
	for _, c := range got {
		kept[c.key.PhotoUID] = true
	}
	if !kept["big"] || !kept["nodims"] || kept["small"] || kept["gone"] || len(got) != 2 {
		t.Errorf("filterByPixel kept %v, want only big and nodims", kept)
	}
}

// TestMergeCandidate checks a repeat sighting bumps the vote and keeps the nearer
// distance.
func TestMergeCandidate(t *testing.T) {
	t.Parallel()

	merged := map[vectors.FaceKey]*votedCandidate{}
	key := vectors.FaceKey{PhotoUID: "p", FaceIndex: 1}
	mergeCandidate(merged, vectors.FaceCandidate{PhotoUID: "p", FaceIndex: 1, Distance: 0.4})
	mergeCandidate(merged, vectors.FaceCandidate{PhotoUID: "p", FaceIndex: 1, Distance: 0.2})
	got := merged[key]
	if got.matchCount != 2 {
		t.Errorf("matchCount = %d, want 2", got.matchCount)
	}
	if got.distance != 0.2 {
		t.Errorf("distance = %v, want 0.2 (the nearer)", got.distance)
	}
}

// TestFaceBox checks the pixel projection honours EXIF orientation by swapping the
// display dimensions for rotated photos.
func TestFaceBox(t *testing.T) {
	t.Parallel()

	bbox := [4]float64{0.1, 0.2, 0.5, 0.25}

	upright := faceBox(bbox, photos.Photo{FileWidth: 1000, FileHeight: 800, FileOrientation: 1})
	if upright.Pixel != [4]int{100, 160, 500, 200} {
		t.Errorf("upright pixel = %v, want [100 160 500 200]", upright.Pixel)
	}
	if upright.Relative != bbox {
		t.Errorf("relative = %v, want %v", upright.Relative, bbox)
	}

	// Orientation 6 (90°) swaps display width/height to 800x1000.
	rotated := faceBox(bbox, photos.Photo{FileWidth: 1000, FileHeight: 800, FileOrientation: 6})
	if rotated.Pixel != [4]int{80, 200, 400, 250} {
		t.Errorf("rotated pixel = %v, want [80 200 400 250]", rotated.Pixel)
	}
}

// TestBoundSurvivors checks the distance floor, the nearest-first order and the
// hydration ceiling, plus that the ceiling is reported rather than applied
// silently.
func TestBoundSurvivors(t *testing.T) {
	t.Parallel()

	voted := []votedCandidate{
		{key: vectors.FaceKey{PhotoUID: "far"}, distance: 0.5},
		{key: vectors.FaceKey{PhotoUID: "near"}, distance: 0.1},
		{key: vectors.FaceKey{PhotoUID: "mid"}, distance: 0.3},
	}
	kept, capped := boundSurvivors(append([]votedCandidate(nil), voted...), 0, 0)
	if capped || len(kept) != 3 || kept[0].key.PhotoUID != "near" || kept[2].key.PhotoUID != "far" {
		t.Errorf("boundSurvivors(_, 0, 0) = %+v, capped=%v; want all three nearest-first", kept, capped)
	}

	floored, _ := boundSurvivors(append([]votedCandidate(nil), voted...), 0.3, 0)
	if len(floored) != 2 || floored[0].key.PhotoUID != "mid" {
		t.Errorf("boundSurvivors(_, 0.3, 0) = %+v, want mid and far — the floor is inclusive", floored)
	}

	cut, capped := boundSurvivors(append([]votedCandidate(nil), voted...), 0, 2)
	if !capped || len(cut) != 2 || cut[1].key.PhotoUID != "mid" {
		t.Errorf("boundSurvivors(_, 0, 2) = %+v, capped=%v; want the two nearest and capped=true",
			cut, capped)
	}
}

// TestSortVoted checks the (photo, face) tie-break makes the cut to the hydration
// ceiling deterministic for candidates at the same distance.
func TestSortVoted(t *testing.T) {
	t.Parallel()

	cands := []votedCandidate{
		{key: vectors.FaceKey{PhotoUID: "b", FaceIndex: 0}, distance: 0.2},
		{key: vectors.FaceKey{PhotoUID: "a", FaceIndex: 1}, distance: 0.2},
		{key: vectors.FaceKey{PhotoUID: "a", FaceIndex: 0}, distance: 0.2},
	}
	sortVoted(cands)
	got := []string{
		cands[0].key.PhotoUID + string(rune('0'+cands[0].key.FaceIndex)),
		cands[1].key.PhotoUID + string(rune('0'+cands[1].key.FaceIndex)),
		cands[2].key.PhotoUID + string(rune('0'+cands[2].key.FaceIndex)),
	}
	if got[0] != "a0" || got[1] != "a1" || got[2] != "b0" {
		t.Errorf("sortVoted order = %v, want [a0 a1 b0]", got)
	}
}

// TestEmptyReason checks the two structural reasons are distinguished by whether the
// subject has any marked photos.
func TestEmptyReason(t *testing.T) {
	t.Parallel()

	if got := emptyReason(0); got != ReasonNoFaces {
		t.Errorf("emptyReason(0) = %q, want %q", got, ReasonNoFaces)
	}
	if got := emptyReason(3); got != ReasonNoEmbeddings {
		t.Errorf("emptyReason(3) = %q, want %q", got, ReasonNoEmbeddings)
	}
}

// TestCountActions checks the per-action tally.
func TestCountActions(t *testing.T) {
	t.Parallel()

	cands := []Candidate{
		{Action: ActionCreateMarker},
		{Action: ActionCreateMarker},
		{Action: ActionAssignPerson},
		{Action: ActionAlreadyDone},
	}
	got := countActions(cands)
	want := Counts{CreateMarker: 2, AssignPerson: 1, AlreadyDone: 1}
	if got != want {
		t.Errorf("countActions = %+v, want %+v", got, want)
	}
}

// TestTruncate checks the limit is applied only when positive.
func TestTruncate(t *testing.T) {
	t.Parallel()

	cands := []Candidate{{FaceIndex: 0}, {FaceIndex: 1}, {FaceIndex: 2}}
	if got := truncate(cands, 0); len(got) != 3 {
		t.Errorf("truncate(_, 0) len = %d, want 3 (all)", len(got))
	}
	if got := truncate(cands, 2); len(got) != 2 {
		t.Errorf("truncate(_, 2) len = %d, want 2", len(got))
	}
}

// TestPerExemplarLimit checks how the per-exemplar neighbour cap is sized: one
// exemplar carries the whole answer and keeps the configured limit, a crowd of them
// shares it, and the floor never lets any exemplar drop below a hundred neighbours.
// The cap is the difference between a search that costs 10 ms per exemplar and one
// that costs 41 ms, paid once per exemplar — see docs/PERF.md.
func TestPerExemplarLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		searchLimit   int
		maxCandidates int
		exemplarCount int
		want          int
	}{
		{"single exemplar keeps the configured limit", 1000, 500, 1, 1000},
		{"a handful still ask for the configured limit", 1000, 500, 2, 1000},
		{"the share falls with the exemplar count", 1000, 500, 8, 250},
		{"a heavily tagged subject lands on the floor", 1000, 500, 428, 100},
		{"the catch-all subject's cap lands there too", 1000, 500, 500, 100},
		{"a low configured limit still wins over the floor", 40, 500, 300, 40},
		{"the floor applies to a small hydration ceiling", 1000, 10, 50, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := New(Config{
				Faces: &fakeFaces{}, People: &fakePeople{}, Feedback: &fakeFeedback{}, Photos: &fakePhotos{},
				Media:       mediaurl.NewBuilder(nil),
				SearchLimit: tt.searchLimit, MaxCandidates: tt.maxCandidates,
			})
			if got := svc.perExemplarLimit(tt.exemplarCount); got != tt.want {
				t.Errorf("perExemplarLimit(%d) = %d, want %d", tt.exemplarCount, got, tt.want)
			}
		})
	}
}
