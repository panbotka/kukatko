package privacyguard_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/privacyguard"
)

// repoRoot returns the repository root: two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// TestRepository_noPrivateData is the guard itself: no tracked text file and
// no file under docs/specs/ may match a generic rule or, where the private
// pattern file exists, a private one — unless allow.txt excuses the line.
func TestRepository_noPrivateData(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	rules := privacyguard.GenericRules()
	private, found, err := privacyguard.LoadPrivateRules(filepath.Join(root, privacyguard.PrivatePatternsPath))
	if err != nil {
		t.Fatalf("%s: %v", privacyguard.PrivatePatternsPath, err)
	}
	if found {
		t.Logf("checking %d generic and %d private rules", len(rules), len(private))
		rules = append(rules, private...)
	} else {
		t.Logf("%s is absent (CI, the build box): checking the %d generic rules only",
			privacyguard.PrivatePatternsPath, len(rules))
	}

	start := time.Now()
	files, err := privacyguard.Files(t.Context(), root)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	findings, err := privacyguard.Check(root, files, rules)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	t.Logf("scanned %d files in %s: %d findings", len(files), time.Since(start), len(findings))

	f, err := os.Open("allow.txt")
	if err != nil {
		t.Fatalf("open allow.txt: %v", err)
	}
	defer f.Close()
	entries, err := privacyguard.ParseAllowList(f)
	if err != nil {
		t.Fatalf("allow.txt: %v", err)
	}
	unallowed, stale := privacyguard.Judge(findings, entries)
	for _, u := range unallowed {
		t.Errorf("%s\n\tremove the private value (docs/OPERATIONS.md, \"Privacy guard\"), or — only if the "+
			"line is synthetic — add `%s <reason>` to internal/privacyguard/allow.txt", u, u.Key())
	}
	for _, e := range stale {
		t.Errorf("stale allow-list entry (allow.txt line %d): %s no longer holds a finding; "+
			"remove it or move it to the line's new number", e.Line, e.Key)
	}
}
