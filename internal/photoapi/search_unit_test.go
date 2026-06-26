package photoapi

import (
	"reflect"
	"testing"

	"github.com/panbotka/kukatko/internal/photos"
)

// TestParseSearchMode_validation checks the mode aliases, the hybrid default for
// an empty value and the error for an unknown mode.
func TestParseSearchMode_validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    searchMode
		wantErr bool
	}{
		{name: "empty defaults to hybrid", raw: "", want: modeHybrid},
		{name: "fulltext", raw: "fulltext", want: modeFulltext},
		{name: "semantic", raw: "semantic", want: modeSemantic},
		{name: "hybrid", raw: "hybrid", want: modeHybrid},
		{name: "unknown is error", raw: "fuzzy", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseSearchMode(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseSearchMode(%q) error = nil, want error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseSearchMode(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("parseSearchMode(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// TestFuseRRF_ordering verifies the fused order rewards items ranked highly in
// both lists, that a single list is honoured, and that ties break by descending
// uid.
func TestFuseRRF_ordering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		lists [][]string
		want  []string
	}{
		{
			name:  "single list keeps its order",
			lists: [][]string{{"a", "b", "c"}},
			want:  []string{"a", "b", "c"},
		},
		{
			name: "item ranked highly in both wins",
			// b is 2nd then 1st; a is 1st then 3rd. b's combined score
			// (1/62 + 1/61) exceeds a's (1/61 + 1/63), so b leads.
			lists: [][]string{{"a", "b", "c"}, {"b", "c", "a"}},
			want:  []string{"b", "a", "c"},
		},
		{
			name:  "de-duplicates across lists",
			lists: [][]string{{"x", "y"}, {"x", "y"}},
			want:  []string{"x", "y"},
		},
		{
			name: "equal scores break by descending uid",
			// a and b each appear once at rank 1 in separate lists → equal score.
			lists: [][]string{{"a"}, {"b"}},
			want:  []string{"b", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fuseRRF(tt.lists...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fuseRRF(%v) = %v, want %v", tt.lists, got, tt.want)
			}
		})
	}
}

// TestFuse_combinesListsAndLookup verifies fuse builds the fused order from the
// full-text and semantic rankings and a lookup spanning both inputs.
func TestFuse_combinesListsAndLookup(t *testing.T) {
	t.Parallel()

	ftList := []photos.Photo{{UID: "a"}, {UID: "b"}}
	semUIDs := []string{"b", "c"}
	semByUID := map[string]photos.Photo{"b": {UID: "b"}, "c": {UID: "c"}}

	order, byUID := fuse(ftList, semUIDs, semByUID)

	// b ranks in both lists, so it leads; a and c follow.
	if len(order) == 0 || order[0] != "b" {
		t.Fatalf("fuse order = %v, want b first", order)
	}
	for _, uid := range []string{"a", "b", "c"} {
		if _, ok := byUID[uid]; !ok {
			t.Errorf("fuse lookup missing %q", uid)
		}
	}
	if len(byUID) != 3 {
		t.Errorf("fuse lookup size = %d, want 3", len(byUID))
	}
}

// TestPaginateUIDs_window checks the offset/limit window is clamped to the slice
// bounds and that an offset past the end yields nil.
func TestPaginateUIDs_window(t *testing.T) {
	t.Parallel()

	all := []string{"a", "b", "c", "d", "e"}
	tests := []struct {
		name   string
		offset int
		limit  int
		want   []string
	}{
		{name: "first page", offset: 0, limit: 2, want: []string{"a", "b"}},
		{name: "middle page", offset: 2, limit: 2, want: []string{"c", "d"}},
		{name: "limit overruns end", offset: 4, limit: 10, want: []string{"e"}},
		{name: "offset past end is nil", offset: 5, limit: 2, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := paginateUIDs(all, tt.offset, tt.limit)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paginateUIDs(offset=%d, limit=%d) = %v, want %v",
					tt.offset, tt.limit, got, tt.want)
			}
		})
	}
}

// TestResolvePhotos_orderAndSkip verifies resolvePhotos preserves uid order and
// skips uids absent from the lookup.
func TestResolvePhotos_orderAndSkip(t *testing.T) {
	t.Parallel()

	byUID := map[string]photos.Photo{
		"a": {UID: "a", Title: "Alpha"},
		"c": {UID: "c", Title: "Gamma"},
	}
	got := resolvePhotos([]string{"c", "b", "a"}, byUID)
	if len(got) != 2 || got[0].UID != "c" || got[1].UID != "a" {
		t.Fatalf("resolvePhotos = %v, want [c a] in order", uidsOf(got))
	}
}

// TestEffectiveLimit_default checks the default substitutes for an unset limit
// and an explicit limit is returned unchanged.
func TestEffectiveLimit_default(t *testing.T) {
	t.Parallel()

	if got := effectiveLimit(photos.ListParams{Limit: 0}); got != defaultPageLimit {
		t.Errorf("effectiveLimit(0) = %d, want %d", got, defaultPageLimit)
	}
	if got := effectiveLimit(photos.ListParams{Limit: 25}); got != 25 {
		t.Errorf("effectiveLimit(25) = %d, want 25", got)
	}
}

// uidsOf extracts the uids of photos for compact assertions.
func uidsOf(list []photos.Photo) []string {
	out := make([]string, len(list))
	for i, p := range list {
		out[i] = p.UID
	}
	return out
}
