package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestCtlAlbumsUpdate verifies the command reads the album before it writes and
// sends the whole record back, so the fields nobody named survive.
func TestCtlAlbumsUpdate(t *testing.T) {
	var methods []string
	var gotBody map[string]any
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"uid":"alb01","title":"Trip","description":"Summer","private":true}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"alb01","title":"Journey","description":"Summer","private":true}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"albums", "update", "alb01", "--title", "Journey")
	if err != nil {
		t.Fatalf("albums update returned %v (%s)", err, out)
	}
	if len(methods) != 2 || methods[1] != http.MethodPatch {
		t.Fatalf("requests = %v, want a GET then a PATCH", methods)
	}
	if gotBody["title"] != "Journey" || gotBody["description"] != "Summer" || gotBody["private"] != true {
		t.Errorf("body = %v, want the new title and the untouched rest", gotBody)
	}
	if !strings.Contains(out, "Journey") {
		t.Errorf("output %q does not print the refreshed album", out)
	}
}

// TestCtlAlbumsUpdate_nothingToChange verifies an update naming no flag fails
// locally rather than rewriting the row and recording an audit entry for nothing.
func TestCtlAlbumsUpdate_nothingToChange(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("the server was contacted with %s despite an empty update", r.Method)
	})
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "update", "alb01"); err == nil {
		t.Error("albums update with no flags returned no error")
	}
}

// TestCtlAlbumsDelete verifies the gate: without --yes nothing is deleted, with
// --dry-run nothing is deleted either, and the confirmation names the album.
func TestCtlAlbumsDelete(t *testing.T) {
	var deletes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(`{"uid":"alb01","title":"Trip"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "delete", "alb01")
	if err == nil {
		t.Errorf("albums delete without --yes returned no error (%s)", out)
	}
	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "delete", "alb01", "--dry-run")
	if err != nil {
		t.Fatalf("albums delete --dry-run returned %v (%s)", err, out)
	}
	if !strings.Contains(out, "dry run") || !strings.Contains(out, "Trip (alb01)") {
		t.Errorf("dry-run output = %q, want it to name the album it would delete", out)
	}
	if deletes != 0 {
		t.Fatalf("the album was deleted %d times before --yes", deletes)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "delete", "alb01", "--yes")
	if err != nil {
		t.Fatalf("albums delete --yes returned %v (%s)", err, out)
	}
	if deletes != 1 || !strings.Contains(out, "photos stayed in the library") {
		t.Errorf("deletes = %d, output = %q, want one delete and the reassurance", deletes, out)
	}
}

// TestCtlLabelsUpdate verifies the read-modify-write and that a lone --priority
// keeps the label's name and review setting.
func TestCtlLabelsUpdate(t *testing.T) {
	var gotBody map[string]any
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"uid":"lbl01","name":"lake","priority":10,"review_enabled":true}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"lbl01","name":"lake","priority":3,"review_enabled":true}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"labels", "update", "lbl01", "--priority", "3")
	if err != nil {
		t.Fatalf("labels update returned %v (%s)", err, out)
	}
	if gotBody["name"] != "lake" || gotBody["priority"] != float64(3) || gotBody["review_enabled"] != true {
		t.Errorf("body = %v, want the new priority and the untouched rest", gotBody)
	}
}

// TestCtlLabelsUpdate_reviewOff verifies --review=false is sent as a deliberate
// false, which an unset boolean must never be.
func TestCtlLabelsUpdate_reviewOff(t *testing.T) {
	var gotBody map[string]any
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"uid":"lbl01","name":"lake","priority":10,"review_enabled":true}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"lbl01","name":"lake","priority":10,"review_enabled":false}`))
	})

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"labels", "update", "lbl01", "--review=false"); err != nil {
		t.Fatalf("labels update --review=false returned %v", err)
	}
	if gotBody["review_enabled"] != false {
		t.Errorf("body = %v, want review_enabled switched off", gotBody)
	}
}

// TestCtlLabelsDelete verifies the same gate as an album delete, with the promise
// that the photos survive.
func TestCtlLabelsDelete(t *testing.T) {
	var deletes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(`{"uid":"lbl01","name":"lake"}`))
	})

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "labels", "delete", "lbl01"); err == nil {
		t.Error("labels delete without --yes returned no error")
	}
	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "labels", "delete", "lbl01", "--yes")
	if err != nil {
		t.Fatalf("labels delete --yes returned %v (%s)", err, out)
	}
	if deletes != 1 || !strings.Contains(out, "lake (lbl01)") {
		t.Errorf("deletes = %d, output = %q, want one delete naming the label", deletes, out)
	}
}

// TestCtlStacksGroup verifies the grouping goes in one request and prints the
// resulting variants strip.
func TestCtlStacksGroup(t *testing.T) {
	var requests int
	var gotBody struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"uid":"pht01","stack_uid":"stk01","stack_members":[
			{"uid":"pht01","file_name":"a.JPG","is_primary":true},
			{"uid":"pht02","file_name":"a.NEF","is_primary":false}]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "stacks", "group", "pht01", "pht02")
	if err != nil {
		t.Fatalf("stacks group returned %v (%s)", err, out)
	}
	if requests != 1 || len(gotBody.PhotoUIDs) != 2 {
		t.Errorf("requests = %d, body = %v, want one request with both uids", requests, gotBody.PhotoUIDs)
	}
	if !strings.Contains(out, "a.NEF") || !strings.Contains(out, "stack stk01 groups 2 photos") {
		t.Errorf("output %q does not print the group", out)
	}
}

// TestCtlStacksGroup_tooSmall verifies cobra refuses a single photo before a
// client is even built.
func TestCtlStacksGroup_tooSmall(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted for a stack of one")
	})
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "stacks", "group", "pht01"); err == nil {
		t.Error("stacks group with one photo returned no error")
	}
}

// TestCtlStacksUngroup verifies the three per-photo commands reach their own
// endpoints and that an ungrouped photo prints the line that says so.
func TestCtlStacksUngroup(t *testing.T) {
	tests := []struct {
		args     []string
		wantPath string
	}{
		{args: []string{"set-primary", "pht01"}, wantPath: "/api/v1/photos/pht01/stack/primary"},
		{args: []string{"ungroup", "pht01"}, wantPath: "/api/v1/photos/pht01/unstack"},
		{args: []string{"ungroup-all", "pht01"}, wantPath: "/api/v1/photos/pht01/unstack-all"},
	}
	for _, tt := range tests {
		t.Run(tt.args[0], func(t *testing.T) {
			var gotPath string
			configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Write([]byte(`{"uid":"pht01"}`))
			})
			args := append([]string{"ctl", "--ctl-config", configPath, "stacks"}, tt.args...)
			out, err := runCtl(t, "", args...)
			if err != nil {
				t.Fatalf("stacks %v returned %v (%s)", tt.args, err, out)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if !strings.Contains(out, "is not stacked") {
				t.Errorf("output %q does not report the photo's standalone state", out)
			}
		})
	}
}

// TestCtlEditsGetAndSet verifies the read, and that a write merges onto the
// stored edit instead of replacing it wholesale.
func TestCtlEditsGetAndSet(t *testing.T) {
	stored := `{"photo_uid":"pht01","crop_x":0.1,"crop_y":0.1,"crop_w":0.8,"crop_h":0.8,"rotation":0}`
	var gotBody map[string]any
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(stored))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"photo_uid":"pht01","crop_x":0.1,"crop_y":0.1,"crop_w":0.8,"crop_h":0.8,` +
			`"rotation":90}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "edits", "get", "pht01")
	if err != nil {
		t.Fatalf("edits get returned %v (%s)", err, out)
	}
	if !strings.Contains(out, "0.1,0.1,0.8,0.8") {
		t.Errorf("output %q does not print the stored crop", out)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "edits", "set", "pht01", "--rotate", "90")
	if err != nil {
		t.Fatalf("edits set returned %v (%s)", err, out)
	}
	if gotBody["rotation"] != float64(90) || gotBody["crop_x"] != 0.1 {
		t.Errorf("body = %v, want the new rotation over the stored crop", gotBody)
	}
}

// TestCtlEditsSet_invalid verifies the two contradictions that need no round trip.
func TestCtlEditsSet_invalid(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("the server was written to with %s despite invalid input", r.Method)
			return
		}
		w.Write([]byte(`{"photo_uid":"pht01"}`))
	})

	for _, args := range [][]string{
		{"edits", "set", "pht01"},
		{"edits", "set", "pht01", "--crop", "0,0,0.5,0.5", "--clear-crop"},
		{"edits", "set", "pht01", "--rotate", "45"},
		{"edits", "set", "pht01", "--brightness", "2"},
	} {
		full := append([]string{"ctl", "--ctl-config", configPath}, args...)
		if _, err := runCtl(t, "", full...); err == nil {
			t.Errorf("%v returned no error", args)
		}
	}
}

// TestCtlEditsReset verifies the reset states the neutral edit in full — it is a
// write, and the thumbnails are rebuilt from it.
func TestCtlEditsReset(t *testing.T) {
	var methods []string
	var gotBody map[string]json.RawMessage
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"photo_uid":"pht01","rotation":0}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "edits", "reset", "pht01")
	if err != nil {
		t.Fatalf("edits reset returned %v (%s)", err, out)
	}
	if len(methods) != 1 || methods[0] != http.MethodPut {
		t.Errorf("requests = %v, want one PUT", methods)
	}
	if string(gotBody["crop_x"]) != "null" {
		t.Errorf("body = %v, want the crop cleared explicitly", gotBody)
	}
	if !strings.Contains(out, "NEUTRAL") {
		t.Errorf("output %q does not confirm the neutral edit", out)
	}
}

// TestCtlSavedSearches verifies the list, a create that folds the repeated
// --param flags into one view, and the delete.
func TestCtlSavedSearches(t *testing.T) {
	var gotBody map[string]any
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`{"saved_searches":[{"uid":"sav01","name":"Léto","params":{"q":"jezero"}}]}`))
		case http.MethodPost:
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"uid":"sav07","name":"Léto","params":{"q":"jezero","mode":"semantic"}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "saved-searches", "list")
	if err != nil || !strings.Contains(out, "q=jezero") {
		t.Fatalf("saved-searches list = %q, %v", out, err)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "saved-searches", "create", "Léto",
		"--param", "q=jezero", "--param", "mode=semantic")
	if err != nil {
		t.Fatalf("saved-searches create returned %v (%s)", err, out)
	}
	params, _ := gotBody["params"].(map[string]any)
	if params["q"] != "jezero" || params["mode"] != "semantic" {
		t.Errorf("body params = %v, want both --param pairs folded in", gotBody["params"])
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "saved-searches", "delete", "sav01")
	if err != nil || !strings.Contains(out, "no photo was touched") {
		t.Errorf("saved-searches delete = %q, %v", out, err)
	}
}

// TestCtlSavedSearches_notYours verifies the 404 the server answers for a foreign
// search surfaces as "not yours" rather than as a bare status.
func TestCtlSavedSearches_notYours(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"saved search not found"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "saved-searches", "get", "sav99")
	if err == nil {
		t.Fatalf("saved-searches get of a foreign search returned no error (%s)", out)
	}
	if !strings.Contains(err.Error(), "not yours") {
		t.Errorf("error = %v, want it to say the search is not yours", err)
	}
}

// TestCtlSavedSearches_paramConflict verifies --params and --param are refused
// together rather than one silently winning.
func TestCtlSavedSearches_paramConflict(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite conflicting flags")
	})
	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "saved-searches", "create", "X",
		"--params", `{"q":"a"}`, "--param", "mode=semantic")
	if err == nil {
		t.Error("--params with --param returned no error")
	}
}

// TestCtlPhotosSimilar verifies the neighbourhood prints with its distances.
func TestCtlPhotosSimilar(t *testing.T) {
	var gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"similar":[{"uid":"pht02","file_name":"b.jpg","distance":0.0821}]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "similar", "pht01", "--limit", "5")
	if err != nil {
		t.Fatalf("photos similar returned %v (%s)", err, out)
	}
	if gotQuery != "limit=5" {
		t.Errorf("query = %q, want the limit forwarded", gotQuery)
	}
	if !strings.Contains(out, "0.082") {
		t.Errorf("output %q does not print the distance", out)
	}
}

// TestCtlDuplicates verifies the listing and the four opinions, none of which
// merges anything.
func TestCtlDuplicates(t *testing.T) {
	var gotPath, gotMethod string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		if strings.HasSuffix(r.URL.Path, "/duplicates") {
			w.Write([]byte(`{"groups":[{"id":"pht01","reason":"phash","keeper_uid":"pht01","members":[` +
				`{"uid":"pht01","file_name":"a.jpg","is_keeper":true},` +
				`{"uid":"pht02","file_name":"b.jpg","is_keeper":false,"phash_distance":2}]}],` +
				`"total":1,"limit":20,"offset":0}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "duplicates", "list")
	if err != nil || !strings.Contains(out, "phash 2") {
		t.Fatalf("duplicates list = %q, %v", out, err)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "duplicates", "confirm", "pht01", "pht02")
	if err != nil {
		t.Fatalf("duplicates confirm returned %v (%s)", err, out)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/feedback/duplicate-confirmations" {
		t.Errorf("request = %s %s, want the confirmation endpoint", gotMethod, gotPath)
	}
	if !strings.Contains(out, "nothing was merged") {
		t.Errorf("output %q does not say the confirmation merged nothing", out)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"duplicates", "dismiss", "pht01", "pht02"); err != nil {
		t.Fatalf("duplicates dismiss returned %v", err)
	}
	if gotPath != "/api/v1/feedback/duplicate-dismissals" {
		t.Errorf("path = %q, want the dismissal endpoint", gotPath)
	}
}

// TestCtlComments verifies a thread reads back whole and that a comment posts
// with no author of its own — the server takes it from the token.
// tasksListBody is a one-task page, as the tasks endpoint serves one.
const tasksListBody = `{"tasks":[{"uid":"tk1","title":"V kterém roce?","state":"question",` +
	`"photo_count":3,"comment_count":1,"has_new_answer":true}],"total":1,"limit":50,"offset":0}`

// TestCtlTasks walks the command group against a stub server: the listing, the
// answered filter an agent polls with, opening a task and answering one.
func TestCtlTasks(t *testing.T) {
	var paths, methods []string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		methods = append(methods, r.Method)
		switch {
		case strings.HasSuffix(r.URL.Path, "/comments"):
			if r.Method == http.MethodPost {
				_, _ = w.Write([]byte(`{"uid":"cm1","task_uid":"tk1","body":"1987",` +
					`"created_at":"2026-09-18T12:00:00Z"}`))
				return
			}
			_, _ = w.Write([]byte(`{"comments":[]}`))
		case r.URL.Path == "/api/v1/tasks" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"uid":"tk2","title":"Nová","state":"question"}`))
		default:
			_, _ = w.Write([]byte(tasksListBody))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "tasks", "list", "--answered")
	if err != nil {
		t.Fatalf("tasks list: %v", err)
	}
	for _, want := range []string{"tk1", "V kterém roce?", "question", "yes", "1 of 1 task"} {
		if !strings.Contains(out, want) {
			t.Errorf("tasks list output does not contain %q:\n%s", want, out)
		}
	}
	if !strings.Contains(paths[0], "answered=true") {
		t.Errorf("the answered filter did not reach the wire: %q", paths[0])
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "tasks", "list", "--waiting"); err != nil {
		t.Fatalf("tasks list --waiting: %v", err)
	}
	if got := paths[len(paths)-1]; !strings.Contains(got, "waiting=true") {
		t.Errorf("the waiting filter did not reach the wire: %q", got)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "tasks", "create",
		"--title", "Nová", "ph1", "ph2"); err != nil {
		t.Fatalf("tasks create: %v", err)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "tasks", "comment",
		"tk1", "1987"); err != nil {
		t.Fatalf("tasks comment: %v", err)
	}
	if got := paths[len(paths)-1]; !strings.HasPrefix(got, "/api/v1/tasks/tk1/comments") {
		t.Errorf("the comment went to %q", got)
	}
	if got := methods[len(methods)-1]; got != http.MethodPost {
		t.Errorf("the comment used %s, want POST", got)
	}
}

// TestCtlTasks_updateNeedsAField verifies an update naming nothing is refused
// before the server is contacted.
func TestCtlTasks_updateNeedsAField(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite an empty update")
	})
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"tasks", "update", "tk1"); err == nil {
		t.Error("an update naming no field succeeded, want a refusal")
	}
}

// TestCtlTasks_deleteNeedsConfirmation verifies the irreversible gate: the
// command reads the task to name it, then refuses without --yes.
func TestCtlTasks_deleteNeedsConfirmation(t *testing.T) {
	var methods []string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		_, _ = w.Write([]byte(`{"uid":"tk1","title":"V kterém roce?","state":"question","photo_count":3}`))
	})
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"tasks", "delete", "tk1"); err == nil {
		t.Error("delete without --yes succeeded, want a refusal")
	}
	for _, method := range methods {
		if method == http.MethodDelete {
			t.Error("the task was deleted despite the missing confirmation")
		}
	}

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"tasks", "delete", "tk1", "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "dry run") || !strings.Contains(out, "V kterém roce?") {
		t.Errorf("dry run output = %q, want it to name the task", out)
	}
}

func TestCtlComments(t *testing.T) {
	var gotBody map[string]string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"comments":[{"uid":"cmt01","photo_uid":"pht01","author_uid":"usr01",` +
				`"author_name":"Anna","body":"Babička na dvoře.","created_at":"2024-05-01T10:22:33Z"}]}`))
			return
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"cmt09","photo_uid":"pht01","author_uid":"usr09","author_name":"Agent",` +
			`"body":"Podle EXIF 1978.","created_at":"2024-05-03T08:00:00Z"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "comments", "list", "pht01")
	if err != nil {
		t.Fatalf("comments list returned %v (%s)", err, out)
	}
	if !strings.Contains(out, "Anna (usr01)") || !strings.Contains(out, "Babička na dvoře.") {
		t.Errorf("output %q does not print the thread", out)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath,
		"comments", "add", "pht01", "Podle EXIF 1978.")
	if err != nil {
		t.Fatalf("comments add returned %v (%s)", err, out)
	}
	if gotBody["body"] != "Podle EXIF 1978." {
		t.Errorf("body = %v, want the comment text", gotBody)
	}
	if _, sent := gotBody["author_uid"]; sent {
		t.Errorf("body = %v, want no author: the token's owner is the author", gotBody)
	}
	if !strings.Contains(out, "Agent (usr09)") {
		t.Errorf("output %q does not echo the created comment", out)
	}
}

// TestCtlCuration_llmOutput verifies -o llm reaches the resources added here, so
// the format really is a rule about keys rather than a per-command feature.
func TestCtlCuration_llmOutput(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		body   string
		want   string
		absent string
	}{
		{
			name:   "saved searches",
			args:   []string{"saved-searches", "list"},
			body:   `{"saved_searches":[{"uid":"sav01","name":"Léto","params":{"q":"jezero"}}]}`,
			want:   `"name":"Léto"`,
			absent: "created_at",
		},
		{
			name:   "similar",
			args:   []string{"photos", "similar", "pht01"},
			body:   `{"similar":[{"uid":"pht02","distance":0.08,"thumb_url":"https://x/y","title":""}]}`,
			want:   `"distance":0.08`,
			absent: "thumb_url",
		},
		{
			name:   "comments",
			args:   []string{"comments", "list", "pht01"},
			body:   `{"comments":[{"uid":"cmt01","body":"Ahoj","author_name":"Anna","edited_at":null}]}`,
			want:   `"body":"Ahoj"`,
			absent: "edited_at",
		},
		{
			name:   "image edit",
			args:   []string{"edits", "get", "pht01"},
			body:   `{"photo_uid":"pht01","rotation":90,"brightness":0,"updated_at":"2024-05-01T10:22:33Z"}`,
			want:   `"rotation":90`,
			absent: "brightness",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Write([]byte(tt.body))
			})
			args := append([]string{"ctl", "--ctl-config", configPath, "-o", "llm"}, tt.args...)
			out, err := runCtl(t, "", args...)
			if err != nil {
				t.Fatalf("%v returned %v (%s)", tt.args, err, out)
			}
			if !strings.Contains(out, tt.want) {
				t.Errorf("llm output %q does not contain %q", out, tt.want)
			}
			if strings.Contains(out, tt.absent) {
				t.Errorf("llm output %q still contains %q, which teaches nothing", out, tt.absent)
			}
		})
	}
}

// participantsBody is a task's people as the endpoint serves them: one who
// joined by acting, one who was asked.
const participantsBody = `{"participants":[` +
	`{"user_uid":"us1","name":"Pan Botka","joined_at":"2026-09-18T10:00:00Z","added_by":""},` +
	`{"user_uid":"us2","name":"Teta","joined_at":"2026-09-18T11:00:00Z",` +
	`"added_by":"us1","added_by_name":"Pan Botka"}]}`

// TestCtlTaskParticipants verifies the three participant commands reach the
// right routes, that the listing tells being asked from having acted, and that
// --mine becomes the filter the server understands.
func TestCtlTaskParticipants(t *testing.T) {
	var paths, methods []string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		methods = append(methods, r.Method)
		switch {
		case strings.Contains(r.URL.Path, "/participants"):
			_, _ = w.Write([]byte(participantsBody))
		case r.URL.Path == "/api/v1/users":
			_, _ = w.Write([]byte(`{"users":[{"uid":"us1","name":"Pan Botka"}]}`))
		default:
			_, _ = w.Write([]byte(tasksListBody))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "tasks", "participants", "tk1")
	if err != nil {
		t.Fatalf("tasks participants: %v", err)
	}
	// The one distinction the table exists to show.
	for _, want := range []string{"us1", "Pan Botka", "acted", "Teta", "asked by"} {
		if !strings.Contains(out, want) {
			t.Errorf("participants output does not contain %q:\n%s", want, out)
		}
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"tasks", "assign", "tk1", "us2"); err != nil {
		t.Fatalf("tasks assign: %v", err)
	}
	if got, want := methods[len(methods)-1], http.MethodPost; got != want {
		t.Errorf("assign used %s, want %s", got, want)
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"tasks", "unassign", "tk1", "us2"); err != nil {
		t.Fatalf("tasks unassign: %v", err)
	}
	if got, want := methods[len(methods)-1], http.MethodDelete; got != want {
		t.Errorf("unassign used %s, want %s", got, want)
	}
	if got := paths[len(paths)-1]; !strings.HasPrefix(got, "/api/v1/tasks/tk1/participants/us2") {
		t.Errorf("unassign went to %q", got)
	}

	// --mine is the agent's "what have I been put on?".
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"tasks", "list", "--mine"); err != nil {
		t.Fatalf("tasks list --mine: %v", err)
	}
	if got := paths[len(paths)-1]; !strings.Contains(got, "participant=me") {
		t.Errorf("--mine did not reach the wire: %q", got)
	}

	people, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "people")
	if err != nil {
		t.Fatalf("people: %v", err)
	}
	if !strings.Contains(people, "us1") || !strings.Contains(people, "Pan Botka") {
		t.Errorf("people output does not list the account:\n%s", people)
	}
}
