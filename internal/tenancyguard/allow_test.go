package tenancyguard

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestParseAllowList(t *testing.T) {
	t.Parallel()
	src := "# header\n\ninternal/a/b.go:12 worker loader, no request project\n  internal/c.go:3   maintenance scan  \n"
	got, err := ParseAllowList(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}
	want := []AllowEntry{
		{Key: "internal/a/b.go:12", Reason: "worker loader, no request project", Line: 3},
		{Key: "internal/c.go:3", Reason: "maintenance scan", Line: 4},
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
		{name: "missing reason", src: "internal/a.go:1\n"},
		{name: "missing line", src: "internal/a.go reason\n"},
		{name: "not a go file", src: "internal/a.sql:1 reason\n"},
		{name: "duplicate key", src: "internal/a.go:1 one\ninternal/a.go:1 two\n"},
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
		{Path: "a.go", Line: 1, Tables: []string{"photos"}},
		{Path: "a.go", Line: 2, Tables: []string{"faces"}},
	}
	entries := []AllowEntry{
		{Key: "a.go:1", Reason: "allowed", Line: 1},
		{Key: "a.go:9", Reason: "stale", Line: 2},
	}
	unallowed, stale := Judge(findings, entries)
	if len(unallowed) != 1 || unallowed[0].Key() != "a.go:2" {
		t.Errorf("unallowed = %v, want a.go:2", unallowed)
	}
	if len(stale) != 1 || stale[0].Key != "a.go:9" {
		t.Errorf("stale = %v, want a.go:9", stale)
	}
}
