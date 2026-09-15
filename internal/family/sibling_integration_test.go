//go:build integration

package family_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/familyexport"
)

// These cover the sibling group: the family with no partners at all that
// migration 0076 opened the schema for. Everything load-bearing about it is in
// the database — the partial unique pair index, the one-family-per-child index,
// and a pruning rule that reads the rows back — so none of it can be proved
// against a fake.

// addSibling records otherUID as a sibling of subjectUID through the one write
// path the API and the CLI both use.
func addSibling(t *testing.T, fam *family.Store, actor, subjectUID, otherUID string) family.AddResult {
	t.Helper()
	res, err := fam.AddRelationAudited(context.Background(), subjectUID,
		family.AddRelation{Role: family.RoleSibling, SubjectUID: otherUID},
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("recording %s as a sibling of %s: %v", otherUID, subjectUID, err)
	}
	return res
}

// siblingUIDs reads one subject's siblings as a set of uids.
func siblingUIDs(t *testing.T, fam *family.Store, subjectUID string) []string {
	t.Helper()
	rel, err := fam.Relations(context.Background(), subjectUID)
	if err != nil {
		t.Fatalf("Relations(%s): %v", subjectUID, err)
	}
	return uidsOf(rel.Siblings)
}

// memberUIDs names the people a walk returned, for a readable failure.
func memberUIDs(members []family.Member) []string {
	uids := make([]string, 0, len(members))
	for _, member := range members {
		uids = append(uids, member.UID)
	}
	return uids
}

// countSubjects counts the people in the library, which is how the tests prove no
// placeholder "unknown parent" was invented behind the reader's back.
func countSubjects(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(), "SELECT COUNT(*) FROM subjects").Scan(&n); err != nil {
		t.Fatalf("counting subjects: %v", err)
	}
	return n
}

// countFamilies counts the family rows, which is how the tests prove a group was
// joined rather than duplicated.
func countFamilies(t *testing.T, db *database.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM subject_families").Scan(&n); err != nil {
		t.Fatalf("counting families: %v", err)
	}
	return n
}

// TestSibling_withNoParents records the relation this whole change exists for:
// two people known to be brother and sister whose parents are not in the library.
// It must produce a family with no partners, no placeholder person, and a second
// such group must be able to exist beside the first — which is what the pair
// index being partial buys.
func TestSibling_withNoParents(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000010", "sibling-editor")
	josef := makeSubject(t, ppl, "Josef Nečas")
	anna := makeSubject(t, ppl, "Anna Nečasová")
	before := countSubjects(t, db)

	res := addSibling(t, fam, actor, josef, anna)
	if res.Family.PartnerA != nil || res.Family.PartnerB != nil {
		t.Errorf("family = %+v, want no partner recorded", res.Family)
	}
	if res.Created {
		t.Error("recording a sibling of an existing subject reported a creation")
	}
	if got := countSubjects(t, db); got != before {
		t.Errorf("subjects = %d, want the %d seeded: no placeholder parent may be invented", got, before)
	}

	// Derived both ways, from one row.
	if got := siblingUIDs(t, fam, josef); !slices.Equal(got, []string{anna}) {
		t.Errorf("Josef's siblings = %v, want [Anna]", got)
	}
	if got := siblingUIDs(t, fam, anna); !slices.Equal(got, []string{josef}) {
		t.Errorf("Anna's siblings = %v, want [Josef]", got)
	}
	rel, err := fam.Relations(ctx, josef)
	if err != nil {
		t.Fatalf("Relations(Josef): %v", err)
	}
	if len(rel.Parents) != 0 || len(rel.Partners) != 0 {
		t.Errorf("a sibling group gave Josef %d parents and %d partnerships, want none",
			len(rel.Parents), len(rel.Partners))
	}

	// A third sibling joins the same group rather than starting another.
	marie := makeSubject(t, ppl, "Marie Nečasová")
	third := addSibling(t, fam, actor, anna, marie)
	if third.Family.UID != res.Family.UID {
		t.Errorf("the third sibling landed in family %s, want the group %s", third.Family.UID, res.Family.UID)
	}
	if got := siblingUIDs(t, fam, marie); len(got) != 2 {
		t.Errorf("Marie's siblings = %v, want both of them", got)
	}

	// The walks read the partner columns, so a family naming nobody is simply not
	// on any of their paths. That must be a quiet skip rather than a broken walk:
	// a group member's tree is themselves, and asking for it is not an error.
	tree, err := fam.Tree(ctx, josef, family.DirectionDescendants, 0)
	if err != nil {
		t.Fatalf("Tree(Josef) over a sibling group: %v", err)
	}
	if len(tree.Members) != 1 || tree.Members[0].UID != josef {
		t.Errorf("descendant tree = %v, want only Josef himself", memberUIDs(tree.Members))
	}
	if len(tree.Families) != 0 {
		t.Errorf("descendant tree drew %d family boxes, want none for a group nobody parents", len(tree.Families))
	}
	up, err := fam.Tree(ctx, josef, family.DirectionAncestors, 0)
	if err != nil {
		t.Fatalf("Tree(Josef, ancestors) over a sibling group: %v", err)
	}
	if len(up.Members) != 1 {
		t.Errorf("pedigree = %v, want only Josef: the group records nobody above him", memberUIDs(up.Members))
	}

	// A second, unrelated group: the pre-0076 index would have collapsed the two
	// into one row, since NULLS NOT DISTINCT makes every all-NULL pair equal.
	petr, eva := makeSubject(t, ppl, "Petr Doležal"), makeSubject(t, ppl, "Eva Doležalová")
	other := addSibling(t, fam, actor, petr, eva)
	if other.Family.UID == res.Family.UID {
		t.Fatal("the second sibling group reused the first one's family row")
	}
	if got := siblingUIDs(t, fam, petr); !slices.Equal(got, []string{eva}) {
		t.Errorf("Petr's siblings = %v, want only Eva", got)
	}
	if got := countFamilies(t, db); got != 2 {
		t.Errorf("families = %d, want the two groups", got)
	}
}

// TestSibling_withKnownParentsIsUnchanged verifies the case that already worked
// keeps working: a sibling of somebody whose parents are recorded becomes another
// child of *their* family, not a group of its own.
func TestSibling_withKnownParentsIsUnchanged(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000011", "sibling-editor")
	mother := makeSubject(t, ppl, "Marie Nečasová")
	josef := makeSubject(t, ppl, "Josef Nečas")
	anna := makeSubject(t, ppl, "Anna Nečasová")

	parentage, err := fam.AddParentAudited(ctx, josef, mother, "",
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("AddParentAudited: %v", err)
	}
	res := addSibling(t, fam, actor, josef, anna)
	if res.Family.UID != parentage.UID {
		t.Errorf("the sibling landed in family %s, want Josef's own %s", res.Family.UID, parentage.UID)
	}
	if got := countFamilies(t, db); got != 1 {
		t.Errorf("families = %d, want the one the mother already had", got)
	}
	annaRel, err := fam.Relations(ctx, anna)
	if err != nil {
		t.Fatalf("Relations(Anna): %v", err)
	}
	if len(annaRel.Parents) != 1 || annaRel.Parents[0].UID != mother {
		t.Errorf("Anna's parents = %v, want the mother the sibling hangs off", uidsOf(annaRel.Parents))
	}
}

// TestSibling_aParentAdoptsTheWholeGroup verifies the rule that keeps a group
// together: a parent recorded on any one of its children becomes the parent of
// all of them, on the family they already share, and never a second family for
// the one child the relation was typed on.
func TestSibling_aParentAdoptsTheWholeGroup(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000012", "sibling-editor")
	josef := makeSubject(t, ppl, "Josef Nečas")
	anna := makeSubject(t, ppl, "Anna Nečasová")
	marie := makeSubject(t, ppl, "Marie Nečasová")
	mother := makeSubject(t, ppl, "Ludmila Nečasová")

	group := addSibling(t, fam, actor, josef, anna).Family
	addSibling(t, fam, actor, josef, marie)

	adopted, err := fam.AddParentAudited(ctx, anna, mother, "",
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("AddParentAudited onto a sibling group: %v", err)
	}
	if adopted.UID != group.UID {
		t.Errorf("the parent landed in family %s, want the group's own %s", adopted.UID, group.UID)
	}
	if got := countFamilies(t, db); got != 1 {
		t.Errorf("families = %d, want the group to have gained the parent rather than split", got)
	}
	for _, child := range []string{josef, anna, marie} {
		rel, relErr := fam.Relations(ctx, child)
		if relErr != nil {
			t.Fatalf("Relations(%s): %v", child, relErr)
		}
		if len(rel.Parents) != 1 || rel.Parents[0].UID != mother {
			t.Errorf("%s's parents = %v, want the mother the whole group gained", child, uidsOf(rel.Parents))
		}
		if len(rel.Siblings) != 2 {
			t.Errorf("%s's siblings = %v, want the other two", child, uidsOf(rel.Siblings))
		}
	}
}

// TestSibling_aParentWithAnOwnFamilyTakesTheGroupIn verifies the other half of
// that rule: when the parent already has a lone-parent family, the group moves
// into it whole — one lone parent is exactly one family, and their children must
// not end up scattered across two rows nothing joins back together.
func TestSibling_aParentWithAnOwnFamilyTakesTheGroupIn(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000013", "sibling-editor")
	mother := makeSubject(t, ppl, "Ludmila Nečasová")
	petr := makeSubject(t, ppl, "Petr Nečas")
	josef := makeSubject(t, ppl, "Josef Nečas")
	anna := makeSubject(t, ppl, "Anna Nečasová")

	own, err := fam.AddParentAudited(ctx, petr, mother, "", entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("AddParentAudited: %v", err)
	}
	group := addSibling(t, fam, actor, josef, anna).Family

	// The adopted child keeps the kind it was given; the rest of the group keeps
	// theirs, which is what moving a whole membership set has to preserve.
	landed, err := fam.AddParentAudited(ctx, josef, mother, family.ChildAdopted,
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if err != nil {
		t.Fatalf("AddParentAudited onto a group whose parent already has a family: %v", err)
	}
	if landed.UID != own.UID {
		t.Errorf("the group landed in family %s, want the mother's own %s", landed.UID, own.UID)
	}
	if got := countFamilies(t, db); got != 1 {
		t.Errorf("families = %d, want only the mother's; the emptied group must be pruned", got)
	}
	if _, err := fam.GetFamily(ctx, group.UID); !errors.Is(err, family.ErrFamilyNotFound) {
		t.Errorf("the emptied group %s = %v, want it gone", group.UID, err)
	}
	rel, err := fam.Relations(ctx, josef)
	if err != nil {
		t.Fatalf("Relations(Josef): %v", err)
	}
	if len(rel.Siblings) != 2 {
		t.Errorf("Josef's siblings = %v, want Anna and the mother's own child", uidsOf(rel.Siblings))
	}
	annaRel, err := fam.Relations(ctx, anna)
	if err != nil {
		t.Fatalf("Relations(Anna): %v", err)
	}
	if len(annaRel.Parents) != 1 || annaRel.Parents[0].UID != mother {
		t.Errorf("Anna's parents = %v, want the mother her brother was given", uidsOf(annaRel.Parents))
	}
	for _, sibling := range rel.Siblings {
		if sibling.UID == anna && sibling.ChildKind != family.ChildBirth {
			t.Errorf("Anna's membership became %q, want it kept as %q", sibling.ChildKind, family.ChildBirth)
		}
	}
	josefKind := ""
	for _, sibling := range annaRel.Siblings {
		if sibling.UID == josef {
			josefKind = string(sibling.ChildKind)
		}
	}
	if josefKind != string(family.ChildAdopted) {
		t.Errorf("Josef's membership = %q, want the %q he was recorded with", josefKind, family.ChildAdopted)
	}
}

// TestSibling_twoDifferentFamiliesAreRefused verifies the refusal that keeps a
// recorded parentage safe: making two people siblings when each is already a
// child somewhere would mean moving one of them, and this model will not do that
// quietly.
func TestSibling_twoDifferentFamiliesAreRefused(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000014", "sibling-editor")
	oneMother := makeSubject(t, ppl, "Marie Nečasová")
	otherMother := makeSubject(t, ppl, "Ludmila Doležalová")
	josef := makeSubject(t, ppl, "Josef Nečas")
	petr := makeSubject(t, ppl, "Petr Doležal")

	for _, pair := range []struct{ child, parent string }{
		{child: josef, parent: oneMother}, {child: petr, parent: otherMother},
	} {
		if _, err := fam.AddParentAudited(ctx, pair.child, pair.parent, "",
			entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
			t.Fatalf("AddParentAudited(%s): %v", pair.child, err)
		}
	}

	_, err := fam.AddRelationAudited(ctx, josef,
		family.AddRelation{Role: family.RoleSibling, SubjectUID: petr},
		entryFor(actor, audit.ActionSubjectRelationAdd))
	if !errors.Is(err, family.ErrDifferentFamilies) {
		t.Fatalf("siblings across two families = %v, want ErrDifferentFamilies", err)
	}
	// Neither parentage moved, and the refusal rolled its audit row back with it.
	for _, pair := range []struct{ child, parent string }{
		{child: josef, parent: oneMother}, {child: petr, parent: otherMother},
	} {
		rel, relErr := fam.Relations(ctx, pair.child)
		if relErr != nil {
			t.Fatalf("Relations(%s): %v", pair.child, relErr)
		}
		if len(rel.Parents) != 1 || rel.Parents[0].UID != pair.parent {
			t.Errorf("%s's parents = %v, want the untouched %s", pair.child, uidsOf(rel.Parents), pair.parent)
		}
		if len(rel.Siblings) != 0 {
			t.Errorf("%s gained siblings %v from a refused write", pair.child, uidsOf(rel.Siblings))
		}
	}
}

// TestSibling_removalRules verifies both halves of the removal rule: a sibling
// link with no parent behind it is the whole relation and goes, a group that
// drops below two children goes with it, and a pair who are siblings *through* a
// recorded parent has no row to remove at all.
func TestSibling_removalRules(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000015", "sibling-editor")
	josef := makeSubject(t, ppl, "Josef Nečas")
	anna := makeSubject(t, ppl, "Anna Nečasová")
	marie := makeSubject(t, ppl, "Marie Nečasová")

	group := addSibling(t, fam, actor, josef, anna).Family
	addSibling(t, fam, actor, josef, marie)

	// Three children: removing one leaves a group that still records something.
	if err := fam.RemoveRelationAudited(ctx, josef, marie,
		entryFor(actor, audit.ActionSubjectRelationRemove)); err != nil {
		t.Fatalf("removing a parentless sibling: %v", err)
	}
	if got := siblingUIDs(t, fam, marie); len(got) != 0 {
		t.Errorf("Marie's siblings = %v, want none left", got)
	}
	if got := siblingUIDs(t, fam, josef); !slices.Equal(got, []string{anna}) {
		t.Errorf("Josef's siblings = %v, want Anna to stay", got)
	}

	// Two left: removing one more leaves nobody a sibling of anybody, so the row
	// records nothing and is deleted rather than kept as a family of one.
	if err := fam.RemoveRelationAudited(ctx, josef, anna,
		entryFor(actor, audit.ActionSubjectRelationRemove)); err != nil {
		t.Fatalf("removing the last parentless sibling: %v", err)
	}
	if _, err := fam.GetFamily(ctx, group.UID); !errors.Is(err, family.ErrFamilyNotFound) {
		t.Errorf("the emptied group %s = %v, want it deleted", group.UID, err)
	}
	if got := countFamilies(t, db); got != 0 {
		t.Errorf("families = %d, want none left", got)
	}

	// With a parent on the family, being siblings follows from that parent and
	// there is nothing between the two to remove.
	mother := makeSubject(t, ppl, "Ludmila Nečasová")
	for _, child := range []string{josef, anna} {
		if _, err := fam.AddParentAudited(ctx, child, mother, "",
			entryFor(actor, audit.ActionSubjectRelationAdd)); err != nil {
			t.Fatalf("AddParentAudited(%s): %v", child, err)
		}
	}
	err := fam.RemoveRelationAudited(ctx, josef, anna, entryFor(actor, audit.ActionSubjectRelationRemove))
	if !errors.Is(err, family.ErrSiblingsDerived) {
		t.Fatalf("removing a derived sibling = %v, want ErrSiblingsDerived", err)
	}
	if got := siblingUIDs(t, fam, josef); !slices.Equal(got, []string{anna}) {
		t.Errorf("Josef's siblings = %v, want the refusal to have changed nothing", got)
	}

	// And two people who share nothing are not related at all.
	stranger := makeSubject(t, ppl, "Cizí Člověk")
	if err := fam.RemoveRelationAudited(ctx, josef, stranger,
		entryFor(actor, audit.ActionSubjectRelationRemove)); !errors.Is(err, family.ErrRelationNotFound) {
		t.Errorf("removing nothing = %v, want ErrRelationNotFound", err)
	}
}

// TestSibling_exportRoundTripsAParentlessFamily verifies the group survives the
// trip through families.yaml, which is the promise that the tree outlives the
// database. A family naming nobody must write no partners key and come back as
// the same document.
func TestSibling_exportRoundTripsAParentlessFamily(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	actor := makeUser(t, db, "usfamily0000000000000016", "sibling-editor")
	josef := makeSubject(t, ppl, "Josef Nečas")
	anna := makeSubject(t, ppl, "Anna Nečasová")
	group := addSibling(t, fam, actor, josef, anna).Family

	export, err := fam.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(export.Families) != 1 || export.Families[0].UID != group.UID {
		t.Fatalf("exported families = %+v, want the one group", export.Families)
	}
	if export.Families[0].PartnerA != nil || export.Families[0].PartnerB != nil {
		t.Errorf("exported family = %+v, want no partner", export.Families[0])
	}
	if len(export.Subjects) != 2 {
		t.Errorf("exported subjects = %d, want the two children the group names", len(export.Subjects))
	}

	doc := familyexport.Build(familyexport.Input{Export: export})
	if len(doc.Families) != 1 || doc.Families[0].Partners != nil {
		t.Fatalf("document family = %+v, want an omitted partners list", doc.Families)
	}
	if len(doc.Families[0].Children) != 2 {
		t.Errorf("document children = %+v, want both siblings", doc.Families[0].Children)
	}
	data, err := familyexport.Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	back, err := familyexport.Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(back.Families) != 1 || back.Families[0].UID != group.UID || back.Families[0].Partners != nil {
		t.Errorf("round-tripped family = %+v, want the group with nobody in its partners", back.Families)
	}
	if len(back.Families[0].Children) != 2 {
		t.Errorf("round-tripped children = %+v, want both siblings", back.Families[0].Children)
	}
	if !bytes.Contains(data, []byte(group.UID)) || bytes.Contains(data, []byte("partners:")) {
		t.Error("the written file names a partners key for a family that has nobody in it")
	}
}
