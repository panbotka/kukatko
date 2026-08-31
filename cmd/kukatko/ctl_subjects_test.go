package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// ctlSubjectBody is a fully populated GET /subjects/{uid} body: the record a
// rename must carry back untouched.
const ctlSubjectBody = `{"uid":"sub01","slug":"micka","name":"Micka","type":"pet","favorite":true,` +
	`"private":true,"notes":"the tabby","cover_photo_uid":"pht07","birth_year":2012}`

// TestCtlSubjectsCreate verifies a create posts only the fields that were written
// and renders the server's new record.
func TestCtlSubjectsCreate(t *testing.T) {
	var seen recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = recordRequest(r)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"sub09","slug":"rex","name":"Rex","type":"pet"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"subjects", "create", "Rex", "--type", "pet", "--birth-year", "2018")
	if err != nil {
		t.Fatalf("subjects create returned %v", err)
	}
	if seen.method != http.MethodPost || seen.path != "/api/v1/subjects" {
		t.Errorf("request = %s %s, want POST /api/v1/subjects", seen.method, seen.path)
	}
	if seen.body["name"] != "Rex" || seen.body["type"] != "pet" || seen.body["birth_year"] != float64(2018) {
		t.Errorf("body = %v, want the written fields", seen.body)
	}
	if _, present := seen.body["death_year"]; present {
		t.Errorf("body = %v, want no death_year for a year nobody wrote", seen.body)
	}
	for _, want := range []string{"UID", "sub09", "NAME", "Rex", "TYPE", "pet"} {
		if !strings.Contains(out, want) {
			t.Errorf("created subject does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlSubjectsRename verifies a rename reads the record first and sends every
// field back, so renaming a pet does not turn it into a person.
func TestCtlSubjectsRename(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		if r.Method == http.MethodGet {
			w.Write([]byte(ctlSubjectBody))
			return
		}
		w.Write([]byte(`{"uid":"sub01","slug":"micinka","name":"Mičinka","type":"pet"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"subjects", "rename", "sub01", "Mičinka")
	if err != nil {
		t.Fatalf("subjects rename returned %v", err)
	}
	if len(seen) != 2 || seen[0].method != http.MethodGet || seen[1].method != http.MethodPatch {
		t.Fatalf("requests = %+v, want a read then a patch", seen)
	}
	if seen[1].body["name"] != "Mičinka" || seen[1].body["type"] != "pet" ||
		seen[1].body["notes"] != "the tabby" || seen[1].body["cover_photo_uid"] != "pht07" {
		t.Errorf("body = %v, want the whole record with only the name changed", seen[1].body)
	}
	if !strings.Contains(out, "Mičinka") {
		t.Errorf("renamed subject = %q, want the new name", out)
	}
}

// TestCtlSubjectsMerge verifies a confirmed merge posts the keeper to the source's
// path and reports both people by name — the source's name exists nowhere else
// once the merge has run.
func TestCtlSubjectsMerge(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		switch r.URL.Path {
		case "/api/v1/subjects/sub01":
			w.Write([]byte(`{"uid":"sub01","name":"Anna N.","type":"person"}`))
		case "/api/v1/subjects/sub02":
			w.Write([]byte(`{"uid":"sub02","name":"Anna Nováková","type":"person"}`))
		default:
			w.Write([]byte(`{"keeper_uid":"sub02","source_uid":"sub01","markers_moved":17,
				"faces_moved":17,"shared_photos":3}`))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"subjects", "merge", "sub01", "sub02", "--yes")
	if err != nil {
		t.Fatalf("subjects merge returned %v", err)
	}
	last := seen[len(seen)-1]
	if last.path != "/api/v1/subjects/sub01/merge" || last.body["keeper_uid"] != "sub02" {
		t.Fatalf("merge request = %+v, want sub01 merged into sub02", last)
	}
	for _, want := range []string{"KEEPER", "Anna Nováková (sub02)", "MERGED AWAY", "Anna N. (sub01)", "17"} {
		if !strings.Contains(out, want) {
			t.Errorf("merge report does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlSubjectsMerge_needsConfirmation verifies an unconfirmed merge refuses,
// naming --yes, and that nothing is written on the way to refusing.
func TestCtlSubjectsMerge_needsConfirmation(t *testing.T) {
	var writes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "subjects", "merge", "sub01", "sub02")
	if err == nil {
		t.Fatal("an unconfirmed merge succeeded")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error %q does not tell the operator to pass --yes", err)
	}
	if writes != 0 {
		t.Errorf("%d writes were made, want none", writes)
	}
}

// TestCtlSubjectsMerge_dryRun verifies --dry-run names both people and writes
// nothing, needing no --yes of its own.
func TestCtlSubjectsMerge_dryRun(t *testing.T) {
	var writes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		if r.URL.Path == "/api/v1/subjects/sub02" {
			w.Write([]byte(`{"uid":"sub02","name":"Anna Nováková","type":"person"}`))
			return
		}
		w.Write([]byte(`{"uid":"sub01","name":"Anna N.","type":"person"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"subjects", "merge", "sub01", "sub02", "--dry-run")
	if err != nil {
		t.Fatalf("subjects merge --dry-run returned %v", err)
	}
	if !strings.Contains(out, "dry run") || !strings.Contains(out, "Anna N. (sub01)") ||
		!strings.Contains(out, "Anna Nováková (sub02)") {
		t.Errorf("dry run = %q, want both people named and nothing changed", out)
	}
	if writes != 0 {
		t.Errorf("%d writes were made by a dry run, want none", writes)
	}
}

// TestCtlSubjectsMerge_intoSelf verifies a subject merged into itself is refused
// before either lookup, since there is nothing sensible it could mean.
func TestCtlSubjectsMerge_intoSelf(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"subjects", "merge", "sub01", "sub01", "--yes")
	if err == nil {
		t.Fatal("merging a subject into itself succeeded")
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}

// TestCtlSubjectsDelete verifies a confirmed delete names the person it removed
// and says what happened to the faces that named them.
func TestCtlSubjectsDelete(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"subjects", "delete", "sub01", "--yes")
	if err != nil {
		t.Fatalf("subjects delete returned %v", err)
	}
	if len(seen) != 2 || seen[1].method != http.MethodDelete || seen[1].path != "/api/v1/subjects/sub01" {
		t.Fatalf("requests = %+v, want the subject named then deleted", seen)
	}
	if !strings.Contains(out, "Anna (sub01) deleted") || !strings.Contains(out, "unnamed") {
		t.Errorf("confirmation = %q, want the person named and the faces accounted for", out)
	}
}

// TestCtlSubjectsDelete_dryRun verifies --dry-run reports who would go, in the
// agent format too, and deletes nothing.
func TestCtlSubjectsDelete_dryRun(t *testing.T) {
	var deletes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
		}
		w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "llm",
		"subjects", "delete", "sub01", "--dry-run")
	if err != nil {
		t.Fatalf("subjects delete --dry-run returned %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal([]byte(out), &ack); err != nil {
		t.Fatalf("llm dry run is not valid JSON: %v (%q)", err, out)
	}
	message, _ := ack["message"].(string)
	if !strings.Contains(message, "dry run") || !strings.Contains(message, "Anna (sub01)") {
		t.Errorf("llm dry run = %v, want it to name who would go", ack)
	}
	if deletes != 0 {
		t.Errorf("%d deletes were made by a dry run, want none", deletes)
	}
}

// TestCtlSubjectsDelete_missing verifies a subject that does not exist fails on
// the lookup, so a mistyped uid never reaches the delete.
func TestCtlSubjectsDelete_missing(t *testing.T) {
	var deletes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"subject not found"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "subjects", "delete", "sub09", "--yes")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("deleting a missing subject = %v, want the 404 surfaced", err)
	}
	if deletes != 0 {
		t.Errorf("%d deletes were made, want none", deletes)
	}
}
