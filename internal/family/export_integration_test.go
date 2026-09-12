//go:build integration

package family_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/family"
	"github.com/panbotka/kukatko/internal/people"
)

// TestExport_readsTheWholeGenealogy verifies the export read returns every
// family, every child membership and every subject either of them names, so the
// file written from it stands on its own.
func TestExport_readsTheWholeGenealogy(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	export, err := fam.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Five children were recorded, each in the family of their two parents. Josef
	// and Anna are siblings and share one, so that is four couples, five
	// memberships and nine people.
	if len(export.Families) != 4 {
		t.Errorf("families = %d, want 4", len(export.Families))
	}
	if len(export.Children) != 5 {
		t.Errorf("child memberships = %d, want 5", len(export.Children))
	}
	if len(export.Subjects) != 9 {
		t.Errorf("subjects = %d, want the 9 seeded people", len(export.Subjects))
	}
	if export.Empty() {
		t.Error("Empty() = true for a seeded genealogy")
	}

	known := make(map[string]family.ExportSubject, len(export.Subjects))
	for _, subj := range export.Subjects {
		known[subj.UID] = subj
	}
	for _, f := range export.Families {
		for _, uid := range []*string{f.PartnerA, f.PartnerB} {
			if uid == nil {
				continue
			}
			if _, ok := known[*uid]; !ok {
				t.Errorf("family %s names partner %s, who is in no exported subject", f.UID, *uid)
			}
		}
	}
	for _, membership := range export.Children {
		if _, ok := known[membership.ChildUID]; !ok {
			t.Errorf("membership names child %s, who is in no exported subject", membership.ChildUID)
		}
		if !slices.ContainsFunc(export.Families, func(f family.Family) bool {
			return f.UID == membership.FamilyUID
		}) {
			t.Errorf("membership names family %s, which was not exported", membership.FamilyUID)
		}
	}
	if got := known[v.jan].Name; got != "Jan Nečas" {
		t.Errorf("Jan's exported name = %q, want %q", got, "Jan Nečas")
	}
}

// TestExport_isOrderedAndRepeatable verifies two reads of an unchanged genealogy
// produce the same order. The file is generated on every relation change, so a
// listing that reshuffled itself would make every export a whole-file diff.
func TestExport_isOrderedAndRepeatable(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	seedVillage(t, fam, ppl, db)

	first, err := fam.Export(ctx)
	if err != nil {
		t.Fatalf("first Export: %v", err)
	}
	second, err := fam.Export(ctx)
	if err != nil {
		t.Fatalf("second Export: %v", err)
	}
	if !slices.IsSortedFunc(first.Families, func(a, b family.Family) int {
		return strings.Compare(a.UID, b.UID)
	}) {
		t.Error("families are not ordered by uid")
	}
	if !slices.IsSortedFunc(first.Subjects, func(a, b family.ExportSubject) int {
		return strings.Compare(a.UID, b.UID)
	}) {
		t.Error("subjects are not ordered by uid")
	}
	if !slices.Equal(familyUIDs(first), familyUIDs(second)) {
		t.Errorf("two reads disagree: %v vs %v", familyUIDs(first), familyUIDs(second))
	}
}

// TestExport_leavesOutSubjectsInNoFamily verifies the export is the genealogy and
// not the people index: somebody nobody recorded a relation for has no place in a
// tree, and what is known about them travels in the sidecars of their photos.
func TestExport_leavesOutSubjectsInNoFamily(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	seedVillage(t, fam, ppl, db)
	stranger := makeSubject(t, ppl, "Nikdo Cizí")

	export, err := fam.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, subj := range export.Subjects {
		if subj.UID == stranger {
			t.Errorf("the export carries %s, who is in no family", subj.Name)
		}
	}
	if len(export.Subjects) != 9 {
		t.Errorf("subjects = %d, want the 9 in a family", len(export.Subjects))
	}
}

// TestExport_carriesWhatARebuildNeeds verifies the fields a tree cannot be
// redrawn without — the name, the nickname and the life years — travel with each
// person. They exist in no photo sidecar, which is why this file exists.
func TestExport_carriesWhatARebuildNeeds(t *testing.T) {
	fam, ppl, _, db := stores(t)
	ctx := context.Background()
	v := seedVillage(t, fam, ppl, db)

	if _, err := ppl.UpdateSubject(ctx, v.bohumil, people.SubjectUpdate{
		Name:      "Bohumil Nečas st.",
		Nickname:  "Bohouš",
		Type:      people.SubjectPerson,
		BirthYear: new(1901),
		DeathYear: new(1978),
	}); err != nil {
		t.Fatalf("updating the subject: %v", err)
	}

	export, err := fam.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	var got family.ExportSubject
	for _, subj := range export.Subjects {
		if subj.UID == v.bohumil {
			got = subj
		}
	}
	if got.Name != "Bohumil Nečas st." || got.Nickname != "Bohouš" {
		t.Errorf("exported subject = %+v, want the edited name and nickname", got)
	}
	if got.BirthYear == nil || *got.BirthYear != 1901 || got.DeathYear == nil || *got.DeathYear != 1978 {
		t.Errorf("exported life years = %v–%v, want 1901–1978", got.BirthYear, got.DeathYear)
	}
	if got.Slug == "" || got.Type != string(people.SubjectPerson) {
		t.Errorf("exported subject = %+v, want a slug and a type", got)
	}
}

// TestExport_emptyLibrary verifies a library nobody has recorded a relation in
// exports nothing at all rather than failing — that is a legitimate state, and
// the file written from it says "there is no tree".
func TestExport_emptyLibrary(t *testing.T) {
	fam, _, _, _ := stores(t)

	export, err := fam.Export(context.Background())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !export.Empty() {
		t.Errorf("export = %+v, want empty", export)
	}
}

// familyUIDs lists an export's family uids in order.
func familyUIDs(export family.Export) []string {
	out := make([]string, 0, len(export.Families))
	for _, f := range export.Families {
		out = append(out, f.UID)
	}
	return out
}
