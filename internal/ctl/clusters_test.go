package ctl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

// clustersBody is a realistic GET /faces/clusters body: one group the server has a
// guess about and one it has none for, which is why suggestion is a pointer.
const clustersBody = `{"clusters":[
	{"uid":"clu01","size":12,
	 "representative":{"photo_uid":"pht01","face_index":0,"bbox":[0.1,0.2,0.3,0.4],"det_score":0.94},
	 "examples":[{"photo_uid":"pht02","face_index":1,"bbox":[0.2,0.2,0.2,0.2],"det_score":0.9}],
	 "suggestion":{"subject_uid":"sub01","subject_name":"Anna","distance":0.28,"confidence":0.72},
	 "created_at":"2026-08-01T10:00:00Z"},
	{"uid":"clu02","size":3,
	 "representative":{"photo_uid":"pht09","face_index":2,"bbox":[0,0,0.1,0.1],"det_score":0.71},
	 "examples":[],"created_at":"2026-08-02T10:00:00Z"}
]}`

// TestClient_ListClusters verifies the cluster listing reaches its path and
// decodes, keeping the guess a pointer so "no idea" stays distinguishable.
func TestClient_ListClusters(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Write([]byte(clustersBody))
	})

	raw, err := client.ListClusters(t.Context())
	if err != nil {
		t.Fatalf("ListClusters returned %v", err)
	}
	if gotPath != "/api/v1/faces/clusters" || gotMethod != http.MethodGet {
		t.Errorf("request = %s %s, want GET /api/v1/faces/clusters", gotMethod, gotPath)
	}
	clusters, err := DecodeClusters(raw)
	if err != nil {
		t.Fatalf("DecodeClusters returned %v", err)
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters = %+v, want two groups", clusters)
	}
	if clusters[0].Size != 12 || clusters[0].Representative.PhotoUID != "pht01" {
		t.Errorf("first cluster = %+v, want twelve faces represented by pht01", clusters[0])
	}
	if clusters[0].Suggestion == nil || clusters[0].Suggestion.SubjectName != "Anna" {
		t.Errorf("first suggestion = %+v, want Anna", clusters[0].Suggestion)
	}
	if clusters[1].Suggestion != nil {
		t.Errorf("second suggestion = %+v, want nil when nobody was close enough", clusters[1].Suggestion)
	}
}

// TestClient_AssignCluster verifies naming a whole cluster reaches the assign
// endpoint with only the half of the subject reference that was given.
func TestClient_AssignCluster(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Write([]byte(`{"cluster_uid":"clu01","subject":{"uid":"sub07","name":"Cyril","type":"person"},
			"markers":[{"uid":"mrk01","photo_uid":"pht01","type":"face"},
			           {"uid":"mrk02","photo_uid":"pht02","type":"face"}]}`))
	})

	raw, err := client.AssignCluster(t.Context(), "clu01", SubjectRef{Name: "Cyril"})
	if err != nil {
		t.Fatalf("AssignCluster returned %v", err)
	}
	if gotPath != "/api/v1/faces/clusters/clu01/assign" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want the cluster assign endpoint", gotMethod, gotPath)
	}
	if gotBody["subject_name"] != "Cyril" {
		t.Errorf("body = %v, want the name sent", gotBody)
	}
	if _, present := gotBody["subject_uid"]; present {
		t.Errorf("body = %v, want no empty subject_uid alongside the name", gotBody)
	}
	result, err := DecodeClusterAssign(raw)
	if err != nil {
		t.Fatalf("DecodeClusterAssign returned %v", err)
	}
	if result.Subject.Name != "Cyril" || len(result.Markers) != 2 {
		t.Errorf("assign result = %+v, want Cyril and two markers", result)
	}
}

// TestClient_RemoveClusterFace verifies the removal reaches its path with the face
// reference, and that both outcomes — a smaller cluster and a vanished one —
// decode distinguishably.
func TestClient_RemoveClusterFace(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody map[string]any
	body := `{"cluster":{"uid":"clu01","size":11,
		"representative":{"photo_uid":"pht01","face_index":0,"bbox":[0,0,0.1,0.1]},"examples":[]}}`
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(body))
	})

	raw, err := client.RemoveClusterFace(t.Context(), "clu01", "pht09", 2)
	if err != nil {
		t.Fatalf("RemoveClusterFace returned %v", err)
	}
	if gotPath != "/api/v1/faces/clusters/clu01/remove-face" {
		t.Errorf("path = %q, want the remove-face endpoint", gotPath)
	}
	if gotBody["photo_uid"] != "pht09" || gotBody["face_index"] != float64(2) {
		t.Errorf("body = %v, want the face reference", gotBody)
	}
	cluster, err := DecodeClusterRemoval(raw)
	if err != nil || cluster == nil || cluster.Size != 11 {
		t.Fatalf("DecodeClusterRemoval = %+v, %v, want the smaller cluster", cluster, err)
	}
	orphaned, err := DecodeClusterRemoval(json.RawMessage(`{"cluster":null}`))
	if err != nil || orphaned != nil {
		t.Errorf("DecodeClusterRemoval(null) = %+v, %v, want nil for a consumed cluster", orphaned, err)
	}
}

// TestClient_RemoveClusterFace_invalid verifies the references the API would only
// answer with a 400 are refused locally.
func TestClient_RemoveClusterFace_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("an invalid removal reached the server")
	})
	if _, err := client.RemoveClusterFace(t.Context(), "", "pht01", 0); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank cluster = %v, want ErrEmptyUID", err)
	}
	if _, err := client.RemoveClusterFace(t.Context(), "clu01", "", 0); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank photo = %v, want ErrEmptyUID", err)
	}
	if _, err := client.RemoveClusterFace(t.Context(), "clu01", "pht01", -1); !errors.Is(err, ErrNegativeFaceIndex) {
		t.Errorf("negative face = %v, want ErrNegativeFaceIndex", err)
	}
}
