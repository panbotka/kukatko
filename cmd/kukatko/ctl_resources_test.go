package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// The bare list envelopes of the three resources that do not page, exactly as
// internal/organizeapi and internal/peopleapi shape them.
const (
	albumsListBody = `{"albums":[{"uid":"alb01","title":"Trip","type":"album","photo_count":12,` +
		`"private":false},{"uid":"alb02","title":"Moments","type":"moment","photo_count":3,"private":true}]}`
	labelsListBody = `{"labels":[{"uid":"lbl01","name":"lake","priority":10,"photo_count":42}]}`
	subjectsBody   = `{"subjects":[{"uid":"sub01","name":"Anna","type":"person","favorite":true,` +
		`"marker_count":128}]}`
	bulkBody = `{"results":[{"photo_uid":"pht01","status":"updated"}],` +
		`"counts":{"total":1,"updated":1,"skipped":0,"errored":0}}`
)

// TestCtlAlbums_listAndGet verifies the album list and detail render as compact
// tables from the bare {"albums": […]} envelope, and that -o json passes the
// API's own bytes through unchanged.
func TestCtlAlbums_listAndGet(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/alb01") {
			w.Write([]byte(`{"uid":"alb01","slug":"trip","title":"Trip","description":"Summer",
				"type":"album","private":true}`))
			return
		}
		w.Write([]byte(albumsListBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "list")
	if err != nil {
		t.Fatalf("albums list returned %v", err)
	}
	if gotPath != "/api/v1/albums" {
		t.Errorf("path = %q, want /api/v1/albums", gotPath)
	}
	for _, want := range []string{"UID", "TITLE", "PHOTOS", "alb01", "Trip", "12", "moment"} {
		if !strings.Contains(out, want) {
			t.Errorf("album table does not contain %q:\n%s", want, out)
		}
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "albums", "list")
	if err != nil {
		t.Fatalf("albums list -o json returned %v", err)
	}
	if out != albumsListBody+"\n" {
		t.Errorf("json output was not passed through unchanged:\ngot  %q\nwant %q", out, albumsListBody+"\n")
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "get", "alb01")
	if err != nil {
		t.Fatalf("albums get returned %v", err)
	}
	if gotPath != "/api/v1/albums/alb01" {
		t.Errorf("path = %q, want the detail path", gotPath)
	}
	for _, want := range []string{"UID", "alb01", "DESCRIPTION", "Summer", "PRIVATE", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("album detail does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlAlbums_listEmpty verifies an empty library prints one line and no header.
func TestCtlAlbums_listEmpty(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"albums":[]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "list")
	if err != nil {
		t.Fatalf("albums list returned %v", err)
	}
	if out != "no albums found\n" {
		t.Errorf("empty album table = %q, want a single no-albums line", out)
	}
}

// TestCtlLabels_listAndGet verifies the label list and detail render from the bare
// {"labels": […]} envelope.
func TestCtlLabels_listAndGet(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/lbl01") {
			w.Write([]byte(`{"uid":"lbl01","slug":"lake","name":"lake","priority":10}`))
			return
		}
		w.Write([]byte(labelsListBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "labels", "list")
	if err != nil {
		t.Fatalf("labels list returned %v", err)
	}
	if gotPath != "/api/v1/labels" {
		t.Errorf("path = %q, want /api/v1/labels", gotPath)
	}
	for _, want := range []string{"UID", "NAME", "PRIORITY", "PHOTOS", "lbl01", "lake", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("label table does not contain %q:\n%s", want, out)
		}
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "labels", "get", "lbl01")
	if err != nil {
		t.Fatalf("labels get returned %v", err)
	}
	if gotPath != "/api/v1/labels/lbl01" {
		t.Errorf("path = %q, want the detail path", gotPath)
	}
	for _, want := range []string{"UID", "lbl01", "SLUG", "lake", "PRIORITY", "10"} {
		if !strings.Contains(out, want) {
			t.Errorf("label detail does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlSubjects_listGetAndPhotos verifies the subject list and detail render from
// the bare {"subjects": […]} envelope, and that the gallery reuses the /photos one.
func TestCtlSubjects_listGetAndPhotos(t *testing.T) {
	var gotPath, gotQuery string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		switch {
		case strings.HasSuffix(r.URL.Path, "/photos"):
			w.Write([]byte(listBody))
		case strings.HasSuffix(r.URL.Path, "/sub01"):
			w.Write([]byte(`{"uid":"sub01","slug":"anna","name":"Anna","type":"person","notes":"sister"}`))
		default:
			w.Write([]byte(subjectsBody))
		}
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "subjects", "list")
	if err != nil {
		t.Fatalf("subjects list returned %v", err)
	}
	for _, want := range []string{"UID", "NAME", "MARKERS", "sub01", "Anna", "128"} {
		if !strings.Contains(out, want) {
			t.Errorf("subject table does not contain %q:\n%s", want, out)
		}
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "subjects", "get", "sub01")
	if err != nil {
		t.Fatalf("subjects get returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub01" {
		t.Errorf("path = %q, want the detail path", gotPath)
	}
	if !strings.Contains(out, "NOTES") || !strings.Contains(out, "sister") {
		t.Errorf("subject detail does not carry the notes:\n%s", out)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "subjects", "photos", "sub01", "--limit", "2")
	if err != nil {
		t.Fatalf("subjects photos returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub01/photos" || !strings.Contains(gotQuery, "limit=2") {
		t.Errorf("request = %s?%s, want the paged gallery path", gotPath, gotQuery)
	}
	if !strings.Contains(out, "pht01") || !strings.Contains(out, "2 of 42 photos") {
		t.Errorf("subject gallery does not render as a photo page:\n%s", out)
	}
}

// TestCtlFavorites verifies the favorites page renders as a photo list and that a
// toggle, which the API answers with a bare 204, still confirms in both formats.
func TestCtlFavorites(t *testing.T) {
	var gotMethod, gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if strings.HasSuffix(r.URL.Path, "/favorite") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte(listBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "favorites", "list")
	if err != nil {
		t.Fatalf("favorites list returned %v", err)
	}
	if gotPath != "/api/v1/favorites" {
		t.Errorf("path = %q, want /api/v1/favorites", gotPath)
	}
	if !strings.Contains(out, "pht01") {
		t.Errorf("favorites list does not render the photo page:\n%s", out)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "favorites", "add", "pht01")
	if err != nil {
		t.Fatalf("favorites add returned %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/v1/photos/pht01/favorite" {
		t.Errorf("request = %s %s, want PUT on the favorite path", gotMethod, gotPath)
	}
	if out != "photo pht01 favorited\n" {
		t.Errorf("favorites add output = %q, want a one-line confirmation", out)
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "favorites", "remove", "pht01")
	if err != nil {
		t.Fatalf("favorites remove -o json returned %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	var ack map[string]string
	if err := json.Unmarshal([]byte(out), &ack); err != nil {
		t.Fatalf("204 confirmation is not valid JSON: %v (%q)", err, out)
	}
	if ack["status"] != "ok" || !strings.Contains(ack["message"], "unfavorited") {
		t.Errorf("json confirmation = %v, want an ok status and a message", ack)
	}
}

// TestCtlRatingSet verifies the star argument and the --flag option travel as the
// independent optional pair the API expects.
func TestCtlRatingSet(t *testing.T) {
	var gotBody map[string]any
	var gotMethod string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotBody = nil
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "rating", "set", "pht01", "4")
	if err != nil {
		t.Fatalf("rating set returned %v", err)
	}
	if gotMethod != http.MethodPut || gotBody["rating"] != float64(4) {
		t.Errorf("request = %s %v, want PUT with rating 4", gotMethod, gotBody)
	}
	if _, set := gotBody["flag"]; set {
		t.Errorf("body = %v, want no flag so the server leaves it alone", gotBody)
	}
	if !strings.Contains(out, "4/5") {
		t.Errorf("rating set output = %q, want the new rating", out)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"rating", "set", "pht01", "--flag", "pick"); err != nil {
		t.Fatalf("rating set --flag returned %v", err)
	}
	if gotBody["flag"] != "pick" {
		t.Errorf("body = %v, want the pick flag", gotBody)
	}
	if _, set := gotBody["rating"]; set {
		t.Errorf("body = %v, want no rating so the server leaves the stars alone", gotBody)
	}
}

// TestCtlRatingSet_invalid verifies a non-numeric and an out-of-range star value
// are refused before the server is contacted.
func TestCtlRatingSet_invalid(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with an invalid rating")
	})

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "rating", "set", "pht01", "many"); err == nil {
		t.Error("a non-numeric rating returned no error")
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "rating", "set", "pht01", "9"); err == nil {
		t.Error("an out-of-range rating returned no error")
	}
	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "rating", "set", "pht01"); err == nil {
		t.Error("a rating command that changes nothing returned no error")
	}
}

// TestCtlLabelsAttach verifies attach and detach reach the label photos path with
// the right verb and confirm with one line.
func TestCtlLabelsAttach(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "labels", "attach", "lbl01", "pht01")
	if err != nil {
		t.Fatalf("labels attach returned %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/labels/lbl01/photos" {
		t.Errorf("request = %s %s, want POST on the label photos path", gotMethod, gotPath)
	}
	if gotBody["photo_uid"] != "pht01" {
		t.Errorf("body = %v, want the photo uid", gotBody)
	}
	if !strings.Contains(out, "attached") {
		t.Errorf("attach output = %q, want a confirmation", out)
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"labels", "detach", "lbl01", "pht01"); err != nil {
		t.Fatalf("labels detach returned %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
}

// TestCtlAlbumsAddPhotos verifies the membership command sends the whole uid list
// in one request and reports the album's new size.
func TestCtlAlbumsAddPhotos(t *testing.T) {
	var requests int
	var gotMethod string
	var gotBody struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"photo_uids":["pht01","pht02"]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"albums", "add-photos", "alb01", "pht01", "pht02")
	if err != nil {
		t.Fatalf("albums add-photos returned %v", err)
	}
	if requests != 1 {
		t.Errorf("the command made %d requests, want one for the whole list", requests)
	}
	if gotMethod != http.MethodPost || len(gotBody.PhotoUIDs) != 2 {
		t.Errorf("request = %s with %v, want both uids in one POST", gotMethod, gotBody.PhotoUIDs)
	}
	if !strings.Contains(out, "album alb01 now holds 2 photos") {
		t.Errorf("membership output = %q, want the album's new size", out)
	}
}

// TestCtlViewerForbidden verifies a viewer's token — which authenticates fine but
// may not mutate — is told exactly that, rather than shown the server's opaque
// body. Every mutating command shares the transport, so one is enough to pin it.
func TestCtlViewerForbidden(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"insufficient permissions"}`))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "albums", "create", "Trip")
	if err == nil {
		t.Fatal("albums create against a viewer's token returned no error")
	}
	msg := err.Error()
	for _, want := range []string{"403", "permission denied", "editor or admin"} {
		if !strings.Contains(msg, want) {
			t.Errorf("403 error %q does not mention %q", msg, want)
		}
	}
	if strings.Contains(msg, "insufficient permissions") {
		t.Errorf("403 error dumps the server's response body: %q", msg)
	}
	if strings.Contains(msg, "supersecret") {
		t.Errorf("403 error leaks the token: %q", msg)
	}
}

// bulkServer starts a server that counts bulk requests and records the last body,
// returning the ctl config path plus accessors for what the command sent.
func bulkServer(t *testing.T) (configPath string, requests *int, body *bulkCapture) {
	t.Helper()

	requests = new(int)
	body = &bulkCapture{}
	configPath = ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		*requests++
		json.NewDecoder(r.Body).Decode(body)
		w.Write([]byte(bulkBody))
	})
	return configPath, requests, body
}

// bulkCapture is the bulk request body, as the fake server receives it.
type bulkCapture struct {
	PhotoUIDs  []string `json:"photo_uids"`
	Operations struct {
		AddLabels []string `json:"add_labels"`
		Archive   bool     `json:"archive"`
	} `json:"operations"`
}

// bulkUIDs builds n distinct photo uids, for exercising the confirmation gate.
func bulkUIDs(n int) []string {
	uids := make([]string, 0, n)
	for i := range n {
		uids = append(uids, "pht"+strconv.Itoa(i))
	}
	return uids
}

// TestCtlBulk_singleRequest verifies the whole batch travels in one POST, matching
// the server's single-transaction contract. A per-photo loop would trade that
// atomicity for N transactions and N audit entries.
func TestCtlBulk_singleRequest(t *testing.T) {
	configPath, requests, body := bulkServer(t)

	args := append([]string{"ctl", "--ctl-config", configPath, "bulk"}, bulkUIDs(3)...)
	out, err := runCtl(t, "", append(args, "--add-label", "lbl01")...)
	if err != nil {
		t.Fatalf("bulk returned %v", err)
	}
	if *requests != 1 {
		t.Errorf("bulk made %d requests, want exactly one for the whole batch", *requests)
	}
	if len(body.PhotoUIDs) != 3 {
		t.Errorf("body carried %d uids, want all three in one request", len(body.PhotoUIDs))
	}
	if len(body.Operations.AddLabels) != 1 || body.Operations.AddLabels[0] != "lbl01" {
		t.Errorf("operations = %+v, want the add-label operation", body.Operations)
	}
	if !strings.Contains(out, "1 photo · 1 updated") {
		t.Errorf("bulk output = %q, want the aggregate summary", out)
	}
}

// TestCtlBulk_readsUIDsFromStdin verifies the command consumes a `ctl photos list
// -o json` envelope directly, which is the pipeline it exists to close.
func TestCtlBulk_readsUIDsFromStdin(t *testing.T) {
	configPath, requests, body := bulkServer(t)

	out, err := runCtl(t, listBody, "ctl", "--ctl-config", configPath, "bulk", "--archive")
	if err != nil {
		t.Fatalf("bulk from stdin returned %v", err)
	}
	if *requests != 1 {
		t.Errorf("bulk made %d requests, want one", *requests)
	}
	if len(body.PhotoUIDs) != 2 || body.PhotoUIDs[0] != "pht01" {
		t.Errorf("photo_uids = %v, want the uids parsed out of the piped envelope", body.PhotoUIDs)
	}
	if !body.Operations.Archive {
		t.Errorf("operations = %+v, want the archive operation", body.Operations)
	}
	if !strings.Contains(out, "1 updated") {
		t.Errorf("bulk output = %q, want the aggregate summary", out)
	}
}

// TestCtlBulk_noOperations verifies a batch that would change nothing is refused
// before stdin is drained or the server is contacted.
func TestCtlBulk_noOperations(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with no operations")
	})

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "bulk", "pht01"); err == nil {
		t.Error("bulk with no operations returned no error")
	}
}

// TestCtlBulk_confirmationGate verifies a batch above the threshold asks before it
// acts: a declined prompt makes no request, an accepted one makes exactly one, and
// --yes skips the question. A batch at the threshold is never questioned.
func TestCtlBulk_confirmationGate(t *testing.T) {
	tests := []struct {
		name         string
		photos       int
		stdin        string
		yes          bool
		wantRequests int
		wantErr      bool
		wantPrompt   bool
	}{
		{name: "at the threshold, no question", photos: 50, wantRequests: 1},
		{name: "above the threshold, declined", photos: 51, stdin: "n\n", wantPrompt: true, wantErr: true},
		{name: "above the threshold, empty answer", photos: 51, stdin: "\n", wantPrompt: true, wantErr: true},
		{
			name:   "above the threshold, accepted",
			photos: 51, stdin: "y\n", wantPrompt: true, wantRequests: 1,
		},
		{
			name:   "above the threshold, accepted with a word",
			photos: 51, stdin: "YES\n", wantPrompt: true, wantRequests: 1,
		},
		{name: "above the threshold with --yes", photos: 51, yes: true, wantRequests: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath, requests, _ := bulkServer(t)

			args := append([]string{"ctl", "--ctl-config", configPath, "bulk"}, bulkUIDs(tt.photos)...)
			args = append(args, "--archive")
			if tt.yes {
				args = append(args, "--yes")
			}
			out, err := runCtl(t, tt.stdin, args...)

			if tt.wantErr && err == nil {
				t.Error("a declined batch returned no error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("bulk returned %v", err)
			}
			if *requests != tt.wantRequests {
				t.Errorf("bulk made %d requests, want %d", *requests, tt.wantRequests)
			}
			gotPrompt := strings.Contains(out, "Continue? [y/N]")
			if gotPrompt != tt.wantPrompt {
				t.Errorf("prompt shown = %v, want %v:\n%s", gotPrompt, tt.wantPrompt, out)
			}
			if tt.wantPrompt && !strings.Contains(out, strconv.Itoa(tt.photos)+" photos") {
				t.Errorf("prompt does not name the batch size:\n%s", out)
			}
		})
	}
}

// TestCtlBulk_confirmationNeedsYesWhenPiped verifies a large batch whose uids came
// from stdin fails with an actionable message instead of silently proceeding: the
// stream that would carry the answer has already been consumed by the uid list.
func TestCtlBulk_confirmationNeedsYesWhenPiped(t *testing.T) {
	configPath, requests, _ := bulkServer(t)

	piped := strings.Join(bulkUIDs(51), "\n")
	_, err := runCtl(t, piped, "ctl", "--ctl-config", configPath, "bulk", "--archive")
	if err == nil {
		t.Fatal("a large piped batch proceeded without confirmation")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error %q does not tell the operator to pass --yes", err)
	}
	if *requests != 0 {
		t.Errorf("bulk made %d requests, want none before confirmation", *requests)
	}

	out, err := runCtl(t, piped, "ctl", "--ctl-config", configPath, "bulk", "--archive", "--yes")
	if err != nil {
		t.Fatalf("bulk --yes from stdin returned %v", err)
	}
	if *requests != 1 {
		t.Errorf("bulk --yes made %d requests, want one", *requests)
	}
	if strings.Contains(out, "Continue?") {
		t.Errorf("--yes still prompted:\n%s", out)
	}
}

// TestCtlAlbumsAddPhotos_confirmationGate verifies the album membership commands
// share the bulk gate: they touch as many photos, and just as irreversibly.
func TestCtlAlbumsAddPhotos_confirmationGate(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(`{"photo_uids":[]}`))
	})

	args := append([]string{"ctl", "--ctl-config", configPath, "albums", "add-photos", "alb01"}, bulkUIDs(51)...)
	out, err := runCtl(t, "n\n", args...)
	if err == nil {
		t.Fatal("a declined membership batch returned no error")
	}
	if requests != 0 {
		t.Errorf("add-photos made %d requests after being declined, want none", requests)
	}
	if !strings.Contains(out, "add 51 photos to album alb01") {
		t.Errorf("prompt does not describe the mutation:\n%s", out)
	}

	if _, err := runCtl(t, "y\n", args...); err != nil {
		t.Fatalf("an accepted membership batch returned %v", err)
	}
	if requests != 1 {
		t.Errorf("add-photos made %d requests once accepted, want one", requests)
	}
}
