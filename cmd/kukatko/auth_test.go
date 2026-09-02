package main

import (
	"testing"

	"github.com/panbotka/kukatko/internal/config"
)

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

// TestBuildPasskeys covers what the wiring does with a relying party the
// configuration resolved: build the flow, or fail startup. Failing is the point
// of the second case — silently degrading to "no passkeys" would leave an
// operator who configured the feature staring at an interface that never offers
// it. (Whether an instance has a relying party at all is decided in
// internal/config; see TestConfigPasskey.)
func TestBuildPasskeys(t *testing.T) {
	tests := []struct {
		name    string
		rp      config.RelyingParty
		wantErr bool
	}{
		{
			name: "a usable relying party",
			rp: config.RelyingParty{
				ID: "kukatko.example.cz", DisplayName: "Kukátko",
				Origins: []string{"https://kukatko.example.cz"}, Enabled: true,
			},
		},
		{
			name: "an id the library refuses",
			rp: config.RelyingParty{
				ID: "..", Origins: []string{"https://kukatko.example.cz"}, Enabled: true,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passkeys, err := buildPasskeys(tt.rp, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatal("buildPasskeys() error = nil, want a startup failure")
				}
				return
			}
			if err != nil {
				t.Fatalf("buildPasskeys(): %v", err)
			}
			if passkeys == nil {
				t.Error("buildPasskeys() built no flow for a usable relying party")
			}
		})
	}
}
