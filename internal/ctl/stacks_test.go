package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// stackDetailBody is a photo detail as the stacking endpoints answer with it: the
// photo, its stack, and the whole variants strip.
const stackDetailBody = `{"uid":"pht01","stack_uid":"stk01","stack_members":[
	{"uid":"pht01","file_name":"DSC_1.JPG","media_type":"image","file_mime":"image/jpeg",
	 "file_width":6000,"file_height":4000,"file_size":5242880,"is_primary":true},
	{"uid":"pht02","file_name":"DSC_1.NEF","media_type":"image","file_mime":"image/x-nikon-nef",
	 "file_width":6000,"file_height":4000,"file_size":26214400,"is_primary":false}
]}`

// TestClient_StackPhotos verifies the selection goes in one body, is de-duplicated
// first, and that the resulting group decodes out of the photo detail the API
// answers with.
func TestClient_StackPhotos(t *testing.T) {
	t.Parallel()

	var requests int
	var gotMethod, gotPath string
	var gotBody struct {
		PhotoUIDs []string `json:"photo_uids"`
	}
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(stackDetailBody))
	})

	raw, err := client.StackPhotos(t.Context(), []string{"pht01", "pht02", "pht01"})
	if err != nil {
		t.Fatalf("StackPhotos returned %v", err)
	}
	if requests != 1 || gotMethod != http.MethodPost || gotPath != "/api/v1/photos/stack" {
		t.Errorf("request = %d× %s %s, want one POST /api/v1/photos/stack", requests, gotMethod, gotPath)
	}
	if len(gotBody.PhotoUIDs) != 2 {
		t.Errorf("body photo_uids = %v, want the repeat dropped", gotBody.PhotoUIDs)
	}
	stack, err := DecodeStack(raw)
	if err != nil {
		t.Fatalf("DecodeStack returned %v", err)
	}
	if !stack.Stacked() || *stack.StackUID != "stk01" || len(stack.Members) != 2 {
		t.Errorf("stack = %+v, want the two-member group", stack)
	}
	if !stack.Members[0].IsPrimary || stack.Members[1].IsPrimary {
		t.Errorf("members = %+v, want only the first marked primary", stack.Members)
	}
}

// TestClient_StackPhotos_tooSmall verifies a group of one is refused before a
// request is spent on a guaranteed 400 — a stack of one is just a photo.
func TestClient_StackPhotos_tooSmall(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite too small a selection")
	})
	if _, err := client.StackPhotos(t.Context(), []string{"pht01"}); !errors.Is(err, ErrStackTooSmall) {
		t.Errorf("one photo error = %v, want ErrStackTooSmall", err)
	}
	// A repeated uid is one photo twice, not two photos.
	_, err := client.StackPhotos(t.Context(), []string{"pht01", "pht01"})
	if !errors.Is(err, ErrStackTooSmall) {
		t.Errorf("repeated uid error = %v, want ErrStackTooSmall", err)
	}
	if _, err := client.StackPhotos(t.Context(), nil); !errors.Is(err, ErrNoPhotoUIDs) {
		t.Errorf("empty selection error = %v, want ErrNoPhotoUIDs", err)
	}
}

// TestClient_stackMutations verifies the three per-photo stacking calls hit the
// paths the API mounts them on, with no body.
func TestClient_stackMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		call     func(*Client) (json.RawMessage, error)
		wantPath string
	}{
		{
			name:     "set primary",
			wantPath: "/api/v1/photos/pht 01/stack/primary",
			call:     func(c *Client) (json.RawMessage, error) { return c.SetStackPrimary(t.Context(), "pht 01") },
		},
		{
			name:     "unstack one",
			wantPath: "/api/v1/photos/pht 01/unstack",
			call:     func(c *Client) (json.RawMessage, error) { return c.UnstackPhoto(t.Context(), "pht 01") },
		},
		{
			name:     "unstack all",
			wantPath: "/api/v1/photos/pht 01/unstack-all",
			call:     func(c *Client) (json.RawMessage, error) { return c.UnstackAll(t.Context(), "pht 01") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.Write([]byte(stackDetailBody))
			})
			if _, err := tt.call(client); err != nil {
				t.Fatalf("stack mutation returned %v", err)
			}
			if gotMethod != http.MethodPost || gotPath != tt.wantPath {
				t.Errorf("request = %s %s, want POST %s", gotMethod, gotPath, tt.wantPath)
			}
		})
	}
}

// TestClient_stackMutations_emptyUID verifies a blank uid never reaches the wire.
func TestClient_stackMutations_emptyUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with a blank uid")
	})
	if _, err := client.UnstackPhoto(t.Context(), " "); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("UnstackPhoto(\" \") error = %v, want ErrEmptyUID", err)
	}
}

// TestWriteStack verifies the table lists every member with its primary marked
// and says outright that the group keeps the files apart.
func TestWriteStack(t *testing.T) {
	t.Parallel()

	stack, err := DecodeStack([]byte(stackDetailBody))
	if err != nil {
		t.Fatalf("DecodeStack returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteStack(&buf, stack); err != nil {
		t.Fatalf("WriteStack returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{"pht01", "pht02", "DSC_1.NEF", "stack stk01 groups 2 photos", "true"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}
}

// TestWriteStack_unstacked verifies an ungrouped photo prints the one line that
// is the whole result of an unstack, not an empty table.
func TestWriteStack_unstacked(t *testing.T) {
	t.Parallel()

	stack, err := DecodeStack([]byte(`{"uid":"pht01"}`))
	if err != nil {
		t.Fatalf("DecodeStack returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteStack(&buf, stack); err != nil {
		t.Fatalf("WriteStack returned %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "photo pht01 is not stacked" {
		t.Errorf("output = %q, want the not-stacked line", got)
	}
}

// TestDecodeStack_invalid verifies malformed JSON surfaces as an error.
func TestDecodeStack_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeStack([]byte(`{"uid":`)); err == nil {
		t.Error("DecodeStack of malformed JSON returned no error")
	}
}
