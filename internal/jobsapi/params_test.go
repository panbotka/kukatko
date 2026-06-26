package jobsapi

import (
	"errors"
	"net/url"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
)

// TestParseListOptions verifies query parsing for the recent-jobs listing,
// covering the happy path, the state filter, and each rejected input.
func TestParseListOptions(t *testing.T) {
	t.Parallel()

	dead := jobs.StateDead
	tests := []struct {
		name      string
		query     string
		want      jobs.ListOptions
		wantErr   error
		checkOnly bool
	}{
		{name: "empty", query: "", want: jobs.ListOptions{}},
		{name: "state and paging", query: "state=dead&limit=20&offset=40",
			want: jobs.ListOptions{State: &dead, Limit: 20, Offset: 40}},
		{name: "invalid state", query: "state=bogus", wantErr: errInvalidState},
		{name: "negative limit", query: "limit=-1", wantErr: errInvalidLimit},
		{name: "non-numeric limit", query: "limit=abc", wantErr: errInvalidLimit},
		{name: "limit over cap", query: "limit=99999", wantErr: errInvalidLimit},
		{name: "negative offset", query: "offset=-5", wantErr: errInvalidOffset},
		{name: "non-numeric offset", query: "offset=x", wantErr: errInvalidOffset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			values, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tt.query, err)
			}
			got, err := parseListOptions(values)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("parseListOptions error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseListOptions error = %v, want nil", err)
			}
			assertOptions(t, got, tt.want)
		})
	}
}

// assertOptions compares two ListOptions, dereferencing the State pointer so the
// comparison is by value.
func assertOptions(t *testing.T, got, want jobs.ListOptions) {
	t.Helper()
	if got.Limit != want.Limit || got.Offset != want.Offset {
		t.Errorf("paging = {limit %d offset %d}, want {limit %d offset %d}",
			got.Limit, got.Offset, want.Limit, want.Offset)
	}
	switch {
	case want.State == nil && got.State != nil:
		t.Errorf("state = %v, want nil", *got.State)
	case want.State != nil && got.State == nil:
		t.Errorf("state = nil, want %v", *want.State)
	case want.State != nil && *got.State != *want.State:
		t.Errorf("state = %v, want %v", *got.State, *want.State)
	}
}
