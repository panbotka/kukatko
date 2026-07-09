package ctl

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

// labelsBody is a realistic bare {"labels": […]} envelope: a third shape next to
// the /photos paging envelope and the /albums list, hence a third decoder.
const labelsBody = `{"labels":[
	{"uid":"lbl01","slug":"lake","name":"lake","priority":10,"photo_count":42},
	{"uid":"lbl02","slug":"dog","name":"dog","priority":0,"photo_count":7}
]}`

// TestClient_ListLabels verifies the bare label envelope decodes with its own
// decoder, priorities and photo counts included.
func TestClient_ListLabels(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(labelsBody))
	})

	raw, err := client.ListLabels(t.Context())
	if err != nil {
		t.Fatalf("ListLabels returned %v", err)
	}
	if gotPath != "/api/v1/labels" {
		t.Errorf("path = %q, want /api/v1/labels", gotPath)
	}
	labels, err := DecodeLabels(raw)
	if err != nil {
		t.Fatalf("DecodeLabels returned %v", err)
	}
	if len(labels) != 2 {
		t.Fatalf("labels = %+v, want two rows", labels)
	}
	if labels[0].UID != "lbl01" || labels[0].Priority != 10 || labels[0].PhotoCount != 42 {
		t.Errorf("first label = %+v, want the decoded lake label", labels[0])
	}
}

// TestClient_ListLabels_empty verifies an empty library decodes to no labels.
func TestClient_ListLabels_empty(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"labels":[]}`))
	})
	raw, err := client.ListLabels(t.Context())
	if err != nil {
		t.Fatalf("ListLabels returned %v", err)
	}
	labels, err := DecodeLabels(raw)
	if err != nil || len(labels) != 0 {
		t.Errorf("DecodeLabels = %+v, %v, want an empty list", labels, err)
	}
}

// TestClient_GetLabel verifies the uid is escaped into the path and one label
// decodes.
func TestClient_GetLabel(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"uid":"lbl01","slug":"lake","name":"lake","priority":10}`))
	})

	raw, err := client.GetLabel(t.Context(), "lbl 01")
	if err != nil {
		t.Fatalf("GetLabel returned %v", err)
	}
	if gotPath != "/api/v1/labels/lbl 01" {
		t.Errorf("path = %q, want the escaped uid", gotPath)
	}
	label, err := DecodeLabel(raw)
	if err != nil || label.Name != "lake" || label.Priority != 10 {
		t.Errorf("DecodeLabel = %+v, %v, want the lake label", label, err)
	}
}

// TestClient_GetLabel_emptyUID verifies a blank uid never reaches the network.
func TestClient_GetLabel_emptyUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with a blank uid")
	})
	if _, err := client.GetLabel(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("GetLabel(\"\") error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_CreateLabel verifies the name reaches the body and the created label
// decodes back with its generated uid.
func TestClient_CreateLabel(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"lbl09","slug":"lake","name":"lake","priority":3}`))
	})

	raw, err := client.CreateLabel(t.Context(), LabelInput{Name: "lake", Priority: 3})
	if err != nil {
		t.Fatalf("CreateLabel returned %v", err)
	}
	if gotBody["name"] != "lake" || gotBody["priority"] != float64(3) {
		t.Errorf("body = %v, want the name and the priority", gotBody)
	}
	label, err := DecodeLabel(raw)
	if err != nil || label.UID != "lbl09" {
		t.Errorf("DecodeLabel = %+v, %v, want the created label", label, err)
	}
}

// TestClient_CreateLabel_emptyName verifies a blank name is rejected client-side.
func TestClient_CreateLabel_emptyName(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with a blank name")
	})
	if _, err := client.CreateLabel(t.Context(), LabelInput{Name: " "}); !errors.Is(err, ErrEmptyName) {
		t.Errorf("blank name error = %v, want ErrEmptyName", err)
	}
}

// TestClient_AttachLabel verifies the attach call posts the photo, the source and
// the uncertainty, and that the endpoint's 204 yields no error and no body.
func TestClient_AttachLabel(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.AttachLabel(t.Context(), "lbl01", "pht01", SourceAI, 20); err != nil {
		t.Fatalf("AttachLabel returned %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/labels/lbl01/photos" {
		t.Errorf("request = %s %s, want POST on the label photos path", gotMethod, gotPath)
	}
	if gotBody["photo_uid"] != "pht01" || gotBody["source"] != SourceAI || gotBody["uncertainty"] != float64(20) {
		t.Errorf("body = %v, want the photo, the source and the uncertainty", gotBody)
	}
}

// TestClient_AttachLabel_defaultSource verifies a blank source is omitted from the
// wire, so the server applies its own "manual" default rather than reading "".
func TestClient_AttachLabel_defaultSource(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.AttachLabel(t.Context(), "lbl01", "pht01", "", 0); err != nil {
		t.Fatalf("AttachLabel returned %v", err)
	}
	if _, set := gotBody["source"]; set {
		t.Errorf("body = %v, want no source so the server defaults it to manual", gotBody)
	}
}

// TestClient_AttachLabel_invalid verifies blank uids and an unknown source are all
// caught before a request is made.
func TestClient_AttachLabel_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if err := client.AttachLabel(t.Context(), "", "pht01", "", 0); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank label uid error = %v, want ErrEmptyUID", err)
	}
	if err := client.AttachLabel(t.Context(), "lbl01", "", "", 0); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank photo uid error = %v, want ErrEmptyUID", err)
	}
	err := client.AttachLabel(t.Context(), "lbl01", "pht01", "guessed", 0)
	if !errors.Is(err, ErrInvalidLabelSource) {
		t.Errorf("unknown source error = %v, want ErrInvalidLabelSource", err)
	}
}

// TestClient_DetachLabel verifies the detach call uses DELETE on the same path and
// names only the photo.
func TestClient_DetachLabel(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.DetachLabel(t.Context(), "lbl01", "pht01"); err != nil {
		t.Fatalf("DetachLabel returned %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotBody["photo_uid"] != "pht01" {
		t.Errorf("body = %v, want just the photo uid", gotBody)
	}
}
