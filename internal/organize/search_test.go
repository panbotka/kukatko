package organize

import "testing"

// TestLikePattern verifies the "contains" wrapping and that LIKE metacharacters
// in the query are escaped so they match literally.
func TestLikePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain word is wrapped", in: "beach", want: "%beach%"},
		{name: "percent is escaped", in: "50%", want: `%50\%%`},
		{name: "underscore is escaped", in: "a_b", want: `%a\_b%`},
		{name: "backslash is escaped", in: `a\b`, want: `%a\\b%`},
		{name: "empty stays a match-all contains", in: "", want: "%%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := likePattern(tt.in); got != tt.want {
				t.Errorf("likePattern(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestClampSearchLimit verifies positive limits pass through and non-positive
// limits fall back to the default.
func TestClampSearchLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "positive passes through", in: 3, want: 3},
		{name: "zero uses default", in: 0, want: defaultSearchLimit},
		{name: "negative uses default", in: -5, want: defaultSearchLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := clampSearchLimit(tt.in); got != tt.want {
				t.Errorf("clampSearchLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
