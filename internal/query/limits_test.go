package query

import (
	"strings"
	"testing"
)

// TestComplexity checks that Complexity counts one condition per free-text term
// and one per filter OR-alternative, since that count is what a trust boundary
// caps to bound a request's query cost.
func TestComplexity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "empty query", input: "", want: 0},
		{name: "one free-text term", input: "beach", want: 1},
		{name: "several free-text terms", input: "beach sunset dog", want: 3},
		{name: "negated term still counts", input: "-blurry", want: 1},
		{name: "single filter value", input: "iso:100", want: 1},
		{name: "filter alternatives count individually", input: "label:cat|dog|fox", want: 3},
		{
			name:  "terms and filter alternatives combine",
			input: "beach iso:100-400 label:cat|dog -blurry",
			want:  5, // terms: beach, blurry; values: iso(1) + label(2)
		},
		{
			name:  "packed alternatives count fully",
			input: "title:" + strings.Repeat("a|", 999) + "a",
			want:  1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Parse(tt.input).Complexity(); got != tt.want {
				t.Errorf("Parse(%.40q).Complexity() = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestComplexityCapConstants guards the invariant the caps rely on: the length
// cap must be generous enough to admit a legitimate query, and the complexity
// cap must sit far below PostgreSQL's 65535 bound-parameter limit so an
// at-the-cap query never trips a 500 instead of matching.
func TestComplexityCapConstants(t *testing.T) {
	t.Parallel()

	if MaxComplexity <= 0 || MaxComplexity >= 65535 {
		t.Errorf("MaxComplexity = %d, want a positive value well below 65535", MaxComplexity)
	}
	if MaxLength < 1024 {
		t.Errorf("MaxLength = %d, want it generous enough for a real query", MaxLength)
	}
}
