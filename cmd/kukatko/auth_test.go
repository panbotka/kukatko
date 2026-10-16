package main

import (
	"bytes"
	"log/slog"
	"strings"
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
				assertStepNamedOnce(t, err.Error())
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

// TestResolvePasskeys covers the one decision the wiring makes beside building
// the flow: a relying party merely derived from mail.base_url that the library
// refuses (an IP address — a LAN or staging deployment) turns passkeys off with
// one warning and lets the server start, while the same refusal of a relying
// party the operator configured is still a startup failure.
func TestResolvePasskeys(t *testing.T) {
	tests := []struct {
		name     string
		cfg      func(*config.Config)
		wantErr  bool
		wantFlow bool
		wantWarn bool
	}{
		{
			name: "nothing configured is off and quiet",
			cfg:  func(*config.Config) {},
		},
		{
			name:     "a domain base URL offers passkeys",
			cfg:      func(c *config.Config) { c.Mail.BaseURL = "https://kukatko.example.cz" },
			wantFlow: true,
		},
		{
			name:     "an IP base URL only turns passkeys off",
			cfg:      func(c *config.Config) { c.Mail.BaseURL = "http://192.0.2.10:6490" },
			wantWarn: true,
		},
		{
			name:     "a single-label base URL only turns passkeys off",
			cfg:      func(c *config.Config) { c.Mail.BaseURL = "http://staging:6490" },
			wantWarn: true,
		},
		{
			name: "an explicit IP rp_id still fails startup",
			cfg: func(c *config.Config) {
				c.Mail.BaseURL = "http://192.0.2.10:6490"
				c.Auth.Passkey.RPID = "192.0.2.10"
			},
			wantErr: true,
		},
		{
			name: "explicit IP origins still fail startup",
			cfg: func(c *config.Config) {
				c.Auth.Passkey.Origins = []string{"http://192.0.2.10:6490"}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.cfg(cfg)
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))

			passkeys, err := resolvePasskeys(cfg.Passkey(), nil, logger)
			if tt.wantErr {
				if err == nil {
					t.Fatal("resolvePasskeys() error = nil, want a startup failure")
				}
				assertStepNamedOnce(t, err.Error())
				return
			}
			if err != nil {
				t.Fatalf("resolvePasskeys(): %v", err)
			}
			if got := passkeys != nil; got != tt.wantFlow {
				t.Errorf("resolvePasskeys() built a flow = %v, want %v", got, tt.wantFlow)
			}
			warned := strings.Count(logs.String(), "level=WARN")
			if want := map[bool]int{true: 1, false: 0}[tt.wantWarn]; warned != want {
				t.Errorf("resolvePasskeys() logged %d warnings, want %d: %s", warned, want, logs.String())
			}
			if tt.wantWarn && !strings.Contains(logs.String(), "auth.passkey.rp_id") {
				t.Errorf("the warning does not say how to configure passkeys: %s", logs.String())
			}
			if tt.wantWarn {
				assertStepNamedOnce(t, logs.String())
			}
		})
	}
}

// passkeyStep is the words naming the step that builds the relying party. The
// startup WARN once printed them twice, because the wiring re-wrapped an error
// internal/auth had already prefixed with them.
const passkeyStep = "building the passkey relying party"

// assertStepNamedOnce fails the test unless text — an error or the logged
// warning carrying it — names the relying-party step exactly once.
func assertStepNamedOnce(t *testing.T, text string) {
	t.Helper()
	if n := strings.Count(text, passkeyStep); n != 1 {
		t.Errorf("%q occurs %d times, want exactly once: %s", passkeyStep, n, text)
	}
}
