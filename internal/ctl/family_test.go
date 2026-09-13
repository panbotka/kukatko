package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// relationsBody is a realistic GET /subjects/{uid}/relations answer: a parent, a
// sibling, a marriage and a child, each shaped as internal/familyapi sends it.
const relationsBody = `{
	"parents":[{"uid":"sub02","slug":"marie-necasova","name":"Marie Nečasová","type":"person",
		"birth_year":1921,"death_year":1998,"photo_count":12,"family_uid":"fam01","child_kind":"birth"}],
	"siblings":[{"uid":"sub03","slug":"josef-necas","name":"Josef Nečas","type":"person",
		"birth_year":1950,"death_year":null,"photo_count":4,"family_uid":"fam01","child_kind":"adopted"}],
	"partners":[{"family":{"uid":"fam02","partner_a_uid":"sub01","partner_b_uid":"sub04",
		"kind":"marriage","from_year":1972,"to_year":null,"note":"oddáni v Křtinách",
		"created_at":"2024-05-01T10:00:00Z","updated_at":"2024-05-02T10:00:00Z"},
		"partner":{"uid":"sub04","slug":"eva-necasova","name":"Eva Nečasová","type":"person",
		"birth_year":1949,"death_year":null,"photo_count":30}}],
	"children":[{"uid":"sub05","slug":"petr-necas","name":"Petr Nečas","type":"person",
		"birth_year":1974,"death_year":null,"photo_count":7,"family_uid":"fam02","child_kind":"birth"}]
}`

// TestClient_GetRelations verifies the relations path and that all four derived
// lists decode, the nullable death year included.
func TestClient_GetRelations(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Write([]byte(relationsBody))
	})

	relations, err := client.FetchRelations(t.Context(), "sub01")
	if err != nil {
		t.Fatalf("FetchRelations returned %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/subjects/sub01/relations" {
		t.Errorf("request = %s %s, want GET on the relations path", gotMethod, gotPath)
	}
	if len(relations.Parents) != 1 || relations.Parents[0].Name != "Marie Nečasová" {
		t.Errorf("parents = %+v, want Marie", relations.Parents)
	}
	if len(relations.Partners) != 1 || relations.Partners[0].Partner == nil ||
		relations.Partners[0].Family.Kind != FamilyMarriage {
		t.Errorf("partners = %+v, want one marriage with the other side named", relations.Partners)
	}
	if relations.Children[0].DeathYear != nil {
		t.Errorf("child = %+v, want a living child's death year left nil", relations.Children[0])
	}
}

// TestRelations_Role verifies every list is searched and that an unrelated uid
// reports nothing rather than guessing.
func TestRelations_Role(t *testing.T) {
	t.Parallel()

	relations, err := DecodeRelations([]byte(relationsBody))
	if err != nil {
		t.Fatalf("DecodeRelations returned %v", err)
	}
	tests := map[string]string{
		"sub02": RoleParent,
		"sub03": RoleSibling,
		"sub04": RolePartner,
		"sub05": RoleChild,
		"sub09": "",
	}
	for uid, want := range tests {
		if got := relations.Role(uid); got != want {
			t.Errorf("Role(%q) = %q, want %q", uid, got, want)
		}
		found := relations.Find(uid)
		if (found == nil) != (want == "") {
			t.Errorf("Find(%q) = %+v, want it to agree with the role %q", uid, found, want)
		}
	}
}

// TestClient_AddRelation verifies an existing subject is sent by uid and the
// child kind rides along.
func TestClient_AddRelation(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"family":{"uid":"fam01","partner_a_uid":"sub02","partner_b_uid":null,
			"kind":"partnership","from_year":null,"to_year":null,"note":""},
			"relative":{"uid":"sub02","name":"Marie Nečasová","type":"person","birth_year":1921,
			"death_year":1998,"photo_count":12},"created":false}`))
	})

	raw, err := client.AddRelation(t.Context(), "sub01", RelationInput{
		Role: RoleParent, SubjectUID: "sub02", ChildKind: ChildAdopted,
	})
	if err != nil {
		t.Fatalf("AddRelation returned %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/subjects/sub01/relations" {
		t.Errorf("request = %s %s, want POST on the relations path", gotMethod, gotPath)
	}
	if gotBody["role"] != RoleParent || gotBody["subject_uid"] != "sub02" ||
		gotBody["child_kind"] != ChildAdopted {
		t.Errorf("body = %v, want the role, the subject and the child kind", gotBody)
	}
	if _, present := gotBody["new_subject"]; present {
		t.Errorf("body = %v, want no new_subject when an existing person was named", gotBody)
	}
	result, err := DecodeRelationResult(raw)
	if err != nil {
		t.Fatalf("DecodeRelationResult returned %v", err)
	}
	if result.Created || result.Relative.Name != "Marie Nečasová" || result.Family.UID != "fam01" {
		t.Errorf("result = %+v, want the existing person in her family", result)
	}
}

// TestRelationInput_validate verifies every local refusal, so an obvious mistake
// costs no round trip.
func TestRelationInput_validate(t *testing.T) {
	t.Parallel()

	person := &NewPerson{Name: "Marie Nečasová"}
	tests := map[string]struct {
		in   RelationInput
		want error
	}{
		"unknown role":     {RelationInput{Role: "sibling", SubjectUID: "sub02"}, ErrInvalidRole},
		"unknown kind":     {RelationInput{Role: RoleChild, SubjectUID: "sub02", ChildKind: "foster"}, ErrInvalidChildKind},
		"neither side":     {RelationInput{Role: RoleParent}, ErrSubjectRequired},
		"both sides":       {RelationInput{Role: RoleParent, SubjectUID: "sub02", New: person}, ErrSubjectAmbiguous},
		"nameless person":  {RelationInput{Role: RoleParent, New: &NewPerson{}}, ErrEmptySubjectName},
		"unknown type":     {RelationInput{Role: RoleParent, New: &NewPerson{Name: "M", Type: "ghost"}}, ErrInvalidSubjectType},
		"existing subject": {RelationInput{Role: RoleParent, SubjectUID: "sub02"}, nil},
		"new person":       {RelationInput{Role: RolePartner, New: person}, nil},
	}
	for name, tc := range tests {
		if err := tc.in.validate(); !errors.Is(err, tc.want) {
			t.Errorf("%s: validate() = %v, want %v", name, err, tc.want)
		}
	}
}

// TestClient_AddRelation_toSelf verifies a person related to themselves is
// refused locally, before a request is spent on it.
func TestClient_AddRelation_toSelf(t *testing.T) {
	t.Parallel()

	var requests int
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusCreated)
	})

	_, err := client.AddRelation(t.Context(), "sub01", RelationInput{Role: RoleParent, SubjectUID: "sub01"})
	if !errors.Is(err, ErrRelationSelf) {
		t.Fatalf("AddRelation to self = %v, want ErrRelationSelf", err)
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}

// TestClient_RemoveRelation verifies the DELETE path carries both uids and that a
// 204 is a success rather than a decoding failure.
func TestClient_RemoveRelation(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.RemoveRelation(t.Context(), "sub01", "sub02"); err != nil {
		t.Fatalf("RemoveRelation returned %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/subjects/sub01/relations/sub02" {
		t.Errorf("request = %s %s, want DELETE naming both people", gotMethod, gotPath)
	}
}

// TestClient_UpdateFamily verifies the whole editable set is sent, the cleared
// year as an explicit null rather than as an omitted field.
func TestClient_UpdateFamily(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"fam02","partner_a_uid":"sub01","partner_b_uid":"sub04",
			"kind":"marriage","from_year":1972,"to_year":null,"note":"oddáni v Křtinách",
			"created_at":"2024-05-01T10:00:00Z","updated_at":"2024-05-03T10:00:00Z"}`))
	})

	year := 1972
	raw, err := client.UpdateFamily(t.Context(), "fam02", FamilyUpdate{
		Kind: FamilyMarriage, FromYear: &year, Note: "oddáni v Křtinách",
	})
	if err != nil {
		t.Fatalf("UpdateFamily returned %v", err)
	}
	if gotMethod != http.MethodPatch || gotPath != "/api/v1/families/fam02" {
		t.Errorf("request = %s %s, want PATCH on the family", gotMethod, gotPath)
	}
	if gotBody["kind"] != FamilyMarriage || gotBody["from_year"] != float64(1972) {
		t.Errorf("body = %v, want the stated kind and year", gotBody)
	}
	if value, present := gotBody["to_year"]; !present || value != nil {
		t.Errorf("body = %v, want the unstated year sent as an explicit null", gotBody)
	}
	family, err := DecodeFamily(raw)
	if err != nil {
		t.Fatalf("DecodeFamily returned %v", err)
	}
	if family.UID != "fam02" || len(family.Partners()) != 2 {
		t.Errorf("family = %+v, want both partners", family)
	}
}

// TestFamilyUpdate_validate verifies the year rules mirror the SQL CHECKs, so a
// mistyped year is refused before it is sent.
func TestFamilyUpdate_validate(t *testing.T) {
	t.Parallel()

	old, recent, future := 198, 1972, 9999
	tests := map[string]struct {
		in   FamilyUpdate
		want error
	}{
		"unknown kind": {FamilyUpdate{Kind: "engagement"}, ErrInvalidFamilyKind},
		"year too old": {FamilyUpdate{FromYear: &old}, ErrInvalidFamilyYear},
		"year ahead":   {FamilyUpdate{FromYear: &future}, ErrInvalidFamilyYear},
		"ends first":   {FamilyUpdate{FromYear: &future, ToYear: &recent}, ErrInvalidFamilyYear},
		"backwards":    {FamilyUpdate{FromYear: &recent, ToYear: &old}, ErrInvalidFamilyYear},
		"empty kind":   {FamilyUpdate{FromYear: &recent}, nil},
		"whole record": {FamilyUpdate{Kind: FamilyUnknown, FromYear: &recent, Note: "x"}, nil},
	}
	for name, tc := range tests {
		if err := tc.in.validate(); !errors.Is(err, tc.want) {
			t.Errorf("%s: validate() = %v, want %v", name, err, tc.want)
		}
	}
}

// familySubjectsBody is a {"subjects": […]} list with two people who share a name, so a
// lookup by name has something ambiguous to refuse.
const familySubjectsBody = `{"subjects":[
	{"uid":"sub02","slug":"marie-necasova","name":"Marie Nečasová","type":"person"},
	{"uid":"sub03","slug":"marie-necasova-2","name":"Marie Nečasová","type":"person"},
	{"uid":"sub04","slug":"eva-necasova","name":"Eva Nečasová","type":"person"}
]}`

// TestClient_FindSubjectByName verifies the three outcomes a name can have:
// exactly one person, nobody at all, or several to choose between.
func TestClient_FindSubjectByName(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(familySubjectsBody))
	})

	found, ok, err := client.FindSubjectByName(t.Context(), "eva nečasová")
	if err != nil || !ok || found.UID != "sub04" {
		t.Errorf("lookup by name = %+v, %v, %v; want Eva, matched case-insensitively", found, ok, err)
	}
	found, ok, err = client.FindSubjectByName(t.Context(), "eva-necasova")
	if err != nil || !ok || found.UID != "sub04" {
		t.Errorf("lookup by slug = %+v, %v, %v; want Eva", found, ok, err)
	}
	if _, ok, err = client.FindSubjectByName(t.Context(), "Bohumila Nečasová"); err != nil || ok {
		t.Errorf("lookup of an unknown name = %v, %v; want nobody and no error", ok, err)
	}
	_, _, err = client.FindSubjectByName(t.Context(), "Marie Nečasová")
	if !errors.Is(err, ErrSubjectAmbiguous) {
		t.Fatalf("ambiguous lookup = %v, want ErrSubjectAmbiguous", err)
	}
	if !strings.Contains(err.Error(), "sub02") || !strings.Contains(err.Error(), "sub03") {
		t.Errorf("ambiguous lookup = %q, want both candidates named", err)
	}
}

// TestWriteRelations verifies the four lists render as one table, in family-strip
// order, with the lone open year left open and the counts underneath.
func TestWriteRelations(t *testing.T) {
	t.Parallel()

	relations, err := DecodeRelations([]byte(relationsBody))
	if err != nil {
		t.Fatalf("DecodeRelations returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteRelations(&buf, relations); err != nil {
		t.Fatalf("WriteRelations returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"ROLE", "WHO", "LIFE", "KIND", "FAMILY", "PHOTOS",
		"parent", "Marie Nečasová (sub02)", "1921–1998", "birth", "fam01",
		"sibling", "adopted", "partner", "Eva Nečasová (sub04)", "marriage", "fam02 1972–",
		"child", "Petr Nečas (sub05)", "1974–",
		"1 parent · 1 sibling · 1 partner · 1 child",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("relations table does not contain %q:\n%s", want, out)
		}
	}
	previous := -1
	for _, who := range []string{"Marie", "Josef", "Eva", "Petr"} {
		at := strings.Index(out, who)
		if at < previous {
			t.Errorf("%s is out of family-strip order (parents, siblings, partners, children):\n%s", who, out)
		}
		previous = at
	}
}

// TestWriteRelations_empty verifies a person nobody has placed yet prints a line
// rather than an empty table.
func TestWriteRelations_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteRelations(&buf, Relations{}); err != nil {
		t.Fatalf("WriteRelations returned %v", err)
	}
	if !strings.Contains(buf.String(), "nobody is related") {
		t.Errorf("empty relations = %q, want one line of prose", buf.String())
	}
}

// TestWriteRelations_lonePartner verifies a family whose second partner nobody
// remembers renders as such — it is a fact about the family, not a gap.
func TestWriteRelations_lonePartner(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	relations := Relations{Partners: []Partnership{{
		Family: Family{UID: "fam03", PartnerA: new("sub01"), Kind: FamilyUnknown},
	}}}
	if err := WriteRelations(&buf, relations); err != nil {
		t.Fatalf("WriteRelations returned %v", err)
	}
	if !strings.Contains(buf.String(), "no partner recorded") ||
		!strings.Contains(buf.String(), "fam03") {
		t.Errorf("lone-parent family = %q, want the family named and its empty side stated", buf.String())
	}
	if !strings.Contains(buf.String(), RoleLoneParent) {
		t.Errorf("lone-parent family = %q, want the row labelled %q", buf.String(), RoleLoneParent)
	}
	if !strings.Contains(buf.String(), "0 partners") {
		t.Errorf("lone-parent family = %q, want it left out of the partner count", buf.String())
	}
}

// TestWriteRelations_loneParentIsNotCounted verifies that a person with one
// lone-parent family and one real partnership is summarized as having exactly
// one partner: the family with nobody on the other side is a row, not a person.
func TestWriteRelations_loneParentIsNotCounted(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	relations := Relations{
		Partners: []Partnership{
			{Family: Family{UID: "fam03", PartnerA: new("sub01"), Kind: FamilyPartnership}},
			{
				Family: Family{
					UID: "fam04", PartnerA: new("sub01"), PartnerB: new("sub04"), Kind: FamilyPartnership,
				},
				Partner: &Relative{UID: "sub04", Name: "Eva Nečasová", PhotoCount: 5},
			},
		},
		Children: []Relative{{UID: "sub05", Name: "Petr Nečas", FamilyUID: "fam03", ChildKind: ChildBirth}},
	}
	if err := WriteRelations(&buf, relations); err != nil {
		t.Fatalf("WriteRelations returned %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "0 parents · 0 siblings · 1 partner · 1 child") {
		t.Errorf("summary of one lone-parent family and one partnership = %q, want one partner", out)
	}
	partnerRows := 0
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, RolePartner+" ") {
			partnerRows++
		}
	}
	if partnerRows != 1 {
		t.Errorf("relations table = %q, want exactly one row in the partner role, got %d", out, partnerRows)
	}
	for _, want := range []string{RoleLoneParent, "fam03", "Eva Nečasová (sub04)", "fam04"} {
		if !strings.Contains(out, want) {
			t.Errorf("relations table does not contain %q:\n%s", want, out)
		}
	}
}

// TestWriteRelationReport verifies a recorded relation names both people and says
// whether it created one, in the table and in the agent format alike.
func TestWriteRelationReport(t *testing.T) {
	t.Parallel()

	birth := 1921
	report := RelationReport{
		RelationResult: RelationResult{
			Family:   Family{UID: "fam01", Kind: FamilyPartnership},
			Relative: Relative{UID: "sub02", Name: "Marie Nečasová", BirthYear: &birth},
			Created:  true,
		},
		Role: RoleParent, SubjectUID: "sub01", SubjectName: "Anna Nečasová",
	}

	var table bytes.Buffer
	if err := WriteRelationReport(&table, Output{Format: FormatTable}, report); err != nil {
		t.Fatalf("WriteRelationReport returned %v", err)
	}
	for _, want := range []string{
		"Marie Nečasová (sub02)", "parent of Anna Nečasová (sub01)", "1921–", "yes", "fam01 · partnership",
	} {
		if !strings.Contains(table.String(), want) {
			t.Errorf("relation report does not contain %q:\n%s", want, table.String())
		}
	}

	var machine bytes.Buffer
	if err := WriteRelationReport(&machine, Output{Format: FormatJSON}, report); err != nil {
		t.Fatalf("WriteRelationReport returned %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(machine.Bytes(), &decoded); err != nil {
		t.Fatalf("json report is not valid JSON: %v (%q)", err, machine.String())
	}
	if decoded["role"] != RoleParent || decoded["subject_name"] != "Anna Nečasová" || decoded["created"] != true {
		t.Errorf("json report = %v, want the synthesized role and subject alongside the server's answer", decoded)
	}
}

// TestWriteFamily verifies a family prints both partners by name where the caller
// could resolve them, and by uid where it could not.
func TestWriteFamily(t *testing.T) {
	t.Parallel()

	from := 1972
	family := Family{
		UID: "fam02", PartnerA: new("sub01"), PartnerB: new("sub04"),
		Kind: FamilyMarriage, FromYear: &from, Note: "oddáni v Křtinách",
	}
	var buf bytes.Buffer
	if err := WriteFamily(&buf, family, map[string]string{"sub01": "Anna Nečasová"}); err != nil {
		t.Fatalf("WriteFamily returned %v", err)
	}
	for _, want := range []string{"fam02", "Anna Nečasová (sub01) & sub04", "marriage", "1972–", "Křtinách"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("family does not contain %q:\n%s", want, buf.String())
		}
	}
}

// TestWriteFamily_loneParent verifies a family with one recorded partner says so,
// since that is what it means rather than a missing half of the answer.
func TestWriteFamily_loneParent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	family := Family{UID: "fam03", PartnerA: new("sub01"), Kind: FamilyUnknown}
	if err := WriteFamily(&buf, family, nil); err != nil {
		t.Fatalf("WriteFamily returned %v", err)
	}
	if !strings.Contains(buf.String(), "sub01 (lone parent)") {
		t.Errorf("lone-parent family = %q, want it named as one", buf.String())
	}
}

// TestFormatYears verifies an open end stays open: an unrecorded death year is
// not the same as a person who was never born.
func TestFormatYears(t *testing.T) {
	t.Parallel()

	from, to := 1921, 1998
	tests := []struct {
		from, to *int
		want     string
	}{
		{nil, nil, "-"},
		{&from, nil, "1921–"},
		{nil, &to, "–1998"},
		{&from, &to, "1921–1998"},
	}
	for _, tc := range tests {
		if got := formatYears(tc.from, tc.to); got != tc.want {
			t.Errorf("formatYears(%v, %v) = %q, want %q", tc.from, tc.to, got, tc.want)
		}
	}
}
