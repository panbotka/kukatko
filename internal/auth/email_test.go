package auth

import (
	"errors"
	"strings"
	"testing"
)

// TestNormalizeEmail verifies that an address is trimmed of surrounding
// whitespace and has its domain lower-cased while the local part — which the
// receiving host alone gets to interpret — is left exactly as typed.
func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "already normalized is unchanged", input: "jan@example.com", want: "jan@example.com"},
		{name: "surrounding whitespace is trimmed", input: "  jan@example.com\t\n", want: "jan@example.com"},
		{name: "domain is lower-cased", input: "jan@Example.COM", want: "jan@example.com"},
		{name: "local part keeps its case", input: "Jan.Novak@EXAMPLE.com", want: "Jan.Novak@example.com"},
		{name: "only the last at-sign splits", input: `"a@b"@Example.com`, want: `"a@b"@example.com`},
		{name: "whitespace only collapses to empty", input: "   ", want: ""},
		{name: "no at-sign is trimmed only", input: "  Jan  ", want: "Jan"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeEmail(tt.input); got != tt.want {
				t.Errorf("normalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestValidateEmail verifies which addresses an account may hold: ordinary
// mailboxes pass, while a missing, blank, malformed, over-long or
// display-name-wrapped value is rejected with ErrInvalidEmail.
func TestValidateEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "plain address", input: "jan@example.com", wantErr: nil},
		{name: "subdomain", input: "jan@mail.example.co.uk", wantErr: nil},
		{name: "plus tag and dots", input: "jan.novak+kukatko@example.com", wantErr: nil},
		{name: "placeholder domain is valid", input: "jan@kukatko.invalid", wantErr: nil},
		{name: "empty", input: "", wantErr: ErrInvalidEmail},
		{name: "whitespace only", input: "   ", wantErr: ErrInvalidEmail},
		{name: "no at-sign", input: "jan.example.com", wantErr: ErrInvalidEmail},
		{name: "no local part", input: "@example.com", wantErr: ErrInvalidEmail},
		{name: "no domain", input: "jan@", wantErr: ErrInvalidEmail},
		{name: "dotless domain", input: "jan@localhost", wantErr: ErrInvalidEmail},
		{name: "trailing dot in domain", input: "jan@example.com.", wantErr: ErrInvalidEmail},
		{name: "leading dot in domain", input: "jan@.example.com", wantErr: ErrInvalidEmail},
		{name: "inner whitespace", input: "jan novak@example.com", wantErr: ErrInvalidEmail},
		{name: "display-name form", input: "Jan <jan@example.com>", wantErr: ErrInvalidEmail},
		{name: "two addresses", input: "jan@example.com, eva@example.com", wantErr: ErrInvalidEmail},
		{name: "over MaxEmailLen", input: strings.Repeat("a", MaxEmailLen) + "@example.com", wantErr: ErrInvalidEmail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := validateEmail(tt.input); !errors.Is(err, tt.wantErr) {
				t.Errorf("validateEmail(%q) = %v, want %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestValidateEmail_acceptsNormalizedInput verifies the pair works together: an
// address a person might type, once normalized, is accepted.
func TestValidateEmail_acceptsNormalizedInput(t *testing.T) {
	t.Parallel()

	const typed = "  Jan.Novak@Example.COM  "
	normalized := normalizeEmail(typed)
	if err := validateEmail(normalized); err != nil {
		t.Fatalf("validateEmail(normalizeEmail(%q)) = %v, want nil", typed, err)
	}
	if normalized != "Jan.Novak@example.com" {
		t.Errorf("normalizeEmail(%q) = %q", typed, normalized)
	}
}

// TestPlaceholderEmail verifies the stand-in address the bootstrap maintainer
// gets: a username reduced to a safe local part in the undeliverable .invalid
// domain, always syntactically valid whatever the username held.
func TestPlaceholderEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		want     string
	}{
		{name: "simple username", username: "admin", want: "admin@kukatko.invalid"},
		{name: "upper case is lowered", username: "Admin", want: "admin@kukatko.invalid"},
		{name: "spaces become one separator", username: "jan  novak", want: "jan-novak@kukatko.invalid"},
		{name: "accents are reduced", username: "jan novák", want: "jan-nov-k@kukatko.invalid"},
		{name: "separators are trimmed", username: " .jan. ", want: "jan@kukatko.invalid"},
		{name: "nothing usable falls back", username: "☺", want: "user@kukatko.invalid"},
		{name: "empty falls back", username: "", want: "user@kukatko.invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := placeholderEmail(tt.username)
			if got != tt.want {
				t.Errorf("placeholderEmail(%q) = %q, want %q", tt.username, got, tt.want)
			}
			if err := validateEmail(got); err != nil {
				t.Errorf("validateEmail(placeholderEmail(%q)) = %v, want nil", tt.username, err)
			}
		})
	}
}
