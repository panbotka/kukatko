package review

import (
	"fmt"
	"testing"

	"github.com/panbotka/kukatko/internal/duplicates"
	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// photoOf builds the minimum photo record the box projection reads.
func photoOf(width, height, orientation int) photos.Photo {
	return photos.Photo{FileWidth: width, FileHeight: height, FileOrientation: orientation}
}

// TestInterleaveKinds_noKindDominates is the property the three new question
// types had to be added under: a batch is a *prefix* of the built queue, so a
// merge that only balanced the kinds in aggregate could still open with twenty
// duplicate pairs in a row. With five equally stocked lists every prefix of five
// holds one of each.
func TestInterleaveKinds_noKindDominates(t *testing.T) {
	t.Parallel()
	lists := make([][]Question, 0, len(Kinds))
	for _, kind := range Kinds {
		list := make([]Question, 20)
		for i := range list {
			list[i] = Question{ID: fmt.Sprintf("%s-%d", kind, i), Kind: kind}
		}
		lists = append(lists, list)
	}
	merged := interleaveKinds(lists)
	if len(merged) != 100 {
		t.Fatalf("merged = %d questions, want all 100", len(merged))
	}
	for start := 0; start+len(Kinds) <= 20*len(Kinds); start += len(Kinds) {
		seen := map[Kind]int{}
		for _, q := range merged[start : start+len(Kinds)] {
			seen[q.Kind]++
		}
		if len(seen) != len(Kinds) {
			t.Fatalf("window at %d = %v, want one question of every kind", start, seen)
		}
	}
}

// TestInterleaveKinds_degradesToWhatIsLeft checks the other half of the rule: a
// kind that is the only one with material fills the batch alone. Withholding the
// work an exhausted library still has would be a worse failure than an unmixed
// batch.
func TestInterleaveKinds_degradesToWhatIsLeft(t *testing.T) {
	t.Parallel()
	only := []Question{{ID: "a", Kind: KindPlace}, {ID: "b", Kind: KindPlace}}
	merged := interleaveKinds([][]Question{nil, nil, only, nil, nil})
	if len(merged) != 2 || merged[0].ID != "a" || merged[1].ID != "b" {
		t.Fatalf("merged = %+v, want the two place questions in order", merged)
	}
}

// TestInterleaveKinds_isReproducible pins the determinism the whole queue rests
// on: no map iteration, no clock, no rand, so the same material merges the same
// way twice.
func TestInterleaveKinds_isReproducible(t *testing.T) {
	t.Parallel()
	build := func() [][]Question {
		return [][]Question{
			{{ID: "f1", Kind: KindFace}, {ID: "f2", Kind: KindFace}, {ID: "f3", Kind: KindFace}},
			{{ID: "l1", Kind: KindLabel}},
			{{ID: "p1", Kind: KindPlace}, {ID: "p2", Kind: KindPlace}},
			{{ID: "d1", Kind: KindDuplicate}},
			{{ID: "o1", Kind: KindOutlier}, {ID: "o2", Kind: KindOutlier}},
		}
	}
	first := interleaveKinds(build())
	second := interleaveKinds(build())
	if fmt.Sprint(first) != fmt.Sprint(second) {
		t.Fatalf("two merges of the same material differ:\n%v\n%v", first, second)
	}
}

func TestQuestionID_roundTripsEveryKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   string
		want questionRef
	}{
		{"place", placeQuestionID("ph1"), questionRef{Kind: KindPlace, PhotoUID: "ph1"}},
		{"duplicate", duplicateQuestionID("ph1", "ph2"),
			questionRef{Kind: KindDuplicate, PhotoUID: "ph1", OtherUID: "ph2"}},
		{"duplicate reversed", duplicateQuestionID("ph2", "ph1"),
			questionRef{Kind: KindDuplicate, PhotoUID: "ph1", OtherUID: "ph2"}},
		{"outlier", outlierQuestionID("ph1", 3, "su1"),
			questionRef{Kind: KindOutlier, PhotoUID: "ph1", FaceIndex: 3, SubjectUID: "su1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseQuestionID(tt.id)
			if err != nil {
				t.Fatalf("parse(%q): %v", tt.id, err)
			}
			if got != tt.want {
				t.Errorf("parse(%q) = %+v, want %+v", tt.id, got, tt.want)
			}
		})
	}
}

// TestDuplicateQuestionID_normalisesThePair pins why the id orders its two uids:
// a pair reported as (B,A) by one scan and (A,B) by the next must be one
// question, or a session asks it twice and the second answer is recorded against
// an id nobody has seen.
func TestDuplicateQuestionID_normalisesThePair(t *testing.T) {
	t.Parallel()
	if duplicateQuestionID("phb", "pha") != duplicateQuestionID("pha", "phb") {
		t.Errorf("the two argument orders mint different ids")
	}
}

func TestParseQuestionID_invalidNewKinds(t *testing.T) {
	t.Parallel()
	for _, id := range []string{
		"place", "place:", "duplicate:ph1", "duplicate:ph1:", "duplicate::ph2",
		// A photo is never a duplicate of itself; the store would reject the pair
		// anyway, so it is a malformed id rather than a failed write.
		"duplicate:ph1:ph1",
		"outlier:ph1", "outlier:ph1:x:su1", "outlier:ph1:0:", "outlier:ph1:-1:su1",
	} {
		if _, err := parseQuestionID(id); err == nil {
			t.Errorf("parse(%q) = nil error, want ErrInvalidQuestion", id)
		}
	}
}

func TestPairConfidence_takesTheStrongerSignal(t *testing.T) {
	t.Parallel()
	phash := 8
	embed := 0.02
	tests := []struct {
		name   string
		member duplicates.Member
		want   float64
	}{
		{"neither signal", duplicates.Member{}, 0},
		// 8 differing bits of 64 → 0.875.
		{"phash only", duplicates.Member{PhashDistance: &phash}, 0.875},
		{"embedding only", duplicates.Member{EmbeddingDistance: &embed}, 0.98},
		{"the stronger of the two wins",
			duplicates.Member{PhashDistance: &phash, EmbeddingDistance: &embed}, 0.98},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := pairConfidence(tt.member); got != tt.want {
				t.Errorf("pairConfidence = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPairGroups_onlyUnjudgedPairs pins both restrictions the duplicate check
// rests on: a larger component is not a yes/no question at all, and a group
// somebody has already confirmed has been answered.
func TestPairGroups_onlyUnjudgedPairs(t *testing.T) {
	t.Parallel()
	member := func(uid string) duplicates.Member { return duplicates.Member{UID: uid} }
	groups := []duplicates.Group{
		{ID: "pair", Members: []duplicates.Member{member("a"), member("b")}},
		{ID: "triple", Members: []duplicates.Member{member("c"), member("d"), member("e")}},
		{ID: "judged", Confirmed: true, Members: []duplicates.Member{member("f"), member("g")}},
		{ID: "single", Members: []duplicates.Member{member("h")}},
	}
	kept := pairGroups(groups)
	if len(kept) != 1 || kept[0].ID != "pair" {
		t.Fatalf("pairGroups = %+v, want only the unjudged two-member group", kept)
	}
}

func TestSortBySuspicion_mostDistantFirst(t *testing.T) {
	t.Parallel()
	questions := []Question{
		{ID: "near", Distance: 0.55},
		{ID: "far", Distance: 0.91},
		{ID: "mid", Distance: 0.70},
	}
	sortBySuspicion(questions)
	got := []string{questions[0].ID, questions[1].ID, questions[2].ID}
	want := []string{"far", "mid", "near"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v — the outlier check ranks the other way round "+
			"from the duplicate check, and the two must not be swapped", got, want)
	}
}

// TestQuestionEntity_facesAndOutliersShareAPerson checks the variety key: "is
// this Alice?" and "is this really Alice?" are both about Alice, so a batch
// cannot hold four of each.
func TestQuestionEntity_facesAndOutliersShareAPerson(t *testing.T) {
	t.Parallel()
	alice := people.Subject{UID: "su1", Name: "Alice"}
	face := Question{Kind: KindFace, Subject: &alice}
	outlier := Question{Kind: KindOutlier, Subject: &alice}
	if questionEntity(face) != questionEntity(outlier) {
		t.Errorf("face entity %q != outlier entity %q, want one person to be one entity",
			questionEntity(face), questionEntity(outlier))
	}
}

func TestQuestionEntity_newKinds(t *testing.T) {
	t.Parallel()
	label := organize.Label{UID: "lb1"}
	tests := []struct {
		name string
		q    Question
		want string
	}{
		{"label", Question{Kind: KindLabel, Label: &label}, "label:lb1"},
		// The place *name*, not the photo: ten photos guessed into the same
		// village are one repetition, not ten different ones.
		{"place", Question{Kind: KindPlace, Place: &PlaceGuess{Name: "Brno"}}, "place:Brno"},
		{"duplicate", Question{Kind: KindDuplicate, GroupID: "grp1"}, "duplicate:grp1"},
		{"nothing to key on", Question{Kind: KindPlace}, "place"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := questionEntity(tt.q); got != tt.want {
				t.Errorf("questionEntity = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSourceWantsChecks pins where the three new kinds live: with the default
// selection only. "People" and "labels" are promises about what the game will
// ask, and a place question would break either one.
func TestSourceWantsChecks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  Source
		want bool
	}{
		{SourceBoth, true},
		{SourcePeople, false},
		{SourceLabels, false},
		{Source("garbled"), true}, // folds to the default
		{Source(""), true},
	}
	for _, tt := range tests {
		if got := tt.src.wantsChecks(); got != tt.want {
			t.Errorf("Source(%q).wantsChecks() = %v, want %v", tt.src, got, tt.want)
		}
	}
}

// TestFaceBoxOf_honoursOrientation checks the pixel projection of an outlier's
// box against a rotated photo: the box is normalised to the *painted* frame, so
// a quarter-turn transposes the stored dimensions before scaling. Getting this
// backwards would draw the crop beside the face rather than on it.
func TestFaceBoxOf_honoursOrientation(t *testing.T) {
	t.Parallel()
	box := [4]float64{0.25, 0.5, 0.1, 0.2}
	upright := faceBoxOf(box, photoOf(1000, 800, 1))
	if upright.Pixel != [4]int{250, 400, 100, 160} {
		t.Errorf("upright pixel box = %v, want [250 400 100 160]", upright.Pixel)
	}
	rotated := faceBoxOf(box, photoOf(1000, 800, 6))
	if rotated.Pixel != [4]int{200, 500, 80, 200} {
		t.Errorf("rotated pixel box = %v, want the dimensions swapped first", rotated.Pixel)
	}
	if upright.Relative != box || rotated.Relative != box {
		t.Errorf("the relative box must be carried through untouched")
	}
}
