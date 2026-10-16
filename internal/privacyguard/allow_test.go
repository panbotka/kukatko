package privacyguard

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseAllowList(t *testing.T) {
	t.Parallel()
	src := "# header\n\ndocs/a.md:12 synthetic fixture\n  web/src/b.test.ts:3   placeholder  \n"
	got, err := ParseAllowList(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}
	want := []AllowEntry{
		{Key: "docs/a.md:12", Reason: "synthetic fixture", Line: 3},
		{Key: "web/src/b.test.ts:3", Reason: "placeholder", Line: 4},
	}
	if !slices.Equal(got, want) {
		t.Errorf("entries = %+v, want %+v", got, want)
	}
}

func TestParseAllowList_rejects(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		src  string
	}{
		{name: "missing reason", src: "docs/a.md:1\n"},
		{name: "missing line", src: "docs/a.md reason\n"},
		{name: "duplicate key", src: "docs/a.md:1 one\ndocs/a.md:1 two\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseAllowList(strings.NewReader(tt.src)); !errors.Is(err, ErrAllowList) {
				t.Errorf("err = %v, want ErrAllowList", err)
			}
		})
	}
}

func TestJudge(t *testing.T) {
	t.Parallel()
	findings := []Finding{
		{Path: "docs/a.md", Line: 1, Rule: "ssh-target"},
		{Path: "docs/a.md", Line: 1, Rule: "email-address"},
		{Path: "docs/a.md", Line: 2, Rule: "phone-number"},
	}
	entries := []AllowEntry{
		{Key: "docs/a.md:1", Reason: "allowed", Line: 1},
		{Key: "docs/a.md:9", Reason: "stale", Line: 2},
	}
	unallowed, stale := Judge(findings, entries)
	if len(unallowed) != 1 || unallowed[0].String() != "docs/a.md:2: phone-number" {
		t.Errorf("unallowed = %v, want docs/a.md:2: phone-number", unallowed)
	}
	if len(stale) != 1 || stale[0].Key != "docs/a.md:9" {
		t.Errorf("stale = %v, want docs/a.md:9", stale)
	}
}
