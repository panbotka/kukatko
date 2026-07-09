package ctl

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestWriteAck verifies a 204 mutation confirms as one prose line in table form
// and as a small synthesized object in JSON form — there are no server bytes to
// pass through, and a piped consumer still needs to tell success from failure.
func TestWriteAck(t *testing.T) {
	t.Parallel()

	var table strings.Builder
	if err := WriteAck(&table, FormatTable, "photo pht01 favorited"); err != nil {
		t.Fatalf("WriteAck(table) returned %v", err)
	}
	if table.String() != "photo pht01 favorited\n" {
		t.Errorf("table ack = %q, want the bare message", table.String())
	}

	var raw strings.Builder
	if err := WriteAck(&raw, FormatJSON, "photo pht01 favorited"); err != nil {
		t.Fatalf("WriteAck(json) returned %v", err)
	}
	var ack Ack
	if err := json.Unmarshal([]byte(raw.String()), &ack); err != nil {
		t.Fatalf("json ack %q does not parse: %v", raw.String(), err)
	}
	if ack.Status != "ok" || ack.Message != "photo pht01 favorited" {
		t.Errorf("json ack = %+v, want an ok status and the message", ack)
	}
}

// TestWriteBulkResult verifies a clean batch prints only its summary, while a
// batch with failures lists the photos that failed — and only those.
func TestWriteBulkResult(t *testing.T) {
	t.Parallel()

	var clean strings.Builder
	err := WriteBulkResult(&clean, BulkResult{
		Results: []BulkPhotoResult{{PhotoUID: "pht01", Status: "updated"}},
		Counts:  BulkCounts{Total: 1, Updated: 1},
	})
	if err != nil {
		t.Fatalf("WriteBulkResult returned %v", err)
	}
	if got := clean.String(); got != "1 photo · 1 updated · 0 skipped · 0 errored\n" {
		t.Errorf("clean batch = %q, want only the summary line", got)
	}

	var failed strings.Builder
	err = WriteBulkResult(&failed, BulkResult{
		Results: []BulkPhotoResult{
			{PhotoUID: "pht01", Status: "updated"},
			{PhotoUID: "pht02", Status: "error", Error: "photo not found"},
		},
		Counts: BulkCounts{Total: 2, Updated: 1, Errored: 1},
	})
	if err != nil {
		t.Fatalf("WriteBulkResult returned %v", err)
	}
	out := failed.String()
	for _, want := range []string{"UID", "ERROR", "pht02", "photo not found", "2 photos", "1 errored"} {
		if !strings.Contains(out, want) {
			t.Errorf("failed batch does not contain %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pht01") {
		t.Errorf("failed batch lists the photos that succeeded:\n%s", out)
	}
}

// TestWriteMembership verifies the compact one-line membership summary, singular
// included — the full order is what -o json is for.
func TestWriteMembership(t *testing.T) {
	t.Parallel()

	tests := map[int]string{
		0: "album alb01 now holds 0 photos\n",
		1: "album alb01 now holds 1 photo\n",
		3: "album alb01 now holds 3 photos\n",
	}
	for count, want := range tests {
		var out strings.Builder
		if err := WriteMembership(&out, "alb01", make([]string, count)); err != nil {
			t.Fatalf("WriteMembership returned %v", err)
		}
		if out.String() != want {
			t.Errorf("WriteMembership(%d) = %q, want %q", count, out.String(), want)
		}
	}
}

// TestWriteResourceLists_empty verifies each empty list prints one line and no
// header, matching what an empty photo page does.
func TestWriteResourceLists_empty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		write func(*strings.Builder) error
		want  string
	}{
		{"albums", func(w *strings.Builder) error { return WriteAlbums(w, nil) }, "no albums found\n"},
		{"labels", func(w *strings.Builder) error { return WriteLabels(w, nil) }, "no labels found\n"},
		{"subjects", func(w *strings.Builder) error { return WriteSubjects(w, nil) }, "no subjects found\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			if err := tt.write(&out); err != nil {
				t.Fatalf("write returned %v", err)
			}
			if out.String() != tt.want {
				t.Errorf("empty %s = %q, want %q", tt.name, out.String(), tt.want)
			}
		})
	}
}

// TestWriteAlbum_nullableFields verifies an unset cover renders as a dash rather
// than as an empty cell or a nil dereference.
func TestWriteAlbum_nullableFields(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	if err := WriteAlbum(&out, Album{UID: "alb01", Title: "Trip"}); err != nil {
		t.Fatalf("WriteAlbum returned %v", err)
	}
	if !strings.Contains(out.String(), "COVER") || !strings.Contains(out.String(), "-") {
		t.Errorf("album detail does not dash an unset cover:\n%s", out.String())
	}

	cover := "pht09"
	out.Reset()
	if err := WriteAlbum(&out, Album{UID: "alb01", CoverPhotoUID: &cover}); err != nil {
		t.Fatalf("WriteAlbum returned %v", err)
	}
	if !strings.Contains(out.String(), "pht09") {
		t.Errorf("album detail drops a set cover:\n%s", out.String())
	}
}
