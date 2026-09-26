//go:build integration

package family_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/people"
)

// kozaks is the production shape the network walk exists for, seeded through
// the same write paths the API uses:
//
//	[Ludmila, Dagmar]      (a sibling group: no partners, kind unknown)
//	Ludmila ⚭ Aleš    Dagmar
//	      |              |
//	    Tomáš          Petra
//
// From Tomáš the descendant walk finds only him and the pedigree only his
// parents; Dagmar (an aunt) and Petra (a cousin) sit sideways, through a family
// that names nobody as a partner.
type kozaks struct {
	tomas, ludmila, ales, dagmar, petra string
	sisters                             string
}

// seedKozaks builds the family above and returns its uids.
func seedKozaks(t *testing.T, fam *family.Store, ppl *people.Store, db *database.DB) kozaks {
	t.Helper()
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000020", "network-editor")
	k := kozaks{
		tomas:   makeSubject(t, ppl, "Tomáš Kozák"),
		ludmila: makeSubject(t, ppl, "Ludmila Kozáková"),
		ales:    makeSubject(t, ppl, "Aleš Kozák"),
		dagmar:  makeSubject(t, ppl, "Dagmar Andrlíková"),
		petra:   makeSubject(t, ppl, "Petra Houdková"),
	}
	for _, rel := range []struct{ child, parent string }{
		{child: k.tomas, parent: k.ludmila},
		{child: k.tomas, parent: k.ales},
		{child: k.petra, parent: k.dagmar},
	} {
		if _, err := fam.AddParentAudited(ctx, rel.child, rel.parent, "",
			entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
			t.Fatalf("adding parent %s of %s: %v", rel.parent, rel.child, err)
		}
	}
	group := addSibling(t, fam, actor, k.ludmila, k.dagmar)
	if group.Family.PartnerA != nil || group.Family.PartnerB != nil || group.Family.Kind != family.KindUnknown {
		t.Fatalf("sibling group = %+v, want no partners and kind unknown", group.Family)
	}
	k.sisters = group.Family.UID
	return k
}

// generations maps each member of a walk to its signed generation, failing on a
// person reported twice.
func generations(t *testing.T, members []family.Member) map[string]int {
	t.Helper()
	out := make(map[string]int, len(members))
	for _, m := range members {
		if _, dup := out[m.UID]; dup {
			t.Errorf("%s (%s) appears twice in the walk", m.Name, m.UID)
		}
		out[m.UID] = m.Generation
	}
	return out
}

// assertClosed fails when a family box names a person the walk did not return:
// the renderer would be handed an edge to a node it does not have.
func assertClosed(t *testing.T, tree family.Tree) {
	t.Helper()
	members := memberUIDs(tree.Members)
	for _, box := range tree.Families {
		named := slices.Clone(box.ChildUIDs)
		for _, partner := range []*string{box.PartnerA, box.PartnerB} {
			if partner != nil {
				named = append(named, *partner)
			}
		}
		for _, uid := range named {
			if !slices.Contains(members, uid) {
				t.Errorf("family %s names %s, who is not a member", box.UID, uid)
			}
		}
	}
}

func TestNetwork_reachesTheAuntAndTheCousin(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	k := seedKozaks(t, fam, ppl, db)

	tree, err := fam.Tree(ctx, k.tomas, family.DirectionNetwork, 0)
	if err != nil {
		t.Fatalf("Tree(network): %v", err)
	}
	want := map[string]int{k.tomas: 0, k.ludmila: -1, k.ales: -1, k.dagmar: -1, k.petra: 0}
	if got := generations(t, tree.Members); !maps.Equal(got, want) {
		t.Errorf("generations = %v, want %v", got, want)
	}
	for _, m := range tree.Members {
		if want := max(m.Generation, -m.Generation); m.Depth != want {
			t.Errorf("%s: depth = %d, want the unsigned generation %d", m.Name, m.Depth, want)
		}
		if m.Partner {
			t.Errorf("%s: partner flag set in a network, where it means nothing", m.Name)
		}
	}
	if tree.Direction != family.DirectionNetwork || tree.Truncated || tree.Root.UID != k.tomas {
		t.Errorf("tree = %q truncated=%v root=%s, want an untruncated network from Tomáš",
			tree.Direction, tree.Truncated, tree.Root.UID)
	}
	if len(tree.Families) != 3 {
		t.Fatalf("families = %d, want the couple, the sibling group and Dagmar's", len(tree.Families))
	}
	children := map[string][]string{}
	for _, box := range tree.Families {
		children[box.UID] = box.ChildUIDs
	}
	if got := children[k.sisters]; len(got) != 2 {
		t.Errorf("the sibling group lists %v, want both sisters", got)
	}
	assertClosed(t, tree)

	// The directional walks are untouched and still stop short of the aunt.
	down, err := fam.Tree(ctx, k.tomas, family.DirectionDescendants, 0)
	if err != nil {
		t.Fatalf("Tree(descendants): %v", err)
	}
	up, err := fam.Tree(ctx, k.tomas, family.DirectionAncestors, 0)
	if err != nil {
		t.Fatalf("Tree(ancestors): %v", err)
	}
	if len(down.Members) != 1 || len(up.Members) != 3 {
		t.Errorf("descendants = %v, ancestors = %v, want Tomáš alone and him with his parents",
			memberUIDs(down.Members), memberUIDs(up.Members))
	}
	if got := generations(t, up.Members); got[k.ludmila] != -1 {
		t.Errorf("the pedigree gives Ludmila generation %d, want -1", got[k.ludmila])
	}
}

// cousinMarriage is a cycle whose two paths disagree: Cyril marries Klára, the
// daughter of his cousin Dana. From Cyril, Dana is his mother-in-law two steps
// away and his cousin three steps away; the nearer reading names her generation.
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
func TestNetwork_aCousinMarriageIsWalkedOnceAndTheNearestWins(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000021", "cycle-editor")
	uid := map[string]string{}
	for _, name := range []string{"Gustav", "Gita", "Adam", "Alena", "Bára", "Cyril", "Dana", "David", "Klára", "Ema"} {
		uid[name] = makeSubject(t, ppl, name)
	}
	for _, rel := range []struct{ child, parent string }{
		{"Adam", "Gustav"}, {"Adam", "Gita"},
		{"Bára", "Gustav"}, {"Bára", "Gita"},
		{"Cyril", "Adam"}, {"Cyril", "Alena"},
		{"Dana", "Bára"},
		{"Klára", "Dana"}, {"Klára", "David"},
		{"Ema", "Cyril"}, {"Ema", "Klára"},
	} {
		if _, err := fam.AddParentAudited(ctx, uid[rel.child], uid[rel.parent], "",
			entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
			t.Fatalf("adding parent %s of %s: %v", rel.parent, rel.child, err)
		}
	}

	tree, err := fam.Tree(ctx, uid["Cyril"], family.DirectionNetwork, 0)
	if err != nil {
		t.Fatalf("Tree(network): %v", err)
	}
	got := generations(t, tree.Members)
	want := map[string]int{
		uid["Cyril"]: 0, uid["Klára"]: 0, uid["Ema"]: 1,
		uid["Adam"]: -1, uid["Alena"]: -1, uid["Bára"]: -1, uid["Dana"]: -1, uid["David"]: -1,
		uid["Gustav"]: -2, uid["Gita"]: -2,
	}
	if !maps.Equal(got, want) {
		t.Errorf("generations = %v, want %v", got, want)
	}
	if got[uid["Dana"]] != -1 {
		t.Errorf("Dana = %d, want -1: a mother-in-law two steps away beats a cousin three steps away",
			got[uid["Dana"]])
	}
	if len(tree.Members) != 10 || len(tree.Families) != 5 {
		t.Errorf("members = %d, families = %d, want 10 people and 5 families, each once",
			len(tree.Members), len(tree.Families))
	}
	assertClosed(t, tree)
}

func TestNetwork_pastTheCapKeepsTheNearest(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	k := seedKozaks(t, fam, ppl, db)

	tree, err := fam.NetworkWithin(ctx, k.tomas, 4)
	if err != nil {
		t.Fatalf("NetworkWithin: %v", err)
	}
	if !tree.Truncated || tree.Total != 5 {
		t.Errorf("truncated = %v, total = %d; want a five-person component under a cap of four "+
			"reported truncated, with all five counted", tree.Truncated, tree.Total)
	}
	want := map[string]int{k.tomas: 0, k.ludmila: -1, k.ales: -1, k.dagmar: -1}
	if got := generations(t, tree.Members); !maps.Equal(got, want) {
		t.Errorf("generations = %v, want the four nearest %v — the cousin is the one left out", got, want)
	}
	if len(tree.Families) != 2 {
		t.Errorf("families = %d, want the couple and the sibling group", len(tree.Families))
	}
	assertClosed(t, tree)

	whole, err := fam.NetworkWithin(ctx, k.tomas, 5)
	if err != nil {
		t.Fatalf("NetworkWithin: %v", err)
	}
	if whole.Truncated || len(whole.Members) != 5 || whole.Total != 5 {
		t.Errorf("a cap the component fits exactly = %d members, truncated %v; want 5, false",
			len(whole.Members), whole.Truncated)
	}
}

func TestNetwork_aPersonWithNoFamilyAndAMissingRoot(t *testing.T) {
	fam, ppl, _, _ := stores(t)
	ctx := context.Background()
	alone := makeSubject(t, ppl, "Samotář")

	tree, err := fam.Tree(ctx, alone, family.DirectionNetwork, 0)
	if err != nil {
		t.Fatalf("Tree(network): %v", err)
	}
	if len(tree.Members) != 1 || tree.Members[0].UID != alone || tree.Families == nil || len(tree.Families) != 0 {
		t.Errorf("tree = %+v, want the root alone and an empty (not nil) family list", tree)
	}
	if _, err := fam.Tree(ctx, "su_ghost", family.DirectionNetwork, 0); !errors.Is(err, family.ErrSubjectNotFound) {
		t.Errorf("Tree(missing root) = %v, want ErrSubjectNotFound", err)
	}
}
