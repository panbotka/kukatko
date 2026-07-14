package importer

import (
	"encoding/json"
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
