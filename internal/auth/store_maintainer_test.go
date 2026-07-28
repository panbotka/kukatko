package auth

import "testing"

// TestStrandsInstance pins the last-maintainer rule: a change is refused exactly
// when it takes the instance from "has an enabled maintainer" to "has none".
// The asymmetric case matters most — an instance that already had zero enabled
// maintainers must stay editable, or the guard would freeze every unrelated user
// change on it.
func TestStrandsInstance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		before int
		after  int
		want   bool
	}{
		{
			name:   "sole maintainer demoted or disabled is refused",
			before: 1,
			after:  0,
			want:   true,
		},
		{
			name:   "one of two maintainers stepping down is allowed",
			before: 2,
			after:  1,
			want:   false,
		},
		{
			name:   "unrelated change leaving the count alone is allowed",
			before: 1,
			after:  1,
			want:   false,
		},
		{
			name:   "promotion raising the count is allowed",
			before: 1,
			after:  2,
			want:   false,
		},
		{
			name:   "instance that never had a maintainer stays editable",
			before: 0,
			after:  0,
			want:   false,
		},
		{
			name:   "first maintainer appearing on a bare instance is allowed",
			before: 0,
			after:  1,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := strandsInstance(tt.before, tt.after); got != tt.want {
				t.Errorf("strandsInstance(%d, %d) = %v, want %v", tt.before, tt.after, got, tt.want)
			}
		})
	}
}
