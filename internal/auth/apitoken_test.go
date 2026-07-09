package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestGenerateAPIToken_shapeAndUniqueness verifies that generated tokens are
// unique, carry a 256-bit secret, embed their row id, and are accepted by the
// parser and the hash check.
func TestGenerateAPIToken_shapeAndUniqueness(t *testing.T) {
	t.Parallel()

	const n = 500
	seen := make(map[string]bool, n)
	for range n {
		id, hash, plaintext, err := generateAPIToken()
		if err != nil {
			t.Fatalf("generateAPIToken: %v", err)
		}
		if seen[plaintext] {
			t.Fatalf("duplicate token generated: %q", plaintext)
		}
		seen[plaintext] = true

		if !strings.HasPrefix(id, apiTokenIDPrefix) {
			t.Errorf("token id %q lacks prefix %q", id, apiTokenIDPrefix)
		}
		if !strings.HasPrefix(plaintext, apiTokenScheme+"_") {
			t.Errorf("plaintext %q lacks scheme prefix", plaintext)
		}

		gotID, secret, err := parseAPIToken(plaintext)
		if err != nil {
			t.Fatalf("parseAPIToken(%q): %v", plaintext, err)
		}
		if gotID != id {
			t.Errorf("parsed id = %q, want %q", gotID, id)
		}
		raw, err := base64.RawURLEncoding.DecodeString(secret)
		if err != nil {
			t.Fatalf("secret %q is not valid base64url: %v", secret, err)
		}
		if len(raw)*8 < 256 {
			t.Errorf("secret carries %d bits, want at least 256", len(raw)*8)
		}
		if !apiTokenSecretMatches(hash, secret) {
			t.Errorf("generated secret does not match its own hash")
		}
	}
}

// TestHashAPITokenSecret_deterministicAndDistinct verifies the hash is a stable
// hex-encoded SHA-256 and that different secrets hash differently.
func TestHashAPITokenSecret_deterministicAndDistinct(t *testing.T) {
	t.Parallel()

	// Known-answer test: SHA-256("abc"), so a future refactor cannot silently
	// swap the primitive (in particular, not to a salted, slow one).
	const wantABC = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := hashAPITokenSecret("abc"); got != wantABC {
		t.Errorf("hashAPITokenSecret(%q) = %q, want %q", "abc", got, wantABC)
	}
	if got := hashAPITokenSecret("abc"); got != wantABC {
		t.Error("hashAPITokenSecret is not deterministic")
	}
	if hashAPITokenSecret("abc") == hashAPITokenSecret("abd") {
		t.Error("distinct secrets produced the same hash")
	}
}

// TestAPITokenSecretMatches verifies the constant-time comparison accepts the
// right secret and rejects near-misses and empty input.
func TestAPITokenSecretMatches(t *testing.T) {
	t.Parallel()

	hash := hashAPITokenSecret("s3cret")
	tests := []struct {
		name   string
		secret string
		want   bool
	}{
		{"exact", "s3cret", true},
		{"wrong", "s3crey", false},
		{"prefix", "s3cre", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := apiTokenSecretMatches(hash, tt.secret); got != tt.want {
				t.Errorf("apiTokenSecretMatches(hash, %q) = %v, want %v", tt.secret, got, tt.want)
			}
		})
	}
	if apiTokenSecretMatches("", "s3cret") {
		t.Error("an empty stored hash must not match any secret")
	}
}

// TestParseAPIToken_rejectsMalformed verifies the parser accepts only
// "kkt_<id>_<secret>" with both fields non-empty, and preserves an underscore
// inside the secret (base64url secrets may contain one).
func TestParseAPIToken_rejectsMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		plaintext  string
		wantID     string
		wantSecret string
		wantErr    bool
	}{
		{name: "valid", plaintext: "kkt_atabc_sec", wantID: "atabc", wantSecret: "sec"},
		{name: "secret with underscore", plaintext: "kkt_atabc_se_c", wantID: "atabc", wantSecret: "se_c"},
		{name: "empty", plaintext: "", wantErr: true},
		{name: "garbage", plaintext: "not-a-token", wantErr: true},
		{name: "wrong scheme", plaintext: "ghp_atabc_sec", wantErr: true},
		{name: "missing secret", plaintext: "kkt_atabc", wantErr: true},
		{name: "empty secret", plaintext: "kkt_atabc_", wantErr: true},
		{name: "empty id", plaintext: "kkt__sec", wantErr: true},
		{name: "session token", plaintext: "8Xf3zQ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, secret, err := parseAPIToken(tt.plaintext)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidAPIToken) {
					t.Fatalf("parseAPIToken(%q) error = %v, want ErrInvalidAPIToken", tt.plaintext, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAPIToken(%q): %v", tt.plaintext, err)
			}
			if id != tt.wantID || secret != tt.wantSecret {
				t.Errorf("parseAPIToken(%q) = (%q, %q), want (%q, %q)",
					tt.plaintext, id, secret, tt.wantID, tt.wantSecret)
			}
		})
	}
}

// TestAPIToken_predicates verifies the revocation and expiry predicates,
// including the boundary instant and the never-expires case.
func TestAPIToken_predicates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	tests := []struct {
		name        string
		tok         APIToken
		wantRevoked bool
		wantExpired bool
		wantActive  bool
	}{
		{name: "no expiry, live", tok: APIToken{}, wantActive: true},
		{name: "future expiry", tok: APIToken{ExpiresAt: &future}, wantActive: true},
		{name: "past expiry", tok: APIToken{ExpiresAt: &past}, wantExpired: true},
		{name: "expiry at now", tok: APIToken{ExpiresAt: &now}, wantExpired: true},
		{name: "revoked", tok: APIToken{RevokedAt: &past}, wantRevoked: true},
		{
			name:        "revoked and expired",
			tok:         APIToken{RevokedAt: &past, ExpiresAt: &past},
			wantRevoked: true,
			wantExpired: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.tok.Revoked(); got != tt.wantRevoked {
				t.Errorf("Revoked() = %v, want %v", got, tt.wantRevoked)
			}
			if got := tt.tok.Expired(now); got != tt.wantExpired {
				t.Errorf("Expired(now) = %v, want %v", got, tt.wantExpired)
			}
			if got := tt.tok.Active(now); got != tt.wantActive {
				t.Errorf("Active(now) = %v, want %v", got, tt.wantActive)
			}
		})
	}
}

// TestAPIToken_shouldRecordUse verifies last_used_at is rewritten at most once
// per apiTokenUseInterval, mirroring the sliding session's guard.
func TestAPIToken_shouldRecordUse(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	justNow := now.Add(-time.Second)
	exactly := now.Add(-apiTokenUseInterval)
	longAgo := now.Add(-time.Hour)

	tests := []struct {
		name     string
		lastUsed *time.Time
		want     bool
	}{
		{name: "never used", lastUsed: nil, want: true},
		{name: "used a second ago", lastUsed: &justNow, want: false},
		{name: "used exactly one interval ago", lastUsed: &exactly, want: true},
		{name: "used an hour ago", lastUsed: &longAgo, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tok := APIToken{LastUsedAt: tt.lastUsed}
			if got := tok.shouldRecordUse(now); got != tt.want {
				t.Errorf("shouldRecordUse(now) = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNormalizeAPITokenName verifies trimming, the empty-name error, and the
// length cap.
func TestNormalizeAPITokenName(t *testing.T) {
	t.Parallel()

	if _, err := normalizeAPITokenName("   "); !errors.Is(err, ErrAPITokenNameRequired) {
		t.Errorf("normalizeAPITokenName(blank) error = %v, want ErrAPITokenNameRequired", err)
	}
	got, err := normalizeAPITokenName("  backup script \n")
	if err != nil {
		t.Fatalf("normalizeAPITokenName: %v", err)
	}
	if got != "backup script" {
		t.Errorf("normalizeAPITokenName = %q, want %q", got, "backup script")
	}
	long, err := normalizeAPITokenName(strings.Repeat("x", apiTokenNameMaxLen+50))
	if err != nil {
		t.Fatalf("normalizeAPITokenName(long): %v", err)
	}
	if len(long) != apiTokenNameMaxLen {
		t.Errorf("long name length = %d, want %d", len(long), apiTokenNameMaxLen)
	}
}

// TestBearerToken verifies extraction of the Authorization header credential,
// including the case-insensitive scheme and the cases that must fall through to
// cookie authentication.
func TestBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   string
		wantOK bool
	}{
		{name: "canonical", header: "Bearer kkt_at1_sec", want: "kkt_at1_sec", wantOK: true},
		{name: "lowercase scheme", header: "bearer kkt_at1_sec", want: "kkt_at1_sec", wantOK: true},
		{name: "mixed case scheme", header: "BeArEr kkt_at1_sec", want: "kkt_at1_sec", wantOK: true},
		{name: "trailing space", header: "Bearer  kkt_at1_sec  ", want: "kkt_at1_sec", wantOK: true},
		{name: "absent", header: ""},
		{name: "basic auth", header: "Basic dXNlcjpwYXNz"},
		{name: "scheme only", header: "Bearer"},
		{name: "empty credential", header: "Bearer "},
		{name: "blank credential", header: "Bearer    "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := bearerToken(tt.header)
			if ok != tt.wantOK {
				t.Fatalf("bearerToken(%q) ok = %v, want %v", tt.header, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("bearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}
