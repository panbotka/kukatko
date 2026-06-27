package organize

import "testing"

// TestSlugify checks diacritics stripping, lower-casing, separator collapsing and
// the per-kind fallback for names that strip to nothing.
func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		in       string
		fallback string
		want     string
	}{
		{name: "simple", in: "Holiday", fallback: albumFallbackSlug, want: "holiday"},
		{name: "spaces collapse", in: "Léto  2024", fallback: albumFallbackSlug, want: "leto-2024"},
		{name: "czech diacritics", in: "Děti u Řeky", fallback: albumFallbackSlug, want: "deti-u-reky"},
		{name: "punctuation to hyphen", in: "Trip (best)!", fallback: labelFallbackSlug, want: "trip-best"},
		{name: "trimmed", in: "  --Hory--  ", fallback: albumFallbackSlug, want: "hory"},
		{name: "empty album fallback", in: "", fallback: albumFallbackSlug, want: albumFallbackSlug},
		{name: "only punctuation label fallback", in: "!!!", fallback: labelFallbackSlug, want: labelFallbackSlug},
		{name: "non-latin label fallback", in: "日本語", fallback: labelFallbackSlug, want: labelFallbackSlug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := slugify(tt.in, tt.fallback); got != tt.want {
				t.Errorf("slugify(%q, %q) = %q, want %q", tt.in, tt.fallback, got, tt.want)
			}
		})
	}
}

// TestCandidateSlug checks that the first attempt is the base and later attempts
// append the expected numeric suffix.
func TestCandidateSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		base    string
		attempt int
		want    string
	}{
		{name: "first attempt is base", base: "holiday", attempt: 0, want: "holiday"},
		{name: "second attempt suffix 2", base: "holiday", attempt: 1, want: "holiday-2"},
		{name: "tenth attempt suffix 11", base: "holiday", attempt: 10, want: "holiday-11"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := candidateSlug(tt.base, tt.attempt); got != tt.want {
				t.Errorf("candidateSlug(%q, %d) = %q, want %q", tt.base, tt.attempt, got, tt.want)
			}
		})
	}
}
