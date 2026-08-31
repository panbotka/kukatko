package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// mergeBody is a realistic answer of POST /duplicates/merge.
const mergeBody = `{"keeper_uid":"pht01","albums_added":2,"labels_added":1,"people_added":3,
	"metadata_filled":["title","taken_at"],"archived":2,"dry_run":false}`

// TestMergeInput verifies the local checks: the keeper is folded into its own
// group, the set is de-duplicated, and a group of one is refused.
func TestMergeInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		keeper      string
		others      []string
		wantErr     error
		wantMembers string
	}{
		{
			name: "keeper joins its own group", keeper: "pht01", others: []string{"pht02", "pht03"},
			wantMembers: "pht01,pht02,pht03",
		},
		{
			name: "a repeated member is one member", keeper: "pht01", others: []string{"pht02", "pht02", "pht01"},
			wantMembers: "pht01,pht02",
		},
		{name: "no keeper", keeper: "  ", others: []string{"pht02"}, wantErr: ErrEmptyUID},
		{name: "nobody to merge", keeper: "pht01", others: nil, wantErr: ErrNoMergeMembers},
		{name: "only the keeper again", keeper: "pht01", others: []string{"pht01"}, wantErr: ErrNoMergeMembers},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in, err := mergeInput(tt.keeper, tt.others, false)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("mergeInput error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := strings.Join(in.MemberUIDs, ","); got != tt.wantMembers {
				t.Errorf("members = %q, want %q", got, tt.wantMembers)
			}
			if in.KeeperUID != tt.keeper {
				t.Errorf("keeper = %q, want %q", in.KeeperUID, tt.keeper)
			}
		})
	}
}

// TestClient_MergeGroup verifies the whole group travels in one request and that
// a dry run asks the server for its own preview rather than guessing at one.
func TestClient_MergeGroup(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotBody MergeInput
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decoding the merge body: %v", err)
		}
		w.Write([]byte(mergeBody))
	})

	raw, err := client.MergeGroup(t.Context(), "pht01", []string{"pht02", "pht03"}, true)
	if err != nil {
		t.Fatalf("MergeGroup returned %v", err)
	}
	if gotPath != "/api/v1/duplicates/merge" {
		t.Errorf("path = %q, want the merge endpoint", gotPath)
	}
	if gotBody.KeeperUID != "pht01" || len(gotBody.MemberUIDs) != 3 || !gotBody.DryRun {
		t.Errorf("body = %+v, want the whole group previewed", gotBody)
	}
	result, err := DecodeDuplicateMerge(raw)
	if err != nil {
		t.Fatalf("DecodeDuplicateMerge returned %v", err)
	}
	if result.Archived != 2 || result.KeeperUID != "pht01" {
		t.Errorf("result = %+v, want the two archived copies", result)
	}
}

// TestClient_MergeGroup_refusedLocally verifies a merge that names nobody to
// merge never reaches the server.
func TestClient_MergeGroup_refusedLocally(t *testing.T) {
	t.Parallel()

	called := false
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(mergeBody))
	})

	if _, err := client.MergeGroup(t.Context(), "pht01", nil, false); !errors.Is(err, ErrNoMergeMembers) {
		t.Fatalf("MergeGroup error = %v, want ErrNoMergeMembers", err)
	}
	if called {
		t.Error("a merge with nothing to merge still reached the server")
	}
}

// TestWriteDuplicateMerge verifies the result says what moved, how many copies
// were archived, and — crucially — that they are in the trash rather than gone.
func TestWriteDuplicateMerge(t *testing.T) {
	t.Parallel()

	result, err := DecodeDuplicateMerge([]byte(mergeBody))
	if err != nil {
		t.Fatalf("DecodeDuplicateMerge returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteDuplicateMerge(&buf, result); err != nil {
		t.Fatalf("WriteDuplicateMerge returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{"pht01", "2 copies", "title, taken_at", "in the trash, not deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("merge result is missing %q:\n%s", want, out)
		}
	}

	buf.Reset()
	result.DryRun = true
	if err := WriteDuplicateMerge(&buf, result); err != nil {
		t.Fatalf("WriteDuplicateMerge returned %v", err)
	}
	if !strings.Contains(buf.String(), "dry run: nothing was merged") {
		t.Errorf("dry run result = %q, want it marked as a rehearsal", buf.String())
	}
}
