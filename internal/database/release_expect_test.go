package database_test

import (
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// operationsDoc is the runbook that carries the release-day expect list.
const operationsDoc = "../../docs/OPERATIONS.md"

// releaseExpectMarker opens the runbook's one authoritative inventory-diff
// expect list: `<!-- release-expect: 0099..0116 -->`, the schema production
// runs and the newest migration the list was proven against, followed by the
// fenced command.
var releaseExpectMarker = regexp.MustCompile(`<!-- release-expect: (\d{4})\.\.(\d{4}) -->`)

// expectArg is one `--expect <value>` of the command, the value bare or in
// single quotes.
var expectArg = regexp.MustCompile(`--expect\s+(?:'([^']*)'|(\S+))`)

// releaseExpectList is the runbook's expect list: the range it was proven for
// and the --expect values of its command, in the order written.
type releaseExpectList struct {
	from, to int
	expects  []string
}

// readReleaseExpectList parses the marked expect list out of the runbook,
// failing the test when the marker or its command is missing.
func readReleaseExpectList(t *testing.T) releaseExpectList {
	t.Helper()
	body, err := os.ReadFile(operationsDoc)
	if err != nil {
		t.Fatalf("reading %s: %v", operationsDoc, err)
	}
	doc := string(body)
	loc := releaseExpectMarker.FindStringSubmatchIndex(doc)
	if loc == nil {
		t.Fatalf("%s has no `<!-- release-expect: NNNN..NNNN -->` marker", operationsDoc)
	}
	from, _ := strconv.Atoi(doc[loc[2]:loc[3]])
	to, _ := strconv.Atoi(doc[loc[4]:loc[5]])
	_, block, found := strings.Cut(doc[loc[1]:], "```sh\n")
	if !found {
		t.Fatalf("%s: no ```sh block after the release-expect marker", operationsDoc)
	}
	block, _, found = strings.Cut(block, "```")
	if !found {
		t.Fatalf("%s: the release-expect block is not closed", operationsDoc)
	}
	list := releaseExpectList{from: from, to: to}
	for _, m := range expectArg.FindAllStringSubmatch(block, -1) {
		list.expects = append(list.expects, m[1]+m[2])
	}
	if len(list.expects) == 0 {
		t.Fatalf("%s: the release-expect block names no --expect", operationsDoc)
	}
	return list
}

// newestMigration is the highest version among the embedded migration files.
func newestMigration(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("listing migrations: %v", err)
	}
	newest := 0
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if v, err := strconv.Atoi(prefix); err == nil {
			newest = max(newest, v)
		}
	}
	return newest
}

// TestReleaseExpectList_coversTheNewestMigration keeps the runbook's release-day
// expect list from going stale silently: the marker names the newest migration
// it was proven against, so adding a migration fails here until someone
// classifies what it adds to `inventory-diff` (a table, a column, nothing),
// updates the list and its marker, and lets
// TestReleaseExpectList_provenOnTheRealPath (integration) prove it again.
func TestReleaseExpectList_coversTheNewestMigration(t *testing.T) {
	list := readReleaseExpectList(t)
	if newest := newestMigration(t); list.to != newest {
		t.Errorf("the release-day expect list in %s was proven up to %04d, but the newest migration is %04d: "+
			"add what the new migration(s) add to inventory-diff to the list, set the marker to %04d..%04d and "+
			"run TestReleaseExpectList_provenOnTheRealPath (-tags integration)",
			operationsDoc, list.to, newest, list.from, newest)
	}
	if list.from != 99 {
		t.Errorf("the release-day expect list starts at %04d, want 0099 (production's schema, 0.31.0)", list.from)
	}
	if slices.Contains(list.expects, "column-removed:users.*") {
		t.Error("the release-day expect list allows every users column to vanish; name the dropped ones")
	}
}
