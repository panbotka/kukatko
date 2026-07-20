package importer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestSource_Valid checks that only the known sources are accepted.
func TestSource_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source Source
		want   bool
	}{
		{name: "photoprism is valid", source: SourcePhotoPrism, want: true},
		{name: "photosorter is valid", source: SourcePhotoSorter, want: true},
		{name: "photosorter feeds is valid", source: SourcePhotoSorterFeeds, want: true},
		{name: "folder is valid", source: SourceFolder, want: true},
		{name: "unknown is invalid", source: Source("flickr"), want: false},
		{name: "empty is invalid", source: Source(""), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.source.Valid(); got != tt.want {
				t.Errorf("Source(%q).Valid() = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

// TestCounts_JSONRoundTrip confirms the counts tally serialises with stable,
// lower-snake JSON keys (the on-disk JSONB shape) and decodes back unchanged.
func TestCounts_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	in := Counts{Imported: 3, Updated: 2, Skipped: 5, Failed: 1}
	encoded, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	const want = `{"imported":3,"updated":2,"skipped":5,"failed":1}`
	if string(encoded) != want {
		t.Errorf("Marshal(%+v) = %s, want %s", in, encoded, want)
	}

	var out Counts
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

// TestNewFailure checks the convenience constructor stamps the run and source and
// records the error message, tolerating a nil error.
func TestNewFailure(t *testing.T) {
	t.Parallel()

	withErr := NewFailure(7, SourcePhotoPrism, StagePhoto, "kk1", "pp1", "beach", errors.New("boom"))
	if withErr.RunID != 7 || withErr.Source != SourcePhotoPrism || withErr.Stage != StagePhoto {
		t.Errorf("NewFailure identity = %+v, want run 7 / photoprism / photo", withErr)
	}
	if withErr.PhotoUID != "kk1" || withErr.SourceRef != "pp1" || withErr.Detail != "beach" {
		t.Errorf("NewFailure descriptors = %+v", withErr)
	}
	if withErr.Error != "boom" {
		t.Errorf("NewFailure error = %q, want boom", withErr.Error)
	}
	if nilErr := NewFailure(1, SourceFolder, StageFile, "", "", "", nil); nilErr.Error != "" {
		t.Errorf("NewFailure(nil err).Error = %q, want empty", nilErr.Error)
	}
}

// TestBuildListFailuresQuery checks the WHERE clauses and the ordered argument
// list are assembled from the filter, with the limit and offset always last.
func TestBuildListFailuresQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     FailureFilter
		wantWheres []string
		wantArgs   []any
	}{
		{
			name:       "no filter: only limit and offset",
			filter:     FailureFilter{},
			wantWheres: nil,
			wantArgs:   []any{10, 5},
		},
		{
			name:       "run, source and unresolved",
			filter:     FailureFilter{RunID: 3, Source: SourcePhotoPrism, UnresolvedOnly: true},
			wantWheres: []string{"run_id = $1", "source = $2", "resolved_at IS NULL"},
			wantArgs:   []any{int64(3), "photoprism", 10, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sql, args := buildListFailuresQuery(tt.filter, 10, 5)
			for _, w := range tt.wantWheres {
				if !strings.Contains(sql, w) {
					t.Errorf("query %q missing clause %q", sql, w)
				}
			}
			if tt.wantWheres == nil && strings.Contains(sql, "WHERE") {
				t.Errorf("query %q has a WHERE for an empty filter", sql)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
			for i := range tt.wantArgs {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("arg[%d] = %v, want %v", i, args[i], tt.wantArgs[i])
				}
			}
			if !strings.Contains(sql, "ORDER BY created_at DESC, id DESC") {
				t.Errorf("query %q missing stable ordering", sql)
			}
		})
	}
}
