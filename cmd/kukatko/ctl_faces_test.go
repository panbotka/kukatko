package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The two recognition envelopes, exactly as internal/facematch and
// internal/clusterapi shape them.
const (
	ctlFacesBody = `{"photo_uid":"pht01","width":4000,"height":3000,"orientation":1,"faces":[` +
		`{"face_index":0,"bbox":[0.1,0.2,0.3,0.4],"det_score":0.94,"action":"already_done",` +
		`"marker_uid":"mrk01","subject_uid":"sub01","subject_name":"Anna","suggestions":[]},` +
		`{"face_index":1,"bbox":[0.5,0.2,0.2,0.25],"det_score":0.88,"action":"create_marker",` +
		`"suggestions":[{"subject_uid":"sub02","subject_name":"Bob","distance":0.31,"confidence":0.69}]}]}`
	ctlClustersBody = `{"total":1,"pending":4,"limit":24,"offset":0,"next_offset":null,` +
		`"clusters":[{"uid":"clu01","size":12,` +
		`"representative":{"photo_uid":"pht01","face_index":0,"bbox":[0.1,0.2,0.3,0.4],"det_score":0.9},` +
		`"examples":[],"suggestion":{"subject_uid":"sub01","subject_name":"Anna","distance":0.28,` +
		`"confidence":0.72},"created_at":"2026-08-01T10:00:00Z"}]}`
)

// recordedRequest is one call a test server saw, reduced to what the assertions
// care about.
type recordedRequest struct {
	method string
	path   string
	body   map[string]any
}

// recordRequest captures a request's method, path and JSON body, if any.
func recordRequest(r *http.Request) recordedRequest {
	rec := recordedRequest{method: r.Method, path: r.URL.Path}
	raw, err := io.ReadAll(r.Body)
	if err == nil && len(raw) > 0 {
		json.Unmarshal(raw, &rec.body)
	}
	return rec
}

// TestCtlPhotosFaces_table verifies the face listing renders as a table naming the
// people, and that -o json passes the API's own bytes through unchanged.
func TestCtlPhotosFaces_table(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(ctlFacesBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "faces", "pht01")
	if err != nil {
		t.Fatalf("photos faces returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/faces" {
		t.Errorf("path = %q, want the faces endpoint", gotPath)
	}
	for _, want := range []string{"FACE", "WHO", "Anna (sub01)", "mrk01", "Bob 0.310", "2 faces"} {
		if !strings.Contains(out, want) {
			t.Errorf("face table does not contain %q:\n%s", want, out)
		}
	}

	out, err = runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "json", "photos", "faces", "pht01")
	if err != nil {
		t.Fatalf("photos faces -o json returned %v", err)
	}
	if out != ctlFacesBody+"\n" {
		t.Errorf("json output was not passed through unchanged:\n%s", out)
	}
}

// TestCtlFacesAssign_routing verifies the assignment reads the photo's faces first
// and then sends the action that face needs: naming an existing marker, or drawing
// one over a bare detection.
func TestCtlFacesAssign_routing(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantAction string
		check      func(*testing.T, map[string]any)
	}{
		{
			name:       "onto an existing marker",
			args:       []string{"faces", "assign", "pht01", "0", "sub02"},
			wantAction: "assign_person",
			check: func(t *testing.T, body map[string]any) {
				if body["marker_uid"] != "mrk01" || body["subject_uid"] != "sub02" {
					t.Errorf("body = %v, want the marker named for sub02", body)
				}
			},
		},
		{
			name:       "onto a bare detection, by name",
			args:       []string{"faces", "assign", "pht01", "1", "--name", "Bob"},
			wantAction: "create_marker",
			check: func(t *testing.T, body map[string]any) {
				if body["subject_name"] != "Bob" || body["face_index"] != float64(1) {
					t.Errorf("body = %v, want a marker created for Bob on face 1", body)
				}
				if _, present := body["bbox"]; !present {
					t.Errorf("body = %v, want the detection's box carried over", body)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen []recordedRequest
			configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
				seen = append(seen, recordRequest(r))
				if r.Method == http.MethodGet {
					w.Write([]byte(ctlFacesBody))
					return
				}
				w.Write([]byte(`{"action":"assign_person","marker":{"uid":"mrk01","photo_uid":"pht01"},
					"subject":{"uid":"sub02","name":"Bob"}}`))
			})

			args := append([]string{"ctl", "--ctl-config", configPath}, tc.args...)
			out, err := runCtl(t, "", args...)
			if err != nil {
				t.Fatalf("faces assign returned %v", err)
			}
			if len(seen) != 2 || seen[0].method != http.MethodGet {
				t.Fatalf("requests = %+v, want a face listing then an assignment", seen)
			}
			if seen[1].path != "/api/v1/photos/pht01/faces/assign" {
				t.Errorf("path = %q, want the assign endpoint", seen[1].path)
			}
			if seen[1].body["action"] != tc.wantAction {
				t.Errorf("action = %v, want %q", seen[1].body["action"], tc.wantAction)
			}
			tc.check(t, seen[1].body)
			if !strings.Contains(out, "Bob (sub02)") {
				t.Errorf("confirmation = %q, want the person named, not just a uid", out)
			}
		})
	}
}

// TestCtlFacesAssign_unknownFace verifies a face index the photo does not have
// fails against the listing — naming the indexes it does have — without an
// assignment ever being sent.
func TestCtlFacesAssign_unknownFace(t *testing.T) {
	var writes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Write([]byte(ctlFacesBody))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "faces", "assign", "pht01", "7", "sub02")
	if err == nil {
		t.Fatal("assigning a face the photo does not have succeeded")
	}
	if !strings.Contains(err.Error(), "0, 1") {
		t.Errorf("error %q does not say which faces the photo has", err)
	}
	if writes != 0 {
		t.Errorf("%d assignments were sent, want none", writes)
	}
}

// TestCtlFacesAssign_needsSubject verifies naming neither a uid nor a --name is
// refused locally, before the photo is even read.
func TestCtlFacesAssign_needsSubject(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(ctlFacesBody))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "faces", "assign", "pht01", "0")
	if err == nil || !strings.Contains(err.Error(), "--name") {
		t.Fatalf("assign without a subject = %v, want an error naming --name", err)
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}

// TestCtlFacesDetach verifies a detach sends unassign_person for the face's marker
// and reports that the marker now names nobody.
func TestCtlFacesDetach(t *testing.T) {
	var seen []recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, recordRequest(r))
		if r.Method == http.MethodGet {
			w.Write([]byte(ctlFacesBody))
			return
		}
		w.Write([]byte(`{"action":"unassign_person","marker":{"uid":"mrk01","photo_uid":"pht01"}}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "faces", "detach", "pht01", "0")
	if err != nil {
		t.Fatalf("faces detach returned %v", err)
	}
	if len(seen) != 2 || seen[1].body["action"] != "unassign_person" ||
		seen[1].body["marker_uid"] != "mrk01" {
		t.Fatalf("requests = %+v, want a detach of mrk01", seen)
	}
	if !strings.Contains(out, "names nobody") {
		t.Errorf("confirmation = %q, want it to say the marker is unnamed", out)
	}
}

// TestCtlFacesDetach_unnamedFace verifies detaching a detection nobody has named
// is refused locally rather than sent as a request the server would reject.
func TestCtlFacesDetach_unnamedFace(t *testing.T) {
	var writes int
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes++
		}
		w.Write([]byte(ctlFacesBody))
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "faces", "detach", "pht01", "1")
	if err == nil {
		t.Fatal("detaching an unnamed face succeeded")
	}
	if writes != 0 {
		t.Errorf("%d assignments were sent, want none", writes)
	}
}

// TestCtlFacesOpinions verifies each of the four feedback commands names the
// person before writing its 204-only opinion, and says so in both output formats.
func TestCtlFacesOpinions(t *testing.T) {
	cases := []struct {
		command string
		path    string
		method  string
		phrase  string
	}{
		{"reject", "/api/v1/feedback/face-rejections", http.MethodPost, "rejected as Anna (sub01)"},
		{"unreject", "/api/v1/feedback/face-rejections", http.MethodDelete, "no longer rejected as"},
		{"confirm", "/api/v1/feedback/face-confirmations", http.MethodPost, "confirmed as Anna (sub01)"},
		{"unconfirm", "/api/v1/feedback/face-confirmations", http.MethodDelete, "no longer confirmed as"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			var seen []recordedRequest
			configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
				seen = append(seen, recordRequest(r))
				if strings.HasPrefix(r.URL.Path, "/api/v1/subjects/") {
					w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})

			out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
				"faces", tc.command, "pht01", "2", "sub01")
			if err != nil {
				t.Fatalf("faces %s returned %v", tc.command, err)
			}
			if len(seen) != 2 || seen[0].path != "/api/v1/subjects/sub01" {
				t.Fatalf("requests = %+v, want the subject resolved before the opinion", seen)
			}
			if seen[1].path != tc.path || seen[1].method != tc.method {
				t.Errorf("request = %s %s, want %s %s", seen[1].method, seen[1].path, tc.method, tc.path)
			}
			if seen[1].body["face_index"] != float64(2) || seen[1].body["photo_uid"] != "pht01" {
				t.Errorf("body = %v, want the whole opinion", seen[1].body)
			}
			if !strings.Contains(out, tc.phrase) {
				t.Errorf("confirmation = %q, want it to contain %q", out, tc.phrase)
			}
		})
	}
}

// TestCtlFacesReject_llm verifies the 204-only opinion still answers in the agent
// format, since there are no server bytes to pass through.
func TestCtlFacesReject_llm(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/subjects/") {
			w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "-o", "llm",
		"faces", "reject", "pht01", "2", "sub01")
	if err != nil {
		t.Fatalf("faces reject -o llm returned %v", err)
	}
	var ack map[string]any
	if err := json.Unmarshal([]byte(out), &ack); err != nil {
		t.Fatalf("llm confirmation is not valid JSON: %v (%q)", err, out)
	}
	message, _ := ack["message"].(string)
	if ack["status"] != "ok" || !strings.Contains(message, "Anna (sub01)") {
		t.Errorf("llm confirmation = %v, want an ok status naming the person", ack)
	}
}

// TestCtlClusters_list verifies the cluster listing renders with its suggestion,
// totals the faces waiting to be named, and says how many groups the server is
// still preparing.
func TestCtlClusters_list(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(ctlClustersBody))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "clusters", "list")
	if err != nil {
		t.Fatalf("clusters list returned %v", err)
	}
	if gotPath != "/api/v1/faces/clusters" {
		t.Errorf("path = %q, want the clusters endpoint", gotPath)
	}
	for _, want := range []string{
		"UID", "clu01", "Anna (sub01) 0.280", "pht01 #0", "12 unnamed faces",
		"4 more groups are still being prepared",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cluster table does not contain %q:\n%s", want, out)
		}
	}
}

// TestCtlClusters_assign verifies naming a whole cluster reaches its endpoint and
// reports the person by name and how many markers it wrote.
func TestCtlClusters_assign(t *testing.T) {
	var seen recordedRequest
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = recordRequest(r)
		w.Write([]byte(`{"cluster_uid":"clu01","subject":{"uid":"sub07","name":"Cyril"},
			"markers":[{"uid":"mrk01"},{"uid":"mrk02"},{"uid":"mrk03"}]}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"clusters", "assign", "clu01", "--name", "Cyril")
	if err != nil {
		t.Fatalf("clusters assign returned %v", err)
	}
	if seen.path != "/api/v1/faces/clusters/clu01/assign" || seen.body["subject_name"] != "Cyril" {
		t.Fatalf("request = %+v, want the cluster assigned to Cyril by name", seen)
	}
	if !strings.Contains(out, "Cyril (sub07)") || !strings.Contains(out, "3 markers") {
		t.Errorf("confirmation = %q, want the person named and the markers counted", out)
	}
}

// TestCtlClusters_removeFace verifies the removal sends the face reference and
// reports what the cluster has left, including the case where it has nothing.
func TestCtlClusters_removeFace(t *testing.T) {
	var seen recordedRequest
	body := `{"cluster":{"uid":"clu01","size":11,"representative":{"photo_uid":"pht01","face_index":0},
		"examples":[]}}`
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = recordRequest(r)
		w.Write([]byte(body))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"clusters", "remove-face", "clu01", "pht09", "2")
	if err != nil {
		t.Fatalf("clusters remove-face returned %v", err)
	}
	if seen.path != "/api/v1/faces/clusters/clu01/remove-face" ||
		seen.body["photo_uid"] != "pht09" || seen.body["face_index"] != float64(2) {
		t.Fatalf("request = %+v, want face 2 of pht09 removed from clu01", seen)
	}
	if !strings.Contains(out, "now holds 11 faces") {
		t.Errorf("confirmation = %q, want the remaining size", out)
	}

	orphan := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"cluster":null}`))
	})
	out, err = runCtl(t, "", "ctl", "--ctl-config", orphan,
		"clusters", "remove-face", "clu01", "pht09", "2")
	if err != nil {
		t.Fatalf("clusters remove-face returned %v", err)
	}
	if !strings.Contains(out, "was removed") {
		t.Errorf("confirmation = %q, want it to say the cluster is gone", out)
	}
}

// TestCtlFaces_badFaceIndex verifies a face argument that is not a number is
// refused before any request, on both trees that take one.
func TestCtlFaces_badFaceIndex(t *testing.T) {
	var requests int
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Write([]byte(ctlFacesBody))
	})

	for _, args := range [][]string{
		{"faces", "assign", "pht01", "first", "sub01"},
		{"clusters", "remove-face", "clu01", "pht01", "first"},
	} {
		full := append([]string{"ctl", "--ctl-config", configPath}, args...)
		if _, err := runCtl(t, "", full...); err == nil {
			t.Errorf("%v with a non-numeric face index succeeded", args)
		}
	}
	if requests != 0 {
		t.Errorf("%d requests were made, want none", requests)
	}
}
