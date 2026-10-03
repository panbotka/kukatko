package uploadlink

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestNewCode_shape verifies a code has the documented length, only alphabet
// symbols, and that fresh codes differ.
func TestNewCode_shape(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for range 200 {
		code, err := NewCode()
		if err != nil {
			t.Fatalf("NewCode: %v", err)
		}
		if !ValidCode(code) {
			t.Fatalf("NewCode() = %q, which ValidCode refuses", code)
		}
		if seen[code] {
			t.Fatalf("NewCode() repeated %q", code)
		}
		seen[code] = true
	}
}

// TestCodeAlphabet_unambiguous pins that no glyph a person could misread made
// it into the alphabet, and that the rejection bound is a whole multiple of it.
func TestCodeAlphabet_unambiguous(t *testing.T) {
	t.Parallel()

	if strings.ContainsAny(codeAlphabet, "0O1lIo") {
		t.Errorf("codeAlphabet %q contains an ambiguous glyph", codeAlphabet)
	}
	if codeRejectAbove%len(codeAlphabet) != 0 || codeRejectAbove > 256 {
		t.Errorf("codeRejectAbove = %d is not a multiple of %d within a byte", codeRejectAbove, len(codeAlphabet))
	}
}

// TestValidCode covers the shape check that spares a lookup.
func TestValidCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want bool
	}{
		{"valid", "Ab3dEf7h", true},
		{"too short", "Ab3dEf7", false},
		{"too long", "Ab3dEf7hj", false},
		{"empty", "", false},
		{"ambiguous zero", "Ab3dEf70", false},
		{"punctuation", "Ab3dEf7/", false},
		{"non-ascii", "Ab3dEfžh", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidCode(tt.code); got != tt.want {
				t.Errorf("ValidCode(%q) = %v, want %v", tt.code, got, tt.want)
			}
		})
	}
}

// TestHashSecret verifies the digest is deterministic, hex and distinct per input.
func TestHashSecret(t *testing.T) {
	t.Parallel()

	a, b := HashSecret("Ab3dEf7h"), HashSecret("Ab3dEf7j")
	if a != HashSecret("Ab3dEf7h") {
		t.Error("HashSecret is not deterministic")
	}
	if a == b {
		t.Error("HashSecret collides on different inputs")
	}
	if len(a) != 64 || strings.Trim(a, "0123456789abcdef") != "" {
		t.Errorf("HashSecret = %q, want 64 lowercase hex characters", a)
	}
}

// TestNewSessionToken verifies the token's length and freshness.
func TestNewSessionToken(t *testing.T) {
	t.Parallel()

	first, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	second, err := NewSessionToken()
	if err != nil {
		t.Fatalf("NewSessionToken: %v", err)
	}
	if len(first) != 43 {
		t.Errorf("len(token) = %d, want 43 (32 bytes, base64url)", len(first))
	}
	if first == second {
		t.Error("NewSessionToken repeated a token")
	}
}

// TestNormalizeUploaderName covers trimming, whitespace collapse, the cap and
// invalid UTF-8.
func TestNormalizeUploaderName(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("ž", MaxUploaderNameLen+5)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"trimmed", "  Jana  ", "Jana"},
		{"collapsed", "Jana\t \n Nováková", "Jana Nováková"},
		{"invalid utf-8 dropped", "Ja\xffna", "Jana"},
		{"capped", long, strings.Repeat("ž", MaxUploaderNameLen)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := NormalizeUploaderName(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeUploaderName(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if utf8.RuneCountInString(got) > MaxUploaderNameLen {
				t.Errorf("result has %d runes, over the cap", utf8.RuneCountInString(got))
			}
		})
	}
}
