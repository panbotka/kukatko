package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// ctlRelationsBody is a GET /subjects/{uid}/relations answer carrying one of
// each: a parent, a sibling, a marriage and a child.
const ctlRelationsBody = `{
	"parents":[{"uid":"sub02","slug":"marie-necasova","name":"Marie Nečasová","type":"person",
		"birth_year":1921,"death_year":1998,"photo_count":12,"family_uid":"fam01","child_kind":"birth"}],
	"siblings":[{"uid":"sub03","slug":"josef-necas","name":"Josef Nečas","type":"person",
		"birth_year":1950,"death_year":null,"photo_count":4,"family_uid":"fam01","child_kind":"birth"}],
	"partners":[{"family":{"uid":"fam02","partner_a_uid":"sub01","partner_b_uid":"sub04",
		"kind":"marriage","from_year":1972,"to_year":null,"note":""},
		"partner":{"uid":"sub04","slug":"eva-necasova","name":"Eva Nečasová","type":"person",
		"birth_year":1949,"death_year":null,"photo_count":30}}],
	"children":[{"uid":"sub05","slug":"petr-necas","name":"Petr Nečas","type":"person",
		"birth_year":1974,"death_year":null,"photo_count":7,"family_uid":"fam02","child_kind":"birth"}]
}`

// ctlSubjectListBody is the {"subjects": […]} list a lookup by name reads.
const ctlSubjectListBody = `{"subjects":[
	{"uid":"sub01","slug":"anna-necasova","name":"Anna Nečasová","type":"person"},
	{"uid":"sub02","slug":"marie-necasova","name":"Marie Nečasová","type":"person"}
]}`

// ctlRelationAddBody is a POST /subjects/{uid}/relations answer for an existing
// person: the family the relation landed in and who it was recorded with.
const ctlRelationAddBody = `{"family":{"uid":"fam01","partner_a_uid":"sub02","partner_b_uid":null,
	"kind":"partnership","from_year":null,"to_year":null,"note":""},
	"relative":{"uid":"sub02","slug":"marie-necasova","name":"Marie Nečasová","type":"person",
	"birth_year":1921,"death_year":1998,"photo_count":12},"created":false}`

// ctlAnnaBody is the subject every family command is recorded on.
const ctlAnnaBody = `{"uid":"sub01","slug":"anna-necasova","name":"Anna Nečasová","type":"person"}`

// TestCtlFamilyKeepsThePersistentOutputFlag verifies no family subcommand defines
// an --output flag of its own. A local one shadows the persistent -o and breaks
// it, which has already happened once in this repo.
func TestCtlFamilyKeepsThePersistentOutputFlag(t *testing.T) {
	t.Parallel()

	family := newCtlFamilyCmd(&ctlOptions{})
	for _, sub := range family.Commands() {
		if flag := sub.Flags().Lookup("output"); flag != nil && sub.LocalNonPersistentFlags().Lookup("output") != nil {
			t.Errorf("%q defines its own --output flag, which shadows the persistent one", sub.Name())
		}
	}
}

// TestCtlFamilyRelations verifies the listing reads the relations endpoint and
// renders all four lists as one table.
func TestCtlFamilyRelations(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(ctlRelationsBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "family", "relations", "sub01")
	if err != nil {
		t.Fatalf("family relations returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub01/relations" {
		t.Errorf("path = %q, want the relations endpoint", gotPath)
	}
	for _, want := range []string{
		"ROLE", "parent", "Marie Nečasová (sub02)", "1921–1998", "fam01",
		"sibling", "partner", "Eva Nečasová (sub04)", "marriage", "child", "Petr Nečas (sub05)",
		"1 parent · 1 sibling · 1 partner · 1 child",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("relations table does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlFamilyRelations_llm verifies the persistent -o reaches this group too,
// so an agent gets the server's own shape rather than a table.
func TestCtlFamilyRelations_llm(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(ctlRelationsBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "llm", "family", "relations", "sub01")
	if err != nil {
		t.Fatalf("family relations -o llm returned %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("llm output is not valid JSON: %v (%q)", err, out)
	}
	if _, present := decoded["parents"]; !present {
		t.Errorf("llm output = %v, want the four derived lists", decoded)
	}
}

// TestCtlFamilyAdd_byUID verifies a relation to an existing person is posted by
// uid and reported with both people named.
func TestCtlFamilyAdd_byUID(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		if r.Method == http.MethodGet {
			w.Write([]byte(ctlAnnaBody))
			return
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(ctlRelationAddBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "add", "sub01", "parent", "sub02", "--child-kind", "adopted")
	if err != nil {
		t.Fatalf("family add returned %v", err)
	}
	if len(seen) != 2 || seen[0].path != "/api/v1/subjects/sub01" ||
		seen[1].path != "/api/v1/subjects/sub01/relations" {
		t.Fatalf("requests = %+v, want the subject named, then the relation recorded", seen)
	}
	if seen[1].body["role"] != "parent" || seen[1].body["subject_uid"] != "sub02" ||
		seen[1].body["child_kind"] != "adopted" {
		t.Errorf("body = %v, want the role, the person and the child kind", seen[1].body)
	}
	for _, want := range []string{
		"Marie Nečasová (sub02)", "parent of Anna Nečasová (sub01)", "fam01", "no —",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("relation report does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlFamilyAdd_byExistingName verifies a name that matches somebody relates
// to that person rather than creating a second copy of them.
func TestCtlFamilyAdd_byExistingName(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		switch {
		case r.URL.Path == "/api/v1/subjects":
			w.Write([]byte(ctlSubjectListBody))
		case r.Method == http.MethodGet:
			w.Write([]byte(ctlAnnaBody))
		default:
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(ctlRelationAddBody))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "add", "sub01", "parent", "--name", "marie nečasová")
	if err != nil {
		t.Fatalf("family add --name returned %v", err)
	}
	post := seen[len(seen)-1]
	if post.body["subject_uid"] != "sub02" {
		t.Errorf("body = %v, want the name resolved to the person who already exists", post.body)
	}
	if _, present := post.body["new_subject"]; present {
		t.Errorf("body = %v, want no person created for a name the library knows", post.body)
	}
	if !strings.Contains(out, "Marie Nečasová (sub02)") {
		t.Errorf("relation report = %q, want the resolved person named", out)
	}
}

// TestCtlFamilyAdd_byNewName verifies a name that matches nobody is created
// inline, with the details the command line carried, and reported as created.
func TestCtlFamilyAdd_byNewName(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		switch {
		case r.URL.Path == "/api/v1/subjects":
			w.Write([]byte(ctlSubjectListBody))
		case r.Method == http.MethodGet:
			w.Write([]byte(ctlAnnaBody))
		default:
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"family":{"uid":"fam03","partner_a_uid":"sub09","partner_b_uid":null,
				"kind":"partnership","from_year":null,"to_year":null,"note":""},
				"relative":{"uid":"sub09","slug":"bohumila-necasova","name":"Bohumila Nečasová",
				"type":"person","birth_year":1899,"death_year":null,"photo_count":0},"created":true}`))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "add", "sub01", "parent", "--name", "Bohumila Nečasová", "--birth-year", "1899")
	if err != nil {
		t.Fatalf("family add --name returned %v", err)
	}
	post := seen[len(seen)-1]
	created, _ := post.body["new_subject"].(map[string]any)
	if created == nil || created["name"] != "Bohumila Nečasová" || created["birth_year"] != float64(1899) {
		t.Fatalf("body = %v, want the unknown person created inline with her birth year", post.body)
	}
	if _, present := post.body["subject_uid"]; present {
		t.Errorf("body = %v, want no subject_uid beside a person being created", post.body)
	}
	for _, want := range []string{"Bohumila Nečasová (sub09)", "yes —", "1899–"} {
		if !strings.Contains(out, want) {
			t.Errorf("relation report does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlFamilyAdd_detailsNeedAName verifies the details of a person to create
// are refused beside the uid of one who already exists, where they could only be
// ignored. Nothing is requested on the way to refusing.
func TestCtlFamilyAdd_detailsNeedAName(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(ctlAnnaBody))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "add", "sub01", "parent", "sub02", "--birth-year", "1921")
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("add with details and a uid = %v, want a refusal naming --name", err)
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}

// TestCtlFamilyAdd_unknownRole verifies a role the API does not know is refused
// locally — there is no sibling relation to record.
func TestCtlFamilyAdd_unknownRole(t *testing.T) {
	var writes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Write([]byte(ctlAnnaBody))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "add", "sub01", "sibling", "sub03")
	if err == nil || !strings.Contains(err.Error(), "parent") {
		t.Fatalf("add with an unknown role = %v, want the roles named", err)
	}
	if writes != 0 {
		t.Errorf("%d writes were made, want none", writes)
	}
}

// familyRemoveServer answers the two reads every removal makes, counting the
// writes that reach it.
func familyRemoveServer(t *testing.T, writes *int) string {
	t.Helper()

	return ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete:
			*writes++
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/relations"):
			w.Write([]byte(ctlRelationsBody))
		default:
			w.Write([]byte(ctlAnnaBody))
		}
	})
}

// TestCtlFamilyRemove verifies a confirmed removal reads how the two are related
// first and says so, rather than reporting a pair of uids.
func TestCtlFamilyRemove(t *testing.T) {
	var writes int
	configPath := familyRemoveServer(t, &writes)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "remove", "sub01", "sub02", "--yes")
	if err != nil {
		t.Fatalf("family remove returned %v", err)
	}
	if writes != 1 {
		t.Fatalf("%d relations were removed, want exactly one", writes)
	}
	if !strings.Contains(out, "Marie Nečasová (sub02) as the parent of Anna Nečasová (sub01)") {
		t.Errorf("confirmation = %q, want both people and the role named", out)
	}
}

// TestCtlFamilyRemove_needsConfirmation verifies an unconfirmed removal refuses,
// naming --yes, and removes nothing.
func TestCtlFamilyRemove_needsConfirmation(t *testing.T) {
	var writes int
	configPath := familyRemoveServer(t, &writes)

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "family", "remove", "sub01", "sub02")
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unconfirmed removal = %v, want it to ask for --yes", err)
	}
	if writes != 0 {
		t.Errorf("%d relations were removed, want none", writes)
	}
}

// TestCtlFamilyRemove_dryRun verifies --dry-run reports what would go, in the
// agent format too, and removes nothing.
func TestCtlFamilyRemove_dryRun(t *testing.T) {
	var writes int
	configPath := familyRemoveServer(t, &writes)

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "llm",
		"family", "remove", "sub01", "sub04", "--dry-run")
	if err != nil {
		t.Fatalf("family remove --dry-run returned %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal([]byte(out), &ack); err != nil {
		t.Fatalf("llm dry run is not valid JSON: %v (%q)", err, out)
	}
	message, _ := ack["message"].(string)
	if !strings.Contains(message, "dry run") ||
		!strings.Contains(message, "Eva Nečasová (sub04) as the partner of Anna Nečasová (sub01)") {
		t.Errorf("llm dry run = %v, want it to name what would go", ack)
	}
	if writes != 0 {
		t.Errorf("%d relations were removed by a dry run, want none", writes)
	}
}

// TestCtlFamilyRemove_siblings verifies a pair of siblings is refused with the
// reason: there is no sibling row, only a shared parent.
func TestCtlFamilyRemove_siblings(t *testing.T) {
	var writes int
	configPath := familyRemoveServer(t, &writes)

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "remove", "sub01", "sub03", "--yes")
	if err == nil || !strings.Contains(err.Error(), "siblings are derived") {
		t.Fatalf("removing a sibling = %v, want the derivation explained", err)
	}
	if writes != 0 {
		t.Errorf("%d relations were removed, want none", writes)
	}
}

// TestCtlFamilyRemove_unrelated verifies two people who share no relation fail
// before a request is spent on a 404.
func TestCtlFamilyRemove_unrelated(t *testing.T) {
	var writes int
	configPath := familyRemoveServer(t, &writes)

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"family", "remove", "sub01", "sub77", "--yes")
	if err == nil || !strings.Contains(err.Error(), "not related") {
		t.Fatalf("removing a relation that does not exist = %v, want it said so", err)
	}
	if writes != 0 {
		t.Errorf("%d relations were removed, want none", writes)
	}
}

// TestCtlFamilyEdit verifies the whole editable set is written and that the
// refreshed family prints with its partners named.
func TestCtlFamilyEdit(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		switch r.URL.Path {
		case "/api/v1/subjects/sub01":
			w.Write([]byte(ctlAnnaBody))
		case "/api/v1/subjects/sub04":
			w.Write([]byte(`{"uid":"sub04","slug":"eva-necasova","name":"Eva Nečasová","type":"person"}`))
		default:
			w.Write([]byte(`{"uid":"fam02","partner_a_uid":"sub01","partner_b_uid":"sub04",
				"kind":"marriage","from_year":1972,"to_year":null,"note":"oddáni v Křtinách",
				"created_at":"2024-05-01T10:00:00Z","updated_at":"2024-05-03T10:00:00Z"}`))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "family", "edit", "fam02",
		"--kind", "marriage", "--from-year", "1972", "--note", "oddáni v Křtinách")
	if err != nil {
		t.Fatalf("family edit returned %v", err)
	}
	if seen[0].method != http.MethodPatch || seen[0].path != "/api/v1/families/fam02" {
		t.Fatalf("requests = %+v, want the family patched first", seen)
	}
	if seen[0].body["kind"] != "marriage" || seen[0].body["from_year"] != float64(1972) {
		t.Errorf("body = %v, want the stated kind and year", seen[0].body)
	}
	if value, present := seen[0].body["to_year"]; !present || value != nil {
		t.Errorf("body = %v, want the unstated year written as an explicit null", seen[0].body)
	}
	for _, want := range []string{"fam02", "Anna Nečasová (sub01) & Eva Nečasová (sub04)", "1972–", "Křtinách"} {
		if !strings.Contains(out, want) {
			t.Errorf("family does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlFamilyEdit_needsAField verifies an edit naming nothing is refused rather
// than run: PATCH rewrites the whole record, so it would erase it.
func TestCtlFamilyEdit_needsAField(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(`{"uid":"fam02","kind":"partnership"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "family", "edit", "fam02")
	if err == nil || !strings.Contains(err.Error(), "--kind") {
		t.Fatalf("edit naming nothing = %v, want a refusal naming the flags", err)
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}

// TestCtlFamilyEdit_impossibleYears verifies a union that ends before it begins
// is refused locally, mirroring the SQL CHECK behind it.
func TestCtlFamilyEdit_impossibleYears(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(`{"uid":"fam02","kind":"marriage"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "family", "edit", "fam02",
		"--from-year", "1972", "--to-year", "1948")
	if err == nil || !strings.Contains(err.Error(), "1800") {
		t.Fatalf("edit with impossible years = %v, want the rule stated", err)
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}
