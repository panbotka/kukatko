package family

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
)

// graph is an in-memory genealogy the network walk can be driven over without a
// database: the familyFetcher it hands out answers exactly what
// networkFamiliesSQL does — every family one of the asked-for people is a
// partner or a child in, with all of its children.
type graph struct {
	families []TreeFamily
	calls    int
}

// fam builds a family box: partners a and b ("" for an unrecorded side, both
// empty for a sibling group) and its children.
func fam(uid, a, b string, children ...string) TreeFamily {
	first, second := normalisePair(a, b)
	return TreeFamily{Family: Family{UID: uid, PartnerA: first, PartnerB: second}, ChildUIDs: children}
}

// fetch is the graph's familyFetcher.
func (g *graph) fetch(_ context.Context, uids []string) ([]TreeFamily, error) {
	g.calls++
	var out []TreeFamily
	for _, f := range g.families {
		people := append(f.partnerUIDs(), f.ChildUIDs...)
		if slices.ContainsFunc(uids, func(uid string) bool { return slices.Contains(people, uid) }) {
			out = append(out, f)
		}
	}
	return out, nil
}

// walk runs walkNetwork over g and fails the test on an error.
func (g *graph) walk(t *testing.T, root string, limit int) network {
	t.Helper()
	got, err := walkNetwork(context.Background(), root, limit, g.fetch)
	if err != nil {
		t.Fatalf("walkNetwork: %v", err)
	}
	return got
}

// kozak is the production shape the network exists for: a couple with a child,
// the mother in a parentless sibling group with her sister, and the sister's own
// daughter. Neither directional walk from Tomáš reaches Dagmar or Petra.
//
//	[Ludmila, Dagmar]      (sibling group, no parents recorded)
//	Ludmila ⚭ Aleš    Dagmar
//	      |              |
//	    Tomáš          Petra
func kozak() *graph {
	return &graph{families: []TreeFamily{
		fam("fm_couple", "ludmila", "ales", "tomas"),
		fam("fm_sisters", "", "", "dagmar", "ludmila"),
		fam("fm_dagmar", "dagmar", "", "petra"),
	}}
}

func TestWalkNetwork_reachesSidewaysThroughASiblingGroup(t *testing.T) {
	t.Parallel()

	got := kozak().walk(t, "tomas", NetworkLimit)
	want := map[string]int{"tomas": 0, "ludmila": -1, "ales": -1, "dagmar": -1, "petra": 0}
	if !maps.Equal(got.generation, want) {
		t.Errorf("generations = %v, want %v", got.generation, want)
	}
	if len(got.order) != len(want) {
		t.Errorf("order = %v, want each of the %d people once", got.order, len(want))
	}
	if got.truncated {
		t.Error("a five-person component reported truncated")
	}
	if len(got.families) != 3 {
		t.Errorf("families = %d, want all three", len(got.families))
	}
}

// The generation is relative to the root, so walking from the cousin gives the
// same shape one step shifted: Tomáš is Petra's cousin, in her generation.
func TestWalkNetwork_generationsAreRelativeToTheRoot(t *testing.T) {
	t.Parallel()

	got := kozak().walk(t, "petra", NetworkLimit)
	want := map[string]int{"petra": 0, "dagmar": -1, "ludmila": -1, "ales": -1, "tomas": 0}
	if !maps.Equal(got.generation, want) {
		t.Errorf("generations = %v, want %v", got.generation, want)
	}
}

// cousinMarriage is a cycle whose two paths disagree about a generation: Cyril
// marries Klára, the daughter of his cousin Dana. Walking from Cyril, Dana is
// his mother-in-law two steps away (−1) and his cousin three steps away (0).
//
//	         Gustav ⚭ Gita
//	         /           \
//	Adam ⚭ Alena        Bára
//	    |                 |
//	  Cyril             Dana ⚭ David
//	    \                     |
//	     ⚭ ———————————————— Klára
//	             |
//	            Ema
func cousinMarriage() *graph {
	return &graph{families: []TreeFamily{
		fam("fm_grand", "gustav", "gita", "adam", "bara"),
		fam("fm_adam", "adam", "alena", "cyril"),
		fam("fm_bara", "bara", "", "dana"),
		fam("fm_dana", "dana", "david", "klara"),
		fam("fm_cyril", "cyril", "klara", "ema"),
	}}
}

func TestWalkNetwork_theNearestRelationshipWinsInACycle(t *testing.T) {
	t.Parallel()

	got := cousinMarriage().walk(t, "cyril", NetworkLimit)
	want := map[string]int{
		"cyril": 0, "klara": 0, "ema": 1,
		"adam": -1, "alena": -1, "dana": -1, "david": -1, "bara": -1,
		"gustav": -2, "gita": -2,
	}
	if !maps.Equal(got.generation, want) {
		t.Errorf("generations = %v, want %v", got.generation, want)
	}
	if len(got.order) != len(want) {
		t.Errorf("order = %v, want each of the %d people once", got.order, len(want))
	}
	if len(got.families) != 5 {
		t.Errorf("families = %d, want each of the five once", len(got.families))
	}
}

func TestWalkNetwork_theCapKeepsTheNearest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		limit     int
		want      []string
		truncated bool
	}{
		{name: "exactly the component", limit: 5, want: []string{"tomas", "ales", "ludmila", "dagmar", "petra"}},
		{name: "one short drops the cousin", limit: 4, want: []string{"tomas", "ales", "ludmila", "dagmar"}, truncated: true},
		{name: "parents only", limit: 3, want: []string{"tomas", "ales", "ludmila"}, truncated: true},
		// The parents' family does not fit whole, so it is not taken at all.
		{name: "a family is taken whole or not at all", limit: 2, want: []string{"tomas"}, truncated: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := kozak().walk(t, "tomas", tc.limit)
			if !slices.Equal(got.order, tc.want) {
				t.Errorf("order = %v, want %v", got.order, tc.want)
			}
			if got.truncated != tc.truncated {
				t.Errorf("truncated = %v, want %v", got.truncated, tc.truncated)
			}
			// The count goes on past the cap: the page says "the nearest N of 5".
			if got.total != 5 {
				t.Errorf("total = %d, want the whole component of 5", got.total)
			}
			for _, f := range got.families {
				for _, uid := range append(f.partnerUIDs(), f.ChildUIDs...) {
					if !slices.Contains(got.order, uid) {
						t.Errorf("family %s names %s, who is not a member", f.UID, uid)
					}
				}
			}
		})
	}
}

func TestWalkNetwork_aPersonInNoFamilyIsAloneAndDone(t *testing.T) {
	t.Parallel()

	g := kozak()
	got := g.walk(t, "stranger", NetworkLimit)
	if !slices.Equal(got.order, []string{"stranger"}) || got.generation["stranger"] != 0 {
		t.Errorf("walk = %v / %v, want the root alone at 0", got.order, got.generation)
	}
	if len(got.families) != 0 || got.truncated {
		t.Errorf("families = %v, truncated = %v, want none and false", got.families, got.truncated)
	}
	if g.calls != 1 {
		t.Errorf("fetches = %d, want the one that found nothing", g.calls)
	}
}

// The walk is deterministic: the same graph handed over in a different order
// resolves every generation the same way.
func TestWalkNetwork_isIndependentOfTheFetchOrder(t *testing.T) {
	t.Parallel()

	g := cousinMarriage()
	want := g.walk(t, "cyril", NetworkLimit)
	slices.Reverse(g.families)
	got := g.walk(t, "cyril", NetworkLimit)
	if !slices.Equal(got.order, want.order) || !maps.Equal(got.generation, want.generation) {
		t.Errorf("reversed fetch = %v %v, want %v %v", got.order, got.generation, want.order, want.generation)
	}
}

func TestWalkNetwork_passesAFetchErrorOn(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	_, err := walkNetwork(context.Background(), "tomas", NetworkLimit,
		func(context.Context, []string) ([]TreeFamily, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the fetch error", err)
	}
}

func TestCompareTreeFamilies_ordersByYearThenCreation(t *testing.T) {
	t.Parallel()

	early := TreeFamily{Family: Family{UID: "fm_b", FromYear: new(1950)}}
	late := TreeFamily{Family: Family{UID: "fm_a", FromYear: new(1970)}}
	unknownA := TreeFamily{Family: Family{UID: "fm_c"}}
	unknownB := TreeFamily{Family: Family{UID: "fm_d"}}
	got := []TreeFamily{unknownB, late, unknownA, early}
	slices.SortFunc(got, compareTreeFamilies)
	uids := make([]string, 0, len(got))
	for _, f := range got {
		uids = append(uids, f.UID)
	}
	if want := []string{"fm_b", "fm_a", "fm_c", "fm_d"}; !slices.Equal(uids, want) {
		t.Errorf("order = %v, want %v", uids, want)
	}
}
