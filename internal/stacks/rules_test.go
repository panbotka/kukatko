package stacks

import (
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/photos"
)

// named builds a candidate with just a uid and file name, the input the name
// rules key on.
func named(uid, name string) photos.StackCandidate {
	return photos.StackCandidate{UID: uid, FileName: name}
}

// stackedUIDs returns, for the single stack the input is expected to form, its
// member uids as a set; it fails the test when the number of stacks is not want.
func stackedUIDs(t *testing.T, cands []photos.StackCandidate, rules RuleSet, wantStacks int) map[string]bool {
	t.Helper()
	groups := Group(cands, rules)
	if len(groups) != wantStacks {
		t.Fatalf("Group formed %d stacks, want %d: %v", len(groups), wantStacks, groups)
	}
	if wantStacks != 1 {
		return nil
	}
	got := make(map[string]bool)
	for _, idx := range groups[0] {
		got[cands[idx].UID] = true
	}
	return got
}

func TestGroup_baseName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cands       []photos.StackCandidate
		wantStacks  int
		wantMembers []string
	}{
		{
			name:        "raw plus jpeg of the same base stack",
			cands:       []photos.StackCandidate{named("a", "IMG_1234.CR2"), named("b", "IMG_1234.jpg")},
			wantStacks:  1,
			wantMembers: []string{"a", "b"},
		},
		{
			name:       "different numeric stems do not stack",
			cands:      []photos.StackCandidate{named("a", "IMG_1234.jpg"), named("b", "IMG_12345.jpg")},
			wantStacks: 0,
		},
		{
			name:       "a copy name is not a bare base-name match",
			cands:      []photos.StackCandidate{named("a", "IMG_1234.jpg"), named("b", "IMG_1234 (2).jpg")},
			wantStacks: 0,
		},
		{
			name: "three extensions of one base stack together",
			cands: []photos.StackCandidate{
				named("a", "DSC01.ARW"), named("b", "DSC01.JPG"), named("c", "DSC01.xmp"),
			},
			wantStacks:  1,
			wantMembers: []string{"a", "b", "c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stackedUIDs(t, tt.cands, RuleSet{BaseName: true}, tt.wantStacks)
			assertMembers(t, got, tt.wantMembers)
		})
	}
}

func TestGroup_sequentialCopy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cands       []photos.StackCandidate
		wantStacks  int
		wantMembers []string
	}{
		{
			name: "copy, sequence and edit derivatives collapse onto the original",
			cands: []photos.StackCandidate{
				named("a", "IMG_1234.jpg"),
				named("b", "IMG_1234 (2).jpg"),
				named("c", "IMG_1234 copy.jpg"),
				named("d", "IMG_1234-edited.jpg"),
			},
			wantStacks:  1,
			wantMembers: []string{"a", "b", "c", "d"},
		},
		{
			name:       "a burst sequence is not a copy set",
			cands:      []photos.StackCandidate{named("a", "IMG_1234.jpg"), named("b", "IMG_1235.jpg")},
			wantStacks: 0,
		},
		{
			name:       "a lone copy.jpg does not anchor a stack",
			cands:      []photos.StackCandidate{named("a", "copy.jpg"), named("b", "copy.png")},
			wantStacks: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stackedUIDs(t, tt.cands, RuleSet{SequentialCopy: true}, tt.wantStacks)
			assertMembers(t, got, tt.wantMembers)
		})
	}
}

func TestGroup_uniqueID(t *testing.T) {
	t.Parallel()
	withID := func(uid, id string) photos.StackCandidate {
		return photos.StackCandidate{UID: uid, UniqueID: id}
	}
	cands := []photos.StackCandidate{
		withID("a", "XYZ-1"), withID("b", "XYZ-1"), withID("c", "XYZ-2"), withID("d", ""),
	}
	got := stackedUIDs(t, cands, RuleSet{UniqueID: true}, 1)
	assertMembers(t, got, []string{"a", "b"})
}

func TestGroup_timeGPS(t *testing.T) {
	t.Parallel()
	when := time.Date(2020, 6, 1, 12, 0, 0, 0, time.UTC)
	other := when.Add(time.Second)
	at := func(uid string, ts time.Time, lat, lng float64) photos.StackCandidate {
		return photos.StackCandidate{UID: uid, TakenAt: &ts, Lat: &lat, Lng: &lng}
	}
	tests := []struct {
		name       string
		cands      []photos.StackCandidate
		rules      RuleSet
		wantStacks int
	}{
		{
			name:       "same second and GPS stack when the rule is on",
			cands:      []photos.StackCandidate{at("a", when, 50.0, 14.0), at("b", when, 50.0, 14.0)},
			rules:      RuleSet{TimeGPS: true},
			wantStacks: 1,
		},
		{
			name:       "same second and GPS do NOT stack when the rule is off",
			cands:      []photos.StackCandidate{at("a", when, 50.0, 14.0), at("b", when, 50.0, 14.0)},
			rules:      RuleSet{BaseName: true},
			wantStacks: 0,
		},
		{
			name:       "same second but different GPS does not stack",
			cands:      []photos.StackCandidate{at("a", when, 50.0, 14.0), at("b", when, 51.0, 14.0)},
			rules:      RuleSet{TimeGPS: true},
			wantStacks: 0,
		},
		{
			name:       "different second does not stack",
			cands:      []photos.StackCandidate{at("a", when, 50.0, 14.0), at("b", other, 50.0, 14.0)},
			rules:      RuleSet{TimeGPS: true},
			wantStacks: 0,
		},
		{
			name: "missing GPS is never grouped by the loose rule",
			cands: []photos.StackCandidate{
				{UID: "a", TakenAt: &when}, {UID: "b", TakenAt: &when},
			},
			rules:      RuleSet{TimeGPS: true},
			wantStacks: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stackedUIDs(t, tt.cands, tt.rules, tt.wantStacks)
		})
	}
}

func TestGroup_rulesUnionTransitively(t *testing.T) {
	t.Parallel()
	// Rule 1 links a–b by base name; rule 3 links b–c by unique id; the union of
	// the two rules must merge all three into one stack.
	cands := []photos.StackCandidate{
		{UID: "a", FileName: "IMG_1.CR2"},
		{UID: "b", FileName: "IMG_1.jpg", UniqueID: "shared"},
		{UID: "c", FileName: "EXPORT.jpg", UniqueID: "shared"},
	}
	got := stackedUIDs(t, cands, RuleSet{BaseName: true, UniqueID: true}, 1)
	assertMembers(t, got, []string{"a", "b", "c"})
}

func TestGroup_noRulesFormsNothing(t *testing.T) {
	t.Parallel()
	cands := []photos.StackCandidate{named("a", "IMG_1.CR2"), named("b", "IMG_1.jpg")}
	if groups := Group(cands, RuleSet{}); len(groups) != 0 {
		t.Fatalf("Group with no rules formed %d stacks, want 0", len(groups))
	}
}

func TestRuleSet_Any(t *testing.T) {
	t.Parallel()
	if (RuleSet{}).Any() {
		t.Error("empty RuleSet.Any() = true, want false")
	}
	if !(RuleSet{TimeGPS: true}).Any() {
		t.Error("RuleSet{TimeGPS}.Any() = false, want true")
	}
}

// assertMembers checks that got holds exactly the wanted uids; a nil want skips.
func assertMembers(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	if want == nil {
		return
	}
	if len(got) != len(want) {
		t.Fatalf("stack members = %v, want %v", got, want)
	}
	for _, uid := range want {
		if !got[uid] {
			t.Errorf("stack missing member %q (got %v)", uid, got)
		}
	}
}
