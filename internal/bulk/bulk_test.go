package bulk

import (
	"errors"
	"strings"
	"testing"
	"time"
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
		{"hide", Operations{Hide: new(true)}, false},
		{"favorite", Operations{Favorite: new(false)}, false},
		{"rating", Operations{Rating: new(4)}, false},
		{"flag", Operations{Flag: new("pick")}, false},
		{
			"taken at",
			Operations{TakenAt: &TakenAt{At: time.Unix(0, 0).UTC(), Precision: "year"}},
			false,
		},
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
		Hide:          new(true),
		Rating:        new(5),
		Flag:          new("reject"),
		TakenAt:       &TakenAt{At: time.Date(1974, time.January, 1, 0, 0, 0, 0, time.UTC), Precision: "year"},
	}
	summary := ops.Summary()
	for _, key := range []string{
		"add_albums", "remove_labels", "description", "clear_location", "hide", "rating", "flag",
		"taken_at",
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
			name:       "title and description",
			ops:        Operations{Title: new("cap"), Description: new("desc")},
			wantOK:     true,
			wantArgs:   4, // uid + title + title_edited + description
			wantSubstr: []string{"title = $2", "title_edited = $3", "description = $4"},
		},
		{
			name:       "set location",
			ops:        Operations{Location: &Location{Lat: 1, Lng: 2}},
			wantOK:     true,
			wantArgs:   4, // uid + lat + lng + location_source ('manual')
			wantSubstr: []string{"lat = $2", "lng = $3", "location_source = $4"},
		},
		{
			name:       "clear location and archive",
			ops:        Operations{ClearLocation: true, Archive: new(true)},
			wantOK:     true,
			wantArgs:   1, // uid only; NULL/now()/'manual' are literals
			wantSubstr: []string{"lat = NULL", "lng = NULL", "location_source = 'manual'", "archived_at = now()"},
		},
		{
			name:       "unarchive",
			ops:        Operations{Archive: new(false)},
			wantOK:     true,
			wantArgs:   1,
			wantSubstr: []string{"archived_at = NULL"},
		},
		{
			name:       "hide",
			ops:        Operations{Hide: new(true)},
			wantOK:     true,
			wantArgs:   2, // uid + the boolean, bound rather than literal
			wantSubstr: []string{"hidden_from_library = $2"},
		},
		{
			// A coarse grain is a guess by nature, so it raises the estimate flag; the
			// note is left alone, since it still describes a date that is one.
			name: "set a year",
			ops: Operations{
				TakenAt: &TakenAt{
					At:        time.Date(1974, time.January, 1, 0, 0, 0, 0, time.UTC),
					Precision: "year",
				},
			},
			wantOK:   true,
			wantArgs: 5, // uid + taken_at + source + precision + estimated
			wantSubstr: []string{
				"taken_at = $2", "taken_at_source = $3",
				"taken_at_precision = $4", "taken_at_estimated = $5",
			},
		},
		{
			// The opposite claim: an exact date is a fact, so the flag comes down and
			// the dating note goes with it.
			name: "set an exact date",
			ops: Operations{
				TakenAt: &TakenAt{
					At:        time.Date(1974, time.June, 14, 0, 0, 0, 0, time.UTC),
					Precision: "day",
				},
			},
			wantOK:     true,
			wantArgs:   6, // ... + taken_at_note = ''
			wantSubstr: []string{"taken_at_precision = $4", "taken_at_note = $6"},
		},
		{
			name:       "unhide alongside an archive toggle",
			ops:        Operations{Hide: new(false), Archive: new(false)},
			wantOK:     true,
			wantArgs:   2,
			wantSubstr: []string{"hidden_from_library = $2", "archived_at = NULL"},
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
