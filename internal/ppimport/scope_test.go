package ppimport

import (
	"errors"
	"testing"
)

// TestScope_Query verifies the PhotoPrism search expression a scope renders: one
// term per filter, values quoted (a person's name has spaces), the terms
// space-separated so the source ANDs them, and no expression at all for a scope
// that only names an album (which travels in the separate s= parameter).
func TestScope_Query(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scope Scope
		want  string
	}{
		{name: "empty scope has no query", scope: Scope{}, want: ""},
		{name: "album only travels as s=, not q=", scope: Scope{AlbumUID: "ppal1"}, want: ""},
		{name: "label by slug", scope: Scope{Label: "sdh"}, want: `label:"sdh"`},
		{name: "person by name is quoted", scope: Scope{Person: "Aleš Kozák"}, want: `person:"Aleš Kozák"`},
		{name: "year is a bare number", scope: Scope{Year: 1985}, want: "year:1985"},
		{
			name:  "filters combine into ANDed terms",
			scope: Scope{AlbumUID: "ppal1", Label: "sdh", Person: "Aleš Kozák", Year: 1985},
			want:  `label:"sdh" person:"Aleš Kozák" year:1985`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.scope.Query(); got != tt.want {
				t.Errorf("Scope%+v.Query() = %q, want %q", tt.scope, got, tt.want)
			}
		})
	}
}

// TestScope_validate verifies an unusable scope is rejected before a run is
// opened: no filter at all, or a year that cannot name real photos.
func TestScope_validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scope   Scope
		wantErr error
	}{
		{name: "empty scope", scope: Scope{}, wantErr: ErrEmptyScope},
		{name: "album only", scope: Scope{AlbumUID: "ppal1"}, wantErr: nil},
		{name: "label only", scope: Scope{Label: "sdh"}, wantErr: nil},
		{name: "person only", scope: Scope{Person: "Aleš Kozák"}, wantErr: nil},
		{name: "plausible year", scope: Scope{Year: 1985}, wantErr: nil},
		{name: "year before photography", scope: Scope{Year: 1200}, wantErr: ErrInvalidYear},
		{name: "negative year", scope: Scope{Year: -1985}, wantErr: ErrInvalidYear},
		{name: "five-digit year", scope: Scope{Year: 20250}, wantErr: ErrInvalidYear},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.scope.validate()
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Scope%+v.validate() = %v, want %v", tt.scope, err, tt.wantErr)
			}
		})
	}
}

// TestScope_normalized verifies surrounding whitespace on a flag value neither
// changes the run nor leaks into the source query, and that a whitespace-only
// scope is still empty.
func TestScope_normalized(t *testing.T) {
	t.Parallel()

	scope := Scope{AlbumUID: " ppal1 ", Label: " sdh\t", Person: "  Aleš Kozák "}.normalized()
	want := Scope{AlbumUID: "ppal1", Label: "sdh", Person: "Aleš Kozák"}
	if scope != want {
		t.Errorf("normalized() = %+v, want %+v", scope, want)
	}
	if blank := (Scope{AlbumUID: "  ", Label: " "}).normalized(); !blank.IsEmpty() {
		t.Errorf("a whitespace-only scope is not empty: %+v", blank)
	}
}

// TestScope_String verifies the human-readable rendering used by the CLI and the
// logs names every filter that is set.
func TestScope_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		scope Scope
		want  string
	}{
		{name: "no filter", scope: Scope{}, want: "full"},
		{
			name:  "every filter",
			scope: Scope{AlbumUID: "ppal1", Label: "sdh", Person: "Aleš Kozák", Year: 1985},
			want:  `album=ppal1 label=sdh person="Aleš Kozák" year=1985`,
		},
		{name: "year only", scope: Scope{Year: 1985}, want: "year=1985"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.scope.String(); got != tt.want {
				t.Errorf("Scope%+v.String() = %q, want %q", tt.scope, got, tt.want)
			}
		})
	}
}
