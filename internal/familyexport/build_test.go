package familyexport

import (
	"reflect"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/family"
)

// sampleExport returns a small genealogy in the shape internal/family reads it:
// a couple with two children, a lone parent, and the four people involved — with
// everything deliberately out of order, so the tests below can assert that Build
// puts it in order.
func sampleExport() family.Export {
	return family.Export{
		Families: []family.Family{
			{
				UID:      "fam2",
				PartnerA: new("sbj4"),
				Kind:     family.KindUnknown,
			},
			{
				UID:      "fam1",
				PartnerA: new("sbj1"),
				PartnerB: new("sbj2"),
				Kind:     family.KindMarriage,
				FromYear: new(1925),
				Note:     "svatba ve Vavřinci",
			},
		},
		Children: []family.ChildMembership{
			{FamilyUID: "fam1", ChildUID: "sbj4", Kind: family.ChildBirth},
			{FamilyUID: "fam1", ChildUID: "sbj3", Kind: family.ChildAdopted},
		},
		Subjects: []family.ExportSubject{
			{UID: "sbj3", Slug: "eva", Name: "Eva", Type: "person"},
			{UID: "sbj1", Slug: "bohumil", Name: "Bohumil", Type: "person", BirthYear: new(1901)},
			{UID: "sbj2", Slug: "marie", Name: "Marie", Type: "person"},
			{UID: "sbj4", Slug: "jan", Name: "Jan", Nickname: "Honza", Type: "person"},
		},
	}
}

// TestBuild_ordersEverythingDeterministically pins the property a generated file
// needs above all others: an unchanged genealogy produces identical bytes.
// Families and subjects come out by uid and a family's children by their own, so
// two exports differ only where the data does.
func TestBuild_ordersEverythingDeterministically(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Export: sampleExport(), Now: time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)})

	gotSubjects := make([]string, 0, len(doc.Subjects))
	for _, subj := range doc.Subjects {
		gotSubjects = append(gotSubjects, subj.UID)
	}
	if want := []string{"sbj1", "sbj2", "sbj3", "sbj4"}; !reflect.DeepEqual(gotSubjects, want) {
		t.Errorf("subject order = %v, want %v", gotSubjects, want)
	}
	gotFamilies := make([]string, 0, len(doc.Families))
	for _, fam := range doc.Families {
		gotFamilies = append(gotFamilies, fam.UID)
	}
	if want := []string{"fam1", "fam2"}; !reflect.DeepEqual(gotFamilies, want) {
		t.Errorf("family order = %v, want %v", gotFamilies, want)
	}
	gotChildren := make([]string, 0, len(doc.Families[0].Children))
	for _, child := range doc.Families[0].Children {
		gotChildren = append(gotChildren, child.SubjectUID)
	}
	if want := []string{"sbj3", "sbj4"}; !reflect.DeepEqual(gotChildren, want) {
		t.Errorf("child order = %v, want %v", gotChildren, want)
	}
}

// TestBuild_attachesChildrenToTheirFamily verifies the two tables are folded into
// one: a membership lands in the family it names, carrying its kind, and nowhere
// else.
func TestBuild_attachesChildrenToTheirFamily(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Export: sampleExport()})

	byUID := make(map[string]Family, len(doc.Families))
	for _, fam := range doc.Families {
		byUID[fam.UID] = fam
	}
	if got := len(byUID["fam1"].Children); got != 2 {
		t.Errorf("fam1 has %d child(ren), want 2", got)
	}
	if got := byUID["fam1"].Children[0].Kind; got != string(family.ChildAdopted) {
		t.Errorf("first child's kind = %q, want %q", got, family.ChildAdopted)
	}
	if got := len(byUID["fam2"].Children); got != 0 {
		t.Errorf("fam2 has %d child(ren), want none", got)
	}
}

// TestBuild_partners covers both shapes of a union: a couple keeps both uids, and
// a lone parent yields one rather than one plus an empty string — the file must
// not describe a partner nobody recorded.
func TestBuild_partners(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Export: sampleExport()})

	if want := []string{"sbj1", "sbj2"}; !reflect.DeepEqual(doc.Families[0].Partners, want) {
		t.Errorf("couple's partners = %v, want %v", doc.Families[0].Partners, want)
	}
	if want := []string{"sbj4"}; !reflect.DeepEqual(doc.Families[1].Partners, want) {
		t.Errorf("lone parent's partners = %v, want %v", doc.Families[1].Partners, want)
	}
}

// TestBuild_dropsAnOrphanedMembership verifies a child row naming a family that
// is not in the export is left out rather than written as a child of nobody. The
// database's foreign keys make it impossible, which is exactly why the fallback
// must be the harmless one.
func TestBuild_dropsAnOrphanedMembership(t *testing.T) {
	t.Parallel()

	export := sampleExport()
	export.Children = append(export.Children,
		family.ChildMembership{FamilyUID: "fam-gone", ChildUID: "sbj3", Kind: family.ChildBirth})

	doc := Build(Input{Export: export})

	for _, fam := range doc.Families {
		if fam.UID == "fam-gone" {
			t.Fatalf("the export invented family %s to hang an orphaned membership on", fam.UID)
		}
	}
	if got := len(doc.Families); got != 2 {
		t.Errorf("families = %d, want the 2 that exist", got)
	}
}

// TestBuild_emptyGenealogy verifies a library with no families yields a valid,
// empty document rather than nil-everything nonsense: the version and the
// timestamp are still there, which is what makes the file a statement that there
// is no tree.
func TestBuild_emptyGenealogy(t *testing.T) {
	t.Parallel()

	doc := Build(Input{Now: time.Date(2026, 9, 13, 8, 0, 0, 0, time.UTC)})

	if doc.Version != Version {
		t.Errorf("version = %d, want %d", doc.Version, Version)
	}
	if doc.GeneratedAt.IsZero() {
		t.Error("generated_at is zero, want the pinned time")
	}
	if len(doc.Subjects) != 0 || len(doc.Families) != 0 {
		t.Errorf("document = %+v, want no subjects and no families", doc)
	}
}

// TestBuild_stampsTheClock verifies Now is honoured in UTC and that a zero Now
// falls back to the wall clock rather than to the zero time, which would make
// every file claim to have been written in year one.
func TestBuild_stampsTheClock(t *testing.T) {
	t.Parallel()

	pinned := time.Date(2026, 9, 13, 8, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	if got := Build(Input{Now: pinned}).GeneratedAt; !got.Equal(pinned) || got.Location() != time.UTC {
		t.Errorf("generated_at = %v, want %v in UTC", got, pinned)
	}
	if got := Build(Input{}).GeneratedAt; got.IsZero() {
		t.Error("generated_at is zero for a zero Now, want the wall clock")
	}
}
