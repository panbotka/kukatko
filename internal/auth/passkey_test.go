package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func TestNormalizePasskeyName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr error
	}{
		{name: "trims surrounding whitespace", input: "  Telefon\n", want: "Telefon"},
		{name: "an empty name is allowed", input: "   ", want: ""},
		{name: "at the limit", input: strings.Repeat("a", MaxPasskeyNameLen), want: strings.Repeat("a", MaxPasskeyNameLen)},
		{
			name:  "accents count as one rune each",
			input: strings.Repeat("ě", MaxPasskeyNameLen),
			want:  strings.Repeat("ě", MaxPasskeyNameLen),
		},
		{
			name:    "one rune over the limit",
			input:   strings.Repeat("ě", MaxPasskeyNameLen+1),
			wantErr: ErrPasskeyNameTooLong,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizePasskeyName(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("normalizePasskeyName(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("normalizePasskeyName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestPasskeyView pins the client-facing projection: the transports come across
// as plain strings and nothing of the credential itself does.
func TestPasskeyView(t *testing.T) {
	t.Parallel()

	used := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	pk := Passkey{
		ID: "pk1", Name: "Telefon", CreatedAt: used.Add(-time.Hour), LastUsedAt: &used,
		Credential: webauthn.Credential{
			ID:        []byte("cred"),
			PublicKey: []byte("secretless but private-ish"),
			Transport: []protocol.AuthenticatorTransport{protocol.Internal, protocol.Hybrid},
		},
	}
	view := pk.View()
	if view.ID != "pk1" || view.Name != "Telefon" || view.LastUsedAt != &used {
		t.Fatalf("View() = %+v, want the row's own fields", view)
	}
	if len(view.Transports) != 2 || view.Transports[0] != "internal" || view.Transports[1] != "hybrid" {
		t.Errorf("View().Transports = %v, want [internal hybrid]", view.Transports)
	}
}

// TestPasskeyView_noTransports pins the empty case as a JSON array rather than
// null, so a client can iterate it without a guard.
func TestPasskeyView_noTransports(t *testing.T) {
	t.Parallel()
	if got := (Passkey{}).View().Transports; got == nil || len(got) != 0 {
		t.Errorf("View().Transports = %v, want an empty non-nil slice", got)
	}
}

func TestPasskeyUser_webAuthnFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		user        User
		wantName    string
		wantDisplay string
	}{
		{
			name:        "display name wins",
			user:        User{UID: "us1", Username: "alice", DisplayName: "Alice Nováková"},
			wantName:    "alice",
			wantDisplay: "Alice Nováková",
		},
		{
			name:        "falls back to the username",
			user:        User{UID: "us2", Username: "bob"},
			wantName:    "bob",
			wantDisplay: "bob",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			adapted := passkeyUser{user: tt.user}
			if got := string(adapted.WebAuthnID()); got != tt.user.UID {
				t.Errorf("WebAuthnID() = %q, want the account UID %q", got, tt.user.UID)
			}
			if got := adapted.WebAuthnName(); got != tt.wantName {
				t.Errorf("WebAuthnName() = %q, want %q", got, tt.wantName)
			}
			if got := adapted.WebAuthnDisplayName(); got != tt.wantDisplay {
				t.Errorf("WebAuthnDisplayName() = %q, want %q", got, tt.wantDisplay)
			}
		})
	}
}

// TestPasskeyUser_credentials pins that the ceremony is driven from the list it
// is given, empty included — an account with no passkeys must not look like one
// whose credentials failed to load.
func TestPasskeyUser_credentials(t *testing.T) {
	t.Parallel()
	credentials := []webauthn.Credential{{ID: []byte("a")}, {ID: []byte("b")}}
	if got := (passkeyUser{credentials: credentials}).WebAuthnCredentials(); len(got) != 2 {
		t.Errorf("WebAuthnCredentials() = %v, want both credentials", got)
	}
	if got := (passkeyUser{}).WebAuthnCredentials(); len(got) != 0 {
		t.Errorf("WebAuthnCredentials() = %v, want none", got)
	}
}

func TestCheckPasskeyLogin(t *testing.T) {
	t.Parallel()

	approved := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		user    User
		wantErr error
	}{
		{name: "an approved, enabled account signs in", user: User{ApprovedAt: &approved}},
		{
			name:    "a disabled account is refused without saying why",
			user:    User{Disabled: true, ApprovedAt: &approved},
			wantErr: ErrPasskeyRejected,
		},
		{
			name:    "an account nobody has approved is told what it waits for",
			user:    User{},
			wantErr: ErrNotApproved,
		},
		{
			name:    "disabled outranks unapproved",
			user:    User{Disabled: true},
			wantErr: ErrPasskeyRejected,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := checkPasskeyLogin(tt.user); !errors.Is(err, tt.wantErr) {
				t.Errorf("checkPasskeyLogin() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestPasskeyLimitKeys pins the two budgets apart. They share the login
// limiter's allowance, so a shared key would let opening ceremonies exhaust the
// budget that guards credential verification.
func TestPasskeyLimitKeys(t *testing.T) {
	t.Parallel()
	if passkeyLoginLimitKey("192.0.2.1") == passkeyBeginLimitKey("192.0.2.1") {
		t.Error("the begin and finish budgets share a key; they must not")
	}
	if passkeyLoginLimitKey("192.0.2.1") == passkeyLoginLimitKey("192.0.2.2") {
		t.Error("two addresses share a login budget key; they must not")
	}
}

func TestWithName(t *testing.T) {
	t.Parallel()
	got := withName(nil, "Telefon")
	if got["name"] != "Telefon" {
		t.Fatalf("withName(nil, …) = %v, want the key set", got)
	}
	got = withName(map[string]any{"other": 1}, "Klíč")
	if got["name"] != "Klíč" || got["other"] != 1 {
		t.Errorf("withName(existing, …) = %v, want both keys", got)
	}
}

// TestNewPasskeys_refusesAnImpossibleRelyingParty pins the startup failure the
// wiring depends on: a relying party the library will not accept must not become
// a silently disabled feature, because credentials minted under one could never
// be used again.
func TestNewPasskeys_refusesAnImpossibleRelyingParty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  PasskeysConfig
	}{
		{name: "no relying-party id", cfg: PasskeysConfig{Origins: []string{"https://example.test"}}},
		{name: "no origin", cfg: PasskeysConfig{RPID: "example.test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewPasskeys(tt.cfg); err == nil {
				t.Error("NewPasskeys() error = nil, want a refusal")
			}
		})
	}
}

// TestNewPasskeys_defaults pins the ceremony window: an unset TTL must not mean
// "expires immediately", which would make every ceremony unanswerable.
func TestNewPasskeys_defaults(t *testing.T) {
	t.Parallel()
	passkeys, err := NewPasskeys(PasskeysConfig{RPID: "example.test", Origins: []string{"https://example.test"}})
	if err != nil {
		t.Fatalf("NewPasskeys: %v", err)
	}
	if got := passkeys.CeremonyTTL(); got != defaultCeremonyTTL {
		t.Errorf("CeremonyTTL() = %s, want %s", got, defaultCeremonyTTL)
	}
}
