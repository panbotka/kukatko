package main

import "testing"

// TestSignInURL covers what the approval mail's link is built from: the
// instance's public URL with the sign-in route appended, a trailing slash on the
// base tolerated, and an unconfigured base left empty rather than guessed.
func TestSignInURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{name: "plain", base: "https://kukatko.example.cz", want: "https://kukatko.example.cz/login"},
		{name: "trailing slash", base: "https://kukatko.example.cz/", want: "https://kukatko.example.cz/login"},
		{name: "sub path", base: "https://example.cz/fotky", want: "https://example.cz/fotky/login"},
		{name: "padded", base: "  https://kukatko.example.cz  ", want: "https://kukatko.example.cz/login"},
		{name: "unconfigured", base: "", want: ""},
		{name: "slash only", base: "/", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := signInURL(tt.base); got != tt.want {
				t.Errorf("signInURL(%q) = %q, want %q", tt.base, got, tt.want)
			}
		})
	}
}
