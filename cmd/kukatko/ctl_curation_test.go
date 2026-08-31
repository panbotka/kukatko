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
