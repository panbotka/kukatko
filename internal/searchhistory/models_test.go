package searchhistory

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestNormalize checks the trimming and the length cap, including that the cap
// counts characters rather than bytes (a Czech query must not lose half of it) and
// that a whitespace-only query normalizes to the empty string callers treat as
// "nothing to remember".
func TestNormalize(t *testing.T) {
	t.Parallel()

	longAscii := strings.Repeat("a", MaxQueryLength+10)
	longCzech := strings.Repeat("á", MaxQueryLength+10)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain query passes through", in: "svatba 1974", want: "svatba 1974"},
		{name: "surrounding whitespace is trimmed", in: "  person:Anna \n", want: "person:Anna"},
		{
			name: "inner whitespace is preserved",
			in:   `title:"a  b"  label:cat`,
			want: `title:"a  b"  label:cat`,
		},
		{name: "empty stays empty", in: "", want: ""},
		{name: "whitespace only collapses to empty", in: " \t\n ", want: ""},
		{name: "over-long ascii is capped", in: longAscii, want: strings.Repeat("a", MaxQueryLength)},
		{name: "over-long czech is capped by characters", in: longCzech, want: strings.Repeat("á", MaxQueryLength)},
		{
			name: "whitespace left by the cut is trimmed",
			in:   strings.Repeat("b", MaxQueryLength-1) + "   tail",
			want: strings.Repeat("b", MaxQueryLength-1),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Normalize(tt.in)
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if n := utf8.RuneCountInString(got); n > MaxQueryLength {
				t.Errorf("Normalize(%q) is %d characters, want at most %d", tt.in, n, MaxQueryLength)
			}
		})
	}
}
