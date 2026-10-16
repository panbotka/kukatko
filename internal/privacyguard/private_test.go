package privacyguard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePatterns writes a private pattern file and returns its path.
func writePatterns(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "privacy-patterns.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write patterns: %v", err)
	}
	return path
}

func TestLoadPrivateRules_missingFile(t *testing.T) {
	t.Parallel()
	rules, found, err := LoadPrivateRules(filepath.Join(t.TempDir(), "absent.txt"))
	if err != nil || found || rules != nil {
		t.Errorf("LoadPrivateRules(absent) = %v, %v, %v; want nil, false, nil", rules, found, err)
	}
}

func TestLoadPrivateRules_namesRulesByLine(t *testing.T) {
	t.Parallel()
	path := writePatterns(t, "# header\n\nsecret-hostname-[0-9]+\n  # indented comment\n(?i)codename\n")
	rules, found, err := LoadPrivateRules(path)
	if err != nil || !found {
		t.Fatalf("LoadPrivateRules = %v, %v; want found, no error", found, err)
	}
	if len(rules) != 2 || rules[0].Name != "private pattern #3" || rules[1].Name != "private pattern #5" {
		t.Fatalf("rules = %+v, want #3 and #5", rules)
	}
	text := "clean\nreach secret-hostname-42 now\nthe CodeName is out\n"
	got := CheckText("docs/specs/task.md", []byte(text), rules)
	if len(got) != 2 || got[0].String() != "docs/specs/task.md:2: private pattern #3" ||
		got[1].String() != "docs/specs/task.md:3: private pattern #5" {
		t.Errorf("findings = %v, want lines 2 and 3 by pattern number", got)
	}
	for _, f := range got {
		if strings.Contains(f.String(), "hostname") || strings.Contains(f.String(), "codename") {
			t.Errorf("finding %q prints the private pattern", f)
		}
	}
}

func TestLoadPrivateRules_invalidPatternHidesIt(t *testing.T) {
	t.Parallel()
	path := writePatterns(t, "fine\nunclosed-(secretvalue\n")
	_, _, err := LoadPrivateRules(path)
	if !errors.Is(err, ErrPrivatePattern) {
		t.Fatalf("err = %v, want ErrPrivatePattern", err)
	}
	if !strings.Contains(err.Error(), "line 2") || strings.Contains(err.Error(), "secretvalue") {
		t.Errorf("err = %q, want the line number and never the pattern", err)
	}
}
