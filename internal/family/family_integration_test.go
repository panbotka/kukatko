//go:build integration

package family_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photos"
)

// These tests run only under `make test-integration` against the database named
// by KUKATKO_TEST_DATABASE_URL. They exercise the genealogy against the real
// schema, because everything load-bearing about it lives there: the unique pair
// index, the one-family-per-child index and two recursive walks are not things a
// fake can be wrong about convincingly.

// stores returns the three stores the family tests seed through, over a truncated
// test database.
func stores(t *testing.T) (*family.Store, *people.Store, *photos.Store, *database.DB) {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	return family.NewStore(db.Pool()), people.NewStore(db.Pool()), photos.NewStore(db.Pool()), db
}

// makeSubject inserts a named person and returns its uid.
func makeSubject(t *testing.T, store *people.Store, name string) string {
	t.Helper()
	subj, err := store.CreateSubject(context.Background(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("creating subject %s: %v", name, err)
	}
	return subj.UID
}

// makeUser inserts a viewer account so audit rows have a valid actor to reference
// (audit_log.actor_uid is a foreign key to users), and returns its uid.
func makeUser(t *testing.T, db *database.DB, uid, username string) string {
	t.Helper()
	if err := auth.NewStore(db.Pool()).CreateUser(context.Background(), auth.User{
		UID:          uid,
		Username:     username,
		Email:        username + "@example.test",
		PasswordHash: "x",
		Role:         auth.RoleViewer,
	}); err != nil {
		t.Fatalf("creating user %s: %v", username, err)
	}
	return uid
}

// entryFor builds the audit entry a handler would hand the store, with the target
// deliberately left empty so the store's own default is what the tests observe.
func entryFor(actorUID, action string) audit.Entry {
	return audit.Entry{ActorUID: actorUID, Action: action, TargetType: "subjects"}
}

// village is the seeded family the walk tests read: three generations with a
// cousin marriage at the bottom, which is what makes the descendant walk a
// diamond rather than a tree.
//
//	     Bohumil ⚭ Marie
//	      /             \
//	Josef ⚭ Ludmila   Anna ⚭ Karel
//	    |                  |
//	  Petr       ⚭        Eva        (first cousins, as villages do)
//	             |
//	            Jan
//
// Jan is reachable from Bohumil through his father and through his mother, so
// UNION ALL would return him twice; UNION returns him once.
type village struct {
	bohumil, marie   string
	josef, ludmila   string
	anna, karel      string
	petr, eva, jan   string
	actor            string
	familyOfChildren map[string]string
}

// seedVillage builds the three-generation family above and returns its uids.
func seedVillage(t *testing.T, fam *family.Store, ppl *people.Store, db *database.DB) village {
	t.Helper()
	ctx := context.Background()
	v := village{
		bohumil: makeSubject(t, ppl, "Bohumil Nečas"),
		marie:   makeSubject(t, ppl, "Marie Nečasová"),
		josef:   makeSubject(t, ppl, "Josef Nečas"),
		ludmila: makeSubject(t, ppl, "Ludmila Nečasová"),
		anna:    makeSubject(t, ppl, "Anna Skotáková"),
		karel:   makeSubject(t, ppl, "Karel Skoták"),
		petr:    makeSubject(t, ppl, "Petr Nečas"),
		eva:     makeSubject(t, ppl, "Eva Skotáková"),
		jan:     makeSubject(t, ppl, "Jan Nečas"),
		actor:   makeUser(t, db, "usfamily0000000000000001", "genealogist"),
	}
	v.familyOfChildren = map[string]string{}
	for _, rel := range []struct{ child, parentA, parentB string }{
		{child: v.josef, parentA: v.bohumil, parentB: v.marie},
		{child: v.anna, parentA: v.bohumil, parentB: v.marie},
		{child: v.petr, parentA: v.josef, parentB: v.ludmila},
		{child: v.eva, parentA: v.anna, parentB: v.karel},
		{child: v.jan, parentA: v.petr, parentB: v.eva},
	} {
		for _, parent := range []string{rel.parentA, rel.parentB} {
			got, err := fam.AddParentAudited(ctx, rel.child, parent, "",
				entryFor(v.actor, audit.ActionSubjectRelationAdd))
			if err != nil {
				t.Fatalf("adding parent %s of %s: %v", parent, rel.child, err)
			}
			v.familyOfChildren[rel.child] = got.UID
		}
	}
	return v
}

// memberDepths maps a walk's result to uid → depth, failing the test if any uid
// appears twice: a person drawn twice is exactly what UNION ALL would produce
// once cousins marry.
func memberDepths(t *testing.T, members []family.Member) map[string]int {
	t.Helper()
	depths := make(map[string]int, len(members))
	for _, m := range members {
		if _, seen := depths[m.UID]; seen {
			t.Errorf("%s (%s) appears twice in the walk", m.Name, m.UID)
		}
		depths[m.UID] = m.Depth
	}
	return depths
}

func TestDescendants_cousinMarriageIsWalkedOnce(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	members, err := fam.Descendants(ctx, v.bohumil, family.DescendantOptions{})
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	depths := memberDepths(t, members)
	want := map[string]int{
		v.bohumil: 0, v.josef: 1, v.anna: 1, v.petr: 2, v.eva: 2, v.jan: 3,
	}
	if len(depths) != len(want) {
		t.Fatalf("walked %d people, want %d (%v)", len(depths), len(want), depths)
	}
	for uid, depth := range want {
		if got, ok := depths[uid]; !ok || got != depth {
			t.Errorf("%s: depth %d (present=%v), want %d", uid, got, ok, depth)
		}
	}
}

func TestDescendants_withPartnersAddsTheOnesWhoMarriedIn(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	members, err := fam.Descendants(ctx, v.bohumil, family.DescendantOptions{WithPartners: true})
	if err != nil {
		t.Fatalf("Descendants: %v", err)
	}
	depths := memberDepths(t, members)
	if len(depths) != 9 {
		t.Fatalf("walked %d people, want 9 (%v)", len(depths), depths)
	}
	for _, uid := range []string{v.marie, v.ludmila, v.karel} {
		if _, ok := depths[uid]; !ok {
			t.Errorf("%s married in but is missing from the walk", uid)
		}
	}
	flags := map[string]bool{}
	for _, m := range members {
		flags[m.UID] = m.Partner
	}
	if !flags[v.ludmila] {
		t.Error("Ludmila is in the set only as a partner, want Partner=true")
	}
	// Petr and Eva are partners of each other *and* descendants; descent wins, so
	// neither is flagged as having married in.
	if flags[v.petr] || flags[v.eva] {
		t.Errorf("the cousins are descendants, want Partner=false, got petr=%v eva=%v",
			flags[v.petr], flags[v.eva])
	}
}

func TestAncestors_theDiamondIsClimbedOnce(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	members, err := fam.Ancestors(ctx, v.jan, 3)
	if err != nil {
		t.Fatalf("Ancestors: %v", err)
	}
	depths := memberDepths(t, members)
	want := map[string]int{
		v.jan: 0, v.petr: 1, v.eva: 1,
		v.josef: 2, v.ludmila: 2, v.anna: 2, v.karel: 2,
		v.bohumil: 3, v.marie: 3,
	}
	if len(depths) != len(want) {
		t.Fatalf("climbed to %d people, want %d (%v)", len(depths), len(want), depths)
	}
	for uid, depth := range want {
		if got, ok := depths[uid]; !ok || got != depth {
			t.Errorf("%s: depth %d (present=%v), want %d", uid, got, ok, depth)
		}
	}

	bounded, err := fam.Ancestors(ctx, v.jan, 1)
	if err != nil {
		t.Fatalf("Ancestors(1): %v", err)
	}
	if len(bounded) != 3 {
		t.Errorf("one generation up = %d people, want 3 (Jan and his parents)", len(bounded))
	}
}

func TestRelations_areDerivedFromTheFamilyRows(t *testing.T) {
	fam, ppl, photoStore, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	// One photo of Anna, so the sibling chip carries a count rather than a guess.
	photo, err := photoStore.Create(ctx, photos.Photo{
		FileHash: "familyhash1", FilePath: "2024/01/a.jpg", FileName: "a.jpg",
		FileWidth: 4000, FileHeight: 3000,
	})
	if err != nil {
		t.Fatalf("creating photo: %v", err)
	}
	if _, err := ppl.CreateMarker(ctx, people.Marker{
		PhotoUID: photo.UID, SubjectUID: &v.anna, Type: people.MarkerFace,
		X: 0.1, Y: 0.1, W: 0.2, H: 0.2,
	}); err != nil {
		t.Fatalf("creating marker: %v", err)
	}

	rel, err := fam.Relations(ctx, v.josef)
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if got := uidsOf(rel.Parents); len(got) != 2 {
		t.Errorf("parents = %v, want Bohumil and Marie", got)
	}
	if len(rel.Siblings) != 1 || rel.Siblings[0].UID != v.anna {
		t.Fatalf("siblings = %v, want only Anna", uidsOf(rel.Siblings))
	}
	if rel.Siblings[0].PhotoCount != 1 {
		t.Errorf("Anna's photo count = %d, want 1", rel.Siblings[0].PhotoCount)
	}
	if len(rel.Children) != 1 || rel.Children[0].UID != v.petr {
		t.Errorf("children = %v, want only Petr", uidsOf(rel.Children))
	}
	if len(rel.Partners) != 1 {
		t.Fatalf("partnerships = %d, want 1", len(rel.Partners))
	}
	if rel.Partners[0].Partner == nil || rel.Partners[0].Partner.UID != v.ludmila {
		t.Errorf("partner = %v, want Ludmila", rel.Partners[0].Partner)
	}
	if rel.Partners[0].Family.UID != v.familyOfChildren[v.petr] {
		t.Errorf("partnership family = %s, want Petr's family %s",
			rel.Partners[0].Family.UID, v.familyOfChildren[v.petr])
	}
}

// uidsOf lists the uids of a relative list, for readable failure messages.
func uidsOf(rels []family.Relative) []string {
	out := make([]string, 0, len(rels))
	for _, rel := range rels {
		out = append(out, rel.UID)
	}
	return out
}

func TestTree_carriesTheFamilyBoxesOfTheWalkedSet(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	tree, err := fam.Tree(ctx, v.bohumil, family.DirectionDescendants, 0)
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	if tree.Root.UID != v.bohumil {
		t.Errorf("root = %s, want %s", tree.Root.UID, v.bohumil)
	}
	if len(tree.Members) != 9 {
		t.Errorf("members = %d, want 9", len(tree.Members))
	}
	// Four couples: the root pair, both of their children's marriages, and the
	// cousins' own.
	if len(tree.Families) != 4 {
		t.Fatalf("family boxes = %d, want 4", len(tree.Families))
	}
	children := map[string][]string{}
	for _, box := range tree.Families {
		children[box.UID] = box.ChildUIDs
	}
	if got := children[v.familyOfChildren[v.josef]]; len(got) != 2 {
		t.Errorf("the root couple's box lists %v, want two children", got)
	}
	if _, err := fam.Tree(ctx, v.bohumil, "sideways", 0); !errors.Is(err, family.ErrInvalidKind) {
		t.Errorf("Tree(sideways) = %v, want ErrInvalidKind", err)
	}
}

func TestAddParent_refusesASecondParentage(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	stranger := makeSubject(t, ppl, "Cizí Člověk")
	_, err := fam.AddParentAudited(ctx, v.petr, stranger, "", entryFor(v.actor, audit.ActionSubjectRelationAdd))
	if !errors.Is(err, family.ErrAlreadyChild) {
		t.Fatalf("a third parent = %v, want ErrAlreadyChild", err)
	}

	// The index behind that rule is the one that keeps the walk a tree, so it is
	// asserted against the schema directly and not only through the store.
	other, err := fam.AddPartnerAudited(ctx, stranger, v.marie, entryFor(v.actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("creating a second couple: %v", err)
	}
	_, err = db.Pool().Exec(ctx,
		"INSERT INTO subject_family_children (family_uid, child_uid, kind) VALUES ($1, $2, 'birth')",
		other.UID, v.petr)
	if err == nil {
		t.Fatal("the database accepted a second parentage for one child")
	}
	if !strings.Contains(err.Error(), "idx_subject_family_children_child") {
		t.Errorf("second parentage refused by %v, want idx_subject_family_children_child", err)
	}
}

func TestAddParent_refusesAnAncestorAsAChild(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	// Bohumil is Jan's great-grandfather; making Jan his parent would close a loop
	// that no walk could terminate on. Bohumil is nobody's child yet, so the only
	// thing refusing this is the cycle check.
	_, err := fam.AddParentAudited(ctx, v.bohumil, v.jan, "", entryFor(v.actor, audit.ActionSubjectRelationAdd))
	if !errors.Is(err, family.ErrCycle) {
		t.Fatalf("attaching a descendant as a parent = %v, want ErrCycle", err)
	}
	_, err = fam.AddParentAudited(ctx, v.josef, v.josef, "", entryFor(v.actor, audit.ActionSubjectRelationAdd))
	if !errors.Is(err, family.ErrSelfRelation) {
		t.Fatalf("a subject as its own parent = %v, want ErrSelfRelation", err)
	}
}

func TestAddParent_completesALoneParentFamily(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000002", "curator")
	mother := makeSubject(t, ppl, "Matka")
	father := makeSubject(t, ppl, "Otec")
	child := makeSubject(t, ppl, "Dítě")

	lone, err := fam.AddParentAudited(ctx, child, mother, family.ChildAdopted,
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("adding the mother: %v", err)
	}
	completed, err := fam.AddParentAudited(ctx, child, father, "",
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("adding the father: %v", err)
	}
	if completed.UID != lone.UID {
		t.Errorf("the second parent made family %s, want the child's own %s", completed.UID, lone.UID)
	}
	if completed.PartnerA == nil || completed.PartnerB == nil {
		t.Fatalf("family = %+v, want both partners recorded", completed)
	}
	if *completed.PartnerA >= *completed.PartnerB {
		t.Errorf("partners %s/%s are not in byte order", *completed.PartnerA, *completed.PartnerB)
	}
	rel, err := fam.Relations(ctx, child)
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(rel.Parents) != 2 {
		t.Fatalf("parents = %v, want both", uidsOf(rel.Parents))
	}
	if rel.Parents[0].ChildKind != family.ChildAdopted {
		t.Errorf("child kind = %q, want it kept as %q", rel.Parents[0].ChildKind, family.ChildAdopted)
	}
}

func TestAddParent_aSecondChildOfTheSameMotherKeepsHerAlone(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000006", "recorder")
	mother := makeSubject(t, ppl, "Matka dvou")
	father := makeSubject(t, ppl, "Otec jednoho")
	first := makeSubject(t, ppl, "Starší")
	second := makeSubject(t, ppl, "Mladší")

	for _, child := range []string{first, second} {
		if _, err := fam.AddParentAudited(ctx, child, mother, "",
			entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
			t.Fatalf("adding the mother of %s: %v", child, err)
		}
	}
	// Naming a father for one of them must not hand the other one the same father
	// by rewriting the pair of the family they happen to share.
	if _, err := fam.AddParentAudited(ctx, first, father, "",
		entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
		t.Fatalf("adding the father: %v", err)
	}
	elder, err := fam.Relations(ctx, first)
	if err != nil {
		t.Fatalf("Relations(elder): %v", err)
	}
	if len(elder.Parents) != 2 {
		t.Errorf("the elder's parents = %v, want both", uidsOf(elder.Parents))
	}
	younger, err := fam.Relations(ctx, second)
	if err != nil {
		t.Fatalf("Relations(younger): %v", err)
	}
	if len(younger.Parents) != 1 || younger.Parents[0].UID != mother {
		t.Fatalf("the younger's parents = %v, want only the mother", uidsOf(younger.Parents))
	}
	// They are half-siblings, which the model gets right for free: each is a child
	// of a different family, and only the mother's own family ties them.
	if len(younger.Siblings) != 0 {
		t.Errorf("the younger's siblings = %v, want none — they are half-siblings now",
			uidsOf(younger.Siblings))
	}
	mothersChildren, err := fam.Relations(ctx, mother)
	if err != nil {
		t.Fatalf("Relations(mother): %v", err)
	}
	if len(mothersChildren.Children) != 2 {
		t.Errorf("the mother's children = %v, want both", uidsOf(mothersChildren.Children))
	}
}

func TestRemoveRelation_aChildKeepsTheOtherParent(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	if err := fam.RemoveRelationAudited(ctx, v.josef, v.petr,
		entryFor(v.actor, audit.ActionSubjectRelationRemove)); err != nil {
		t.Fatalf("removing Josef as Petr's father: %v", err)
	}
	rel, err := fam.Relations(ctx, v.petr)
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(rel.Parents) != 1 || rel.Parents[0].UID != v.ludmila {
		t.Fatalf("parents = %v, want only Ludmila", uidsOf(rel.Parents))
	}
	if err := fam.RemoveRelationAudited(ctx, v.ludmila, v.petr,
		entryFor(v.actor, audit.ActionSubjectRelationRemove)); err != nil {
		t.Fatalf("removing the last parent: %v", err)
	}
	rel, err = fam.Relations(ctx, v.petr)
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(rel.Parents) != 0 {
		t.Errorf("parents = %v, want none", uidsOf(rel.Parents))
	}
	err = fam.RemoveRelationAudited(ctx, v.ludmila, v.petr,
		entryFor(v.actor, audit.ActionSubjectRelationRemove))
	if !errors.Is(err, family.ErrRelationNotFound) {
		t.Errorf("removing it twice = %v, want ErrRelationNotFound", err)
	}
}

func TestRemoveRelation_partnersPartAndTheChildrenStay(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	if err := fam.RemoveRelationAudited(ctx, v.bohumil, v.marie,
		entryFor(v.actor, audit.ActionSubjectRelationRemove)); err != nil {
		t.Fatalf("separating the couple: %v", err)
	}
	rel, err := fam.Relations(ctx, v.josef)
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(rel.Parents) != 1 || rel.Parents[0].UID != v.bohumil {
		t.Errorf("parents = %v, want the remaining Bohumil", uidsOf(rel.Parents))
	}
	if len(rel.Siblings) != 1 || rel.Siblings[0].UID != v.anna {
		t.Errorf("siblings = %v, want Anna to stay", uidsOf(rel.Siblings))
	}
	marie, err := fam.Relations(ctx, v.marie)
	if err != nil {
		t.Fatalf("Relations(Marie): %v", err)
	}
	if len(marie.Partners) != 0 || len(marie.Children) != 0 {
		t.Errorf("Marie still holds %d partnerships and %d children",
			len(marie.Partners), len(marie.Children))
	}
}

func TestUpdateFamily_editsTheUnionItself(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000003", "editor")
	a := makeSubject(t, ppl, "Jeden")
	b := makeSubject(t, ppl, "Druhá")

	created, err := fam.AddPartnerAudited(ctx, a, b, entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("AddPartnerAudited: %v", err)
	}
	if created.Kind != family.KindPartnership {
		t.Errorf("kind = %q, want the %q default", created.Kind, family.KindPartnership)
	}
	again, err := fam.AddPartnerAudited(ctx, b, a, entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("AddPartnerAudited twice: %v", err)
	}
	if again.UID != created.UID {
		t.Errorf("the same couple made two families (%s, %s)", created.UID, again.UID)
	}

	from, to := 1948, 1991
	updated, err := fam.UpdateFamilyAudited(ctx, created.UID, family.Update{
		Kind: family.KindMarriage, FromYear: &from, ToYear: &to, Note: "svatba ve Sloupu",
	}, audit.Entry{ActorUID: actor, Action: audit.ActionFamilyUpdate, TargetType: "families"})
	if err != nil {
		t.Fatalf("UpdateFamilyAudited: %v", err)
	}
	if updated.Kind != family.KindMarriage || updated.FromYear == nil || *updated.FromYear != from {
		t.Errorf("updated = %+v, want a marriage from %d", updated, from)
	}
	_, err = fam.UpdateFamilyAudited(ctx, created.UID, family.Update{Kind: "engagement"},
		audit.Entry{ActorUID: actor, Action: audit.ActionFamilyUpdate})
	if !errors.Is(err, family.ErrInvalidKind) {
		t.Errorf("an unknown kind = %v, want ErrInvalidKind", err)
	}
	_, err = fam.UpdateFamilyAudited(ctx, "fmmissing", family.Update{},
		audit.Entry{ActorUID: actor, Action: audit.ActionFamilyUpdate})
	if !errors.Is(err, family.ErrFamilyNotFound) {
		t.Errorf("a missing family = %v, want ErrFamilyNotFound", err)
	}
}

func TestAudit_theRowLandsInTheMutationsTransaction(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	auditStore := audit.NewStore(db.Pool())
	actor := makeUser(t, db, "usfamily0000000000000004", "archivist")
	parent := makeSubject(t, ppl, "Rodič")
	child := makeSubject(t, ppl, "Potomek")

	if _, err := fam.AddParentAudited(ctx, child, parent, "",
		entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
		t.Fatalf("AddParentAudited: %v", err)
	}
	recs, err := auditStore.List(ctx, audit.Filter{Action: audit.ActionSubjectRelationAdd, Limit: 10})
	if err != nil {
		t.Fatalf("listing audit rows: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(recs))
	}
	if recs[0].ActorUID == nil || *recs[0].ActorUID != actor {
		t.Errorf("actor = %v, want %s", recs[0].ActorUID, actor)
	}
	if recs[0].TargetUID == nil || *recs[0].TargetUID != child {
		t.Errorf("target = %v, want the child %s — a target stamped inside the closure is lost",
			recs[0].TargetUID, child)
	}

	// A refused mutation rolls back, and its audit row goes with it: the trail
	// must not claim a relation that was never recorded.
	if _, err := fam.AddParentAudited(ctx, parent, child, "",
		entryFor(actor, audit.ActionSubjectRelationAdd)); !errors.Is(err, family.ErrCycle) {
		t.Fatalf("the cycle was not refused: %v", err)
	}
	recs, err = auditStore.List(ctx, audit.Filter{Action: audit.ActionSubjectRelationAdd, Limit: 10})
	if err != nil {
		t.Fatalf("listing audit rows: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("audit rows after the refusal = %d, want the original 1", len(recs))
	}
}

func TestStore_missingSubjectsAreSentinels(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000005", "tester")
	known := makeSubject(t, ppl, "Známá")

	if _, err := fam.Relations(ctx, "sumissing"); !errors.Is(err, family.ErrSubjectNotFound) {
		t.Errorf("Relations(missing) = %v, want ErrSubjectNotFound", err)
	}
	if _, err := fam.Descendants(ctx, "sumissing", family.DescendantOptions{}); !errors.Is(
		err, family.ErrSubjectNotFound) {
		t.Errorf("Descendants(missing) = %v, want ErrSubjectNotFound", err)
	}
	if _, err := fam.AddPartnerAudited(ctx, known, "sumissing",
		entryFor(actor, audit.ActionSubjectRelationAdd)); !errors.Is(err, family.ErrSubjectNotFound) {
		t.Errorf("AddPartnerAudited(missing) = %v, want ErrSubjectNotFound", err)
	}
}
