package config

import (
	"errors"
	"reflect"
	"testing"
)

// TestConfigPasskey pins the resolution the whole feature switches on: what the
// operator configured, what is derived from the address they already declared for
// mail, and — the case that matters most — when the answer is "off".
func TestConfigPasskey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		passkey     PasskeyConfig
		baseURL     string
		wantID      string
		wantOrigins []string
		wantEnabled bool
		wantDerived bool
	}{
		{
			name:        "nothing configured stays off",
			wantEnabled: false,
			wantDerived: true,
		},
		{
			name:        "derived from the mail base URL",
			baseURL:     "https://photos.example.com/",
			wantID:      "photos.example.com",
			wantOrigins: []string{"https://photos.example.com"},
			wantEnabled: true,
			wantDerived: true,
		},
		{
			// Enabled as far as the configuration can tell; the WebAuthn library
			// refuses an IP ID, and Derived is what lets the wiring answer that
			// with "passkeys off" instead of a failed start.
			name:        "an IP base URL is derived",
			baseURL:     "http://192.0.2.10:6490",
			wantID:      "192.0.2.10",
			wantOrigins: []string{"http://192.0.2.10:6490"},
			wantEnabled: true,
			wantDerived: true,
		},
		{
			name:        "an explicit id with a derived origin is configured",
			passkey:     PasskeyConfig{RPID: "photos.example.com"},
			baseURL:     "https://photos.example.com",
			wantID:      "photos.example.com",
			wantOrigins: []string{"https://photos.example.com"},
			wantEnabled: true,
		},
		{
			name:        "an explicit relying party wins over the derivation",
			passkey:     PasskeyConfig{RPID: "example.com", Origins: []string{"https://photos.example.com"}},
			baseURL:     "https://other.example.net",
			wantID:      "example.com",
			wantOrigins: []string{"https://photos.example.com"},
			wantEnabled: true,
		},
		{
			name:        "an origin alone yields its host as the id",
			passkey:     PasskeyConfig{Origins: []string{"https://Photos.Example.COM:8443/login"}},
			wantID:      "photos.example.com",
			wantOrigins: []string{"https://photos.example.com:8443"},
			wantEnabled: true,
		},
		{
			name: "several origins keep their order and lose duplicates",
			passkey: PasskeyConfig{Origins: []string{
				"https://photos.example.com", "http://localhost:5173", "https://photos.example.com/",
			}},
			wantID:      "photos.example.com",
			wantOrigins: []string{"https://photos.example.com", "http://localhost:5173"},
			wantEnabled: true,
		},
		{
			name:        "an id without any origin cannot run a ceremony",
			passkey:     PasskeyConfig{RPID: "example.com"},
			wantID:      "example.com",
			wantEnabled: false,
		},
		{
			name:        "a base URL that is not an origin derives nothing",
			baseURL:     "not-a-url",
			wantEnabled: false,
			wantDerived: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{}
			cfg.Auth.Passkey = tt.passkey
			cfg.Mail.BaseURL = tt.baseURL

			got := cfg.Passkey()
			if got.Enabled != tt.wantEnabled {
				t.Fatalf("Passkey().Enabled = %v, want %v (%+v)", got.Enabled, tt.wantEnabled, got)
			}
			if got.Derived != tt.wantDerived {
				t.Errorf("Passkey().Derived = %v, want %v", got.Derived, tt.wantDerived)
			}
			if got.ID != tt.wantID {
				t.Errorf("Passkey().ID = %q, want %q", got.ID, tt.wantID)
			}
			if len(got.Origins) != len(tt.wantOrigins) || (len(tt.wantOrigins) > 0 &&
				!reflect.DeepEqual(got.Origins, tt.wantOrigins)) {
				t.Errorf("Passkey().Origins = %v, want %v", got.Origins, tt.wantOrigins)
			}
		})
	}
}

// TestConfigPasskey_displayName pins the name an authenticator shows: the
// product's, unless the operator chose one.
func TestConfigPasskey_displayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configured string
		want       string
	}{
		{name: "default", want: defaultRPDisplayName},
		{name: "whitespace only falls back", configured: "   ", want: defaultRPDisplayName},
		{name: "configured wins", configured: " Rodinné fotky ", want: "Rodinné fotky"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := &Config{}
			cfg.Auth.Passkey.RPDisplayName = tt.configured
			if got := cfg.Passkey().DisplayName; got != tt.want {
				t.Errorf("Passkey().DisplayName = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPasskeyConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		origins []string
		wantErr error
	}{
		{name: "no origins is valid — the feature is simply off"},
		{name: "https origin", origins: []string{"https://photos.example.com"}},
		{name: "http origin for a local development instance", origins: []string{"http://localhost:5173"}},
		{name: "a bare domain is not an origin", origins: []string{"photos.example.com"},
			wantErr: ErrInvalidPasskeyOrigin},
		{name: "a scheme no browser reports", origins: []string{"ftp://photos.example.com"},
			wantErr: ErrInvalidPasskeyOrigin},
		{name: "a scheme with no host", origins: []string{"https://"}, wantErr: ErrInvalidPasskeyOrigin},
		{name: "one bad entry fails the whole list",
			origins: []string{"https://photos.example.com", "nonsense"}, wantErr: ErrInvalidPasskeyOrigin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := (PasskeyConfig{Origins: tt.origins}).validate(); !errors.Is(err, tt.wantErr) {
				t.Errorf("validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseOrigin_empty pins that "nothing configured" is not a mistake: it must
// not be an error, or an instance that never mentions passkeys would refuse to
// start.
func TestParseOrigin_empty(t *testing.T) {
	t.Parallel()
	origin, host, err := parseOrigin("   ")
	if err != nil || origin != "" || host != "" {
		t.Errorf("parseOrigin(blank) = (%q, %q, %v), want empty and no error", origin, host, err)
	}
}

// TestPasskeyEnvOverride pins the environment form of the two keys an operator
// is most likely to set from a container's env file rather than a YAML file —
// and in particular that the origin *list* arrives comma-separated, the same way
// web.trusted_proxies does. It cannot run in parallel: it sets process
// environment.
func TestPasskeyEnvOverride(t *testing.T) {
	t.Setenv("KUKATKO_DATABASE_URL", "postgres://user:pass@localhost/kukatko")
	t.Setenv("KUKATKO_AUTH_PASSKEY_RP_ID", "example.cz")
	t.Setenv("KUKATKO_AUTH_PASSKEY_ORIGINS", "https://a.example.cz,https://b.example.cz")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	rp := cfg.Passkey()
	want := []string{"https://a.example.cz", "https://b.example.cz"}
	if rp.ID != "example.cz" || !reflect.DeepEqual(rp.Origins, want) || !rp.Enabled {
		t.Errorf("Passkey() = %+v, want id example.cz and origins %v", rp, want)
	}
}
