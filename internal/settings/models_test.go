package settings

import (
	"errors"
	"strings"
	"testing"
)

// TestUpdate_validate covers normalization and the one refusal: enabling
// registration while the shared secret is blank.
func TestUpdate_validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      Update
		wantErr    error
		wantSecret string
		wantMD     string
	}{
		{
			name:       "closed registration accepts an empty secret",
			input:      Update{RegistrationEnabled: false, RegistrationSecret: ""},
			wantSecret: "",
		},
		{
			name:       "open registration with a secret passes",
			input:      Update{RegistrationEnabled: true, RegistrationSecret: "rodina2026"},
			wantSecret: "rodina2026",
		},
		{
			name:       "the secret is trimmed",
			input:      Update{RegistrationEnabled: true, RegistrationSecret: "  rodina2026\n"},
			wantSecret: "rodina2026",
		},
		{
			name:    "open registration with no secret is refused",
			input:   Update{RegistrationEnabled: true, RegistrationSecret: ""},
			wantErr: ErrSecretRequired,
		},
		{
			name:    "open registration with a whitespace-only secret is refused",
			input:   Update{RegistrationEnabled: true, RegistrationSecret: " \t\n"},
			wantErr: ErrSecretRequired,
		},
		{
			name: "the welcome Markdown keeps its leading whitespace",
			input: Update{
				RegistrationEnabled: false,
				WelcomeMarkdown:     "  indented code\n\n# Vítej\n",
			},
			wantMD: "  indented code\n\n# Vítej\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.input.validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate(%+v) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.RegistrationSecret != tt.wantSecret {
				t.Errorf("secret = %q, want %q", got.RegistrationSecret, tt.wantSecret)
			}
			if got.WelcomeMarkdown != tt.wantMD {
				t.Errorf("welcome = %q, want %q", got.WelcomeMarkdown, tt.wantMD)
			}
			if got.RegistrationEnabled != tt.input.RegistrationEnabled {
				t.Errorf("enabled = %v, want %v", got.RegistrationEnabled, tt.input.RegistrationEnabled)
			}
		})
	}
}

// TestUpdate_validateFooter covers the footer line: trimmed when sent, kept
// absent (nil) when not, refused when it breaks a line or runs too long. The
// length counts characters, not bytes, so a Czech line is not cut short.
func TestUpdate_validateFooter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		footer  *string
		wantErr error
		want    *string
	}{
		{name: "absent stays absent", footer: nil, want: nil},
		{name: "trimmed", footer: new("  Provozuje SDH Veselice \t"), want: new("Provozuje SDH Veselice")},
		{name: "empty clears", footer: new("   "), want: new("")},
		{name: "a line break is refused", footer: new("první\ndruhý"), wantErr: ErrFooterMultiline},
		{name: "a carriage return is refused", footer: new("první\rdruhý"), wantErr: ErrFooterMultiline},
		{name: "exactly the cap passes", footer: new(strings.Repeat("ž", MaxFooterRunes)),
			want: new(strings.Repeat("ž", MaxFooterRunes))},
		{name: "over the cap is refused", footer: new(strings.Repeat("ž", MaxFooterRunes+1)),
			wantErr: ErrFooterTooLong},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Update{FooterText: tt.footer}.validate()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			switch {
			case tt.want == nil && got.FooterText != nil:
				t.Errorf("footer = %q, want it left absent", *got.FooterText)
			case tt.want != nil && (got.FooterText == nil || *got.FooterText != *tt.want):
				t.Errorf("footer = %v, want %q", got.FooterText, *tt.want)
			}
		})
	}
}

// TestNullableUID maps an empty UID to SQL NULL and passes anything else through.
func TestNullableUID(t *testing.T) {
	t.Parallel()

	if got := nullableUID(""); got != nil {
		t.Errorf("nullableUID(\"\") = %v, want nil", got)
	}
	if got := nullableUID("us_abc"); got != "us_abc" {
		t.Errorf("nullableUID(\"us_abc\") = %v, want \"us_abc\"", got)
	}
}
