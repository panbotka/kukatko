package auth

import (
	"errors"
	"strings"
	"testing"
)

// TestDerivedUsername verifies the username an address suggests: its local
// part, lower case, only the safe characters, trimmed, capped with room for a
// numeric suffix, and a fallback when nothing is left.
func TestDerivedUsername(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", MaxUsernameLen+10)
	tests := []struct {
		name, email, want string
	}{
		{"plain", "jana@example.com", "jana"},
		{"case and dots", "Jana.Novakova@example.com", "jana.novakova"},
		{"plus tag dropped", "jana+fotky@example.com", "janafotky"},
		{"accents dropped", "žofie@example.com", "ofie"},
		{"separators trimmed", "-_jana._@example.com", "jana"},
		{"nothing usable", "ž+@example.com", derivedUsernameFallback},
		{"capped", long + "@example.com", strings.Repeat("a", MaxUsernameLen-2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := derivedUsername(tt.email); got != tt.want {
				t.Errorf("derivedUsername(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}

// TestNormalizeFirstAdmin verifies the first admin's input is trimmed and
// validated: a bad address and an over-long username are refused.
func TestNormalizeFirstAdmin(t *testing.T) {
	t.Parallel()
	got, err := normalizeFirstAdmin(FirstAdminInput{
		Email: "  jana@Example.COM ", DisplayName: " Jana ", Username: " Jana ",
	})
	if err != nil {
		t.Fatalf("normalizeFirstAdmin: %v", err)
	}
	want := FirstAdminInput{Email: "jana@example.com", DisplayName: "Jana", Username: "jana"}
	if got != want {
		t.Errorf("normalizeFirstAdmin = %+v, want %+v", got, want)
	}
	if _, err := normalizeFirstAdmin(FirstAdminInput{Email: "nope"}); !errors.Is(err, ErrInvalidEmail) {
		t.Errorf("bad address error = %v, want ErrInvalidEmail", err)
	}
	long := strings.Repeat("x", MaxUsernameLen+1)
	_, err = normalizeFirstAdmin(FirstAdminInput{Email: "a@example.com", Username: long})
	if !errors.Is(err, ErrUsernameTooLong) {
		t.Errorf("long username error = %v, want ErrUsernameTooLong", err)
	}
}

// TestProjectSetup_links verifies the set-password link carries the project as
// a query hint and the project link falls back to the site-relative path.
func TestProjectSetup_links(t *testing.T) {
	t.Parallel()
	ps := NewProjectSetup(ProjectSetupConfig{LinkBase: "https://k.example/password-reset/"})
	if got, want := ps.link("tok", "rodina"), "https://k.example/password-reset/tok?project=rodina"; got != want {
		t.Errorf("link = %q, want %q", got, want)
	}
	if got := ps.projectLink("rodina"); got != "/p/rodina/" {
		t.Errorf("projectLink without a base = %q, want /p/rodina/", got)
	}
	ps = NewProjectSetup(ProjectSetupConfig{ProjectURL: func(slug string) string { return "https://k.example/p/" + slug + "/" }})
	if got := ps.projectLink("rodina"); got != "https://k.example/p/rodina/" {
		t.Errorf("projectLink = %q, want the configured address", got)
	}
	if ps.mail.Delivers("a@example.com") {
		t.Error("a setup without mail claims to deliver")
	}
}
