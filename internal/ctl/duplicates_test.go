package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// duplicatesBody is a realistic page of duplicate groups, with both detectors'
// distances on the non-keeper.
const duplicatesBody = `{"groups":[
	{"id":"pht01","reason":"phash","keeper_uid":"pht01","confirmed":true,"members":[
		{"uid":"pht01","file_name":"a.jpg","file_size":2097152,"is_keeper":true},
		{"uid":"pht02","file_name":"a-copy.jpg","file_size":2097100,"is_keeper":false,
		 "phash_distance":2,"embedding_distance":0.0134}
	]}
],"total":1,"limit":20,"offset":0,"next_offset":null}`

// TestClient_ListDuplicates verifies the path and the paging parameters, and that
// the page decodes with its groups and members.
func TestClient_ListDuplicates(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Write([]byte(duplicatesBody))
	})

	raw, err := client.ListDuplicates(t.Context(), PageOptions{Limit: 20, Offset: 40})
	if err != nil {
		t.Fatalf("ListDuplicates returned %v", err)
	}
	if gotPath != "/api/v1/duplicates" || !strings.Contains(gotQuery, "limit=20") ||
		!strings.Contains(gotQuery, "offset=40") {
		t.Errorf("request = %s?%s, want the duplicates path with paging", gotPath, gotQuery)
	}
	page, err := DecodeDuplicates(raw)
	if err != nil {
		t.Fatalf("DecodeDuplicates returned %v", err)
	}
	if len(page.Groups) != 1 || len(page.Groups[0].Members) != 2 || !page.Groups[0].Confirmed {
		t.Errorf("page = %+v, want the one confirmed two-member group", page)
	}
}

// TestClient_duplicateFeedback verifies each opinion hits its own endpoint with
// the verb that distinguishes recording it from taking it back, and that the pair
// travels in the body.
func TestClient_duplicateFeedback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		call       func(*Client) error
		wantMethod string
		wantPath   string
	}{
		{
			name:       "confirm",
			wantMethod: http.MethodPost,
			wantPath:   "/api/v1/feedback/duplicate-confirmations",
			call:       func(c *Client) error { return c.ConfirmDuplicate(t.Context(), "pht01", "pht02") },
		},
		{
			name:       "unconfirm",
			wantMethod: http.MethodDelete,
			wantPath:   "/api/v1/feedback/duplicate-confirmations",
			call:       func(c *Client) error { return c.UnconfirmDuplicate(t.Context(), "pht01", "pht02") },
		},
		{
			name:       "dismiss",
			wantMethod: http.MethodPost,
			wantPath:   "/api/v1/feedback/duplicate-dismissals",
			call:       func(c *Client) error { return c.DismissDuplicate(t.Context(), "pht01", "pht02") },
		},
		{
			name:       "undismiss",
			wantMethod: http.MethodDelete,
			wantPath:   "/api/v1/feedback/duplicate-dismissals",
			call:       func(c *Client) error { return c.UndismissDuplicate(t.Context(), "pht01", "pht02") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			var gotBody map[string]string
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				json.NewDecoder(r.Body).Decode(&gotBody)
				w.WriteHeader(http.StatusNoContent)
			})
			if err := tt.call(client); err != nil {
				t.Fatalf("%s returned %v", tt.name, err)
			}
			if gotMethod != tt.wantMethod || gotPath != tt.wantPath {
				t.Errorf("request = %s %s, want %s %s", gotMethod, gotPath, tt.wantMethod, tt.wantPath)
			}
			if gotBody["photo_uid"] != "pht01" || gotBody["other_uid"] != "pht02" {
				t.Errorf("body = %v, want the pair", gotBody)
			}
		})
	}
}

// TestClient_duplicateFeedback_invalid verifies a pair naming one photo twice —
// which the API answers 400 for — is caught locally, as is a blank uid.
func TestClient_duplicateFeedback_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if err := client.ConfirmDuplicate(t.Context(), "pht01", "pht01"); !errors.Is(err, ErrSameDuplicatePhoto) {
		t.Errorf("same-photo error = %v, want ErrSameDuplicatePhoto", err)
	}
	if err := client.DismissDuplicate(t.Context(), "pht01", " "); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
}

// TestWriteDuplicates verifies one row per member — the uids confirm and dismiss
// take — and that each distance says which detector measured it.
func TestWriteDuplicates(t *testing.T) {
	t.Parallel()

	page, err := DecodeDuplicates([]byte(duplicatesBody))
	if err != nil {
		t.Fatalf("DecodeDuplicates returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteDuplicates(&buf, page); err != nil {
		t.Fatalf("WriteDuplicates returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{"pht01", "pht02", "phash 2", "cos 0.013", "1 of 1 groups"} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}

	buf.Reset()
	if err := WriteDuplicates(&buf, DuplicatePage{}); err != nil {
		t.Fatalf("WriteDuplicates returned %v", err)
	}
	if strings.TrimSpace(buf.String()) != "no duplicate groups found" {
		t.Errorf("empty output = %q, want the one-line message", buf.String())
	}
}

// TestDecodeDuplicates_invalid verifies malformed JSON surfaces as an error.
func TestDecodeDuplicates_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeDuplicates([]byte(`{"groups":`)); err == nil {
		t.Error("DecodeDuplicates of malformed JSON returned no error")
	}
}
