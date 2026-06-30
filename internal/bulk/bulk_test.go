package bulk

import (
	"errors"
	"strings"
	"testing"
)

// TestOperations_IsEmpty verifies an operation set with no requested change is
// empty while any single change makes it non-empty.
func TestOperations_IsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ops  Operations
		want bool
	}{
		{"nothing set", Operations{}, true},
		{"add album", Operations{AddAlbums: []string{"al1"}}, false},
		{"empty album slice", Operations{AddAlbums: []string{}}, true},
		{"set title", Operations{Title: new("")}, false},
		{"clear location", Operations{ClearLocation: true}, false},
		{"archive", Operations{Archive: new(true)}, false},
		{"favorite", Operations{Favorite: new(false)}, false},
		{"rating", Operations{Rating: new(4)}, false},
		{"flag", Operations{Flag: new("pick")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.ops.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOperations_Summary verifies only requested operations appear in the audit
// summary, including the clear-location marker.
func TestOperations_Summary(t *testing.T) {
	t.Parallel()

	ops := Operations{
		AddAlbums:     []string{"al1"},
		RemoveLabels:  []string{"lb1"},
		Description:   new("hi"),
		ClearLocation: true,
		Private:       new(true),
		Rating:        new(5),
		Flag:          new("reject"),
	}
	summary := ops.Summary()
	for _, key := range []string{
		"add_albums", "remove_labels", "description", "clear_location", "private", "rating", "flag",
	} {
		if _, ok := summary[key]; !ok {
			t.Errorf("Summary() missing key %q in %v", key, summary)
		}
	}
	if _, ok := summary["title"]; ok {
		t.Errorf("Summary() unexpectedly included title: %v", summary)
	}
}

// TestOperations_photoColumnUpdate verifies the dynamic UPDATE is emitted only
// when a column-level change is requested and includes the expected columns and
// argument count.
func TestOperations_photoColumnUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		ops        Operations
		wantOK     bool
		wantArgs   int
		wantSubstr []string
	}{
		{"no column ops", Operations{AddAlbums: []string{"al1"}}, false, 0, nil},
		{
			name:       "title and private",
			ops:        Operations{Title: new("cap"), Private: new(true)},
			wantOK:     true,
			wantArgs:   3, // uid + title + private
			wantSubstr: []string{"title = $2", "private = $3"},
		},
		{
			name:       "set location",
			ops:        Operations{Location: &Location{Lat: 1, Lng: 2}},
			wantOK:     true,
			wantArgs:   3, // uid + lat + lng
			wantSubstr: []string{"lat = $2", "lng = $3"},
		},
		{
			name:       "clear location and archive",
			ops:        Operations{ClearLocation: true, Archive: new(true)},
			wantOK:     true,
			wantArgs:   1, // uid only; NULL/now() are literals
			wantSubstr: []string{"lat = NULL", "lng = NULL", "archived_at = now()"},
		},
		{
			name:       "unarchive",
			ops:        Operations{Archive: new(false)},
			wantOK:     true,
			wantArgs:   1,
			wantSubstr: []string{"archived_at = NULL"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			query, args, ok := tt.ops.photoColumnUpdate("ph1")
			if ok != tt.wantOK {
				t.Fatalf("photoColumnUpdate ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if len(args) != tt.wantArgs {
				t.Errorf("args count = %d, want %d (%v)", len(args), tt.wantArgs, args)
			}
			if args[0] != "ph1" {
				t.Errorf("args[0] = %v, want ph1", args[0])
			}
			for _, sub := range tt.wantSubstr {
				if !strings.Contains(query, sub) {
					t.Errorf("query %q missing %q", query, sub)
				}
			}
		})
	}
}

// TestService_validateBatch verifies the pre-transaction guards for an empty
// list, an oversized batch and an empty operation set.
func TestService_validateBatch(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, 2)
	ops := Operations{Archive: new(true)}
	tests := []struct {
		name    string
		uids    []string
		ops     Operations
		wantErr error
	}{
		{"ok", []string{"ph1"}, ops, nil},
		{"no photos", nil, ops, ErrNoPhotos},
		{"too large", []string{"ph1", "ph2", "ph3"}, ops, ErrBatchTooLarge},
		{"no operations", []string{"ph1"}, Operations{}, ErrNoOperations},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := svc.validateBatch(tt.uids, tt.ops)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateBatch() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestNewService_defaultBatch verifies a non-positive limit falls back to the
// default.
func TestNewService_defaultBatch(t *testing.T) {
	t.Parallel()

	if got := NewService(nil, 0).MaxBatch(); got != DefaultMaxBatchSize {
		t.Errorf("MaxBatch() = %d, want %d", got, DefaultMaxBatchSize)
	}
	if got := NewService(nil, 50).MaxBatch(); got != 50 {
		t.Errorf("MaxBatch() = %d, want 50", got)
	}
}

// TestResult_add verifies per-photo outcomes increment the matching counters.
func TestResult_add(t *testing.T) {
	t.Parallel()

	var r Result
	r.add("ph1", StatusUpdated, "")
	r.add("ph2", StatusSkipped, "dup")
	r.add("ph3", StatusError, "missing")
	r.add("ph4", StatusUpdated, "")
	if r.Counts.Updated != 2 || r.Counts.Skipped != 1 || r.Counts.Errored != 1 {
		t.Errorf("counts = %+v, want updated=2 skipped=1 errored=1", r.Counts)
	}
	if len(r.Results) != 4 {
		t.Errorf("results len = %d, want 4", len(r.Results))
	}
}
