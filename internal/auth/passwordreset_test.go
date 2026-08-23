package auth

import (
	"strings"
	"testing"
	"time"
)

// TestPasswordResetTokenUsable pins when a link still works: never after it was
// used, never at or after its expiry, and the boundary instant counts as
// expired — the same rule an API token's expiry follows.
func TestPasswordResetTokenUsable(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	used := now.Add(-time.Hour)
	cases := []struct {
		name  string
		token PasswordResetToken
		want  bool
	}{
		{"fresh", PasswordResetToken{ExpiresAt: now.Add(time.Hour)}, true},
		{"used", PasswordResetToken{ExpiresAt: now.Add(time.Hour), UsedAt: &used}, false},
		{"expired", PasswordResetToken{ExpiresAt: now.Add(-time.Second)}, false},
		{"at the boundary", PasswordResetToken{ExpiresAt: now}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.token.Usable(now); got != tc.want {
				t.Errorf("Usable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHashPasswordResetToken checks the property the whole table rests on: the
// stored value is a stable hash and not the token itself, so reading the table
// hands nobody a working link.
func TestHashPasswordResetToken(t *testing.T) {
	const token = "3PYuXZ0m7Qk1nB2vLd4sTgHjKfWc8RaE"
	hash := hashPasswordResetToken(token)
	if hash == token {
		t.Fatal("the hash equals the token")
	}
	if len(hash) != 64 {
		t.Errorf("hash length = %d, want 64 hex characters", len(hash))
	}
	if again := hashPasswordResetToken(token); again != hash {
		t.Errorf("hashing twice gave %q and %q", hash, again)
	}
	if other := hashPasswordResetToken(token + "x"); other == hash {
		t.Error("a different token hashed to the same value")
	}
}

// TestNewPasswordResetDefaults covers the three defaults a caller may leave to
// the constructor: no mail, no link base and no TTL.
func TestNewPasswordResetDefaults(t *testing.T) {
	pr := NewPasswordReset(PasswordResetConfig{})
	if pr.ttl != PasswordResetTTL {
		t.Errorf("ttl = %v, want %v", pr.ttl, PasswordResetTTL)
	}
	if got := pr.link("tok"); got != passwordResetPath+"/tok" {
		t.Errorf("link with no base = %q, want the site-relative path", got)
	}
	if _, ok := pr.mail.(noMail); !ok {
		t.Errorf("mail = %T, want the no-op scheduler", pr.mail)
	}
}

// TestNewPasswordResetLinkBase pins that a configured base is used verbatim
// except for a trailing slash, so a link never carries a doubled separator.
func TestNewPasswordResetLinkBase(t *testing.T) {
	for _, base := range []string{
		"https://kukatko.example.test/password-reset",
		"https://kukatko.example.test/password-reset/",
		"  https://kukatko.example.test/password-reset  ",
	} {
		pr := NewPasswordReset(PasswordResetConfig{LinkBase: base})
		want := "https://kukatko.example.test/password-reset/tok"
		if got := pr.link("tok"); got != want {
			t.Errorf("link(%q) = %q, want %q", strings.TrimSpace(base), got, want)
		}
	}
}
