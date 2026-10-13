package tenancyguard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// AllowEntry is one line of allow.txt: a statement deliberately left unscoped.
type AllowEntry struct {
	// Key is the `path:line` of the finding it allows.
	Key string
	// Reason says why the statement may span every project.
	Reason string
	// Line is the entry's own line in the allow-list, for error messages.
	Line int
}

// ErrAllowList reports a malformed allow-list line.
var ErrAllowList = errors.New("malformed allow-list")

// allowLine matches `path:line reason`.
var allowLine = regexp.MustCompile(`^(\S+\.go:\d+)\s+(\S.*)$`)

// ParseAllowList reads an allow-list: one `path:line reason` per line, blank
// lines and `#` comments ignored. A line without a reason, with a malformed key
// or repeating an earlier key is an ErrAllowList error.
func ParseAllowList(r io.Reader) ([]AllowEntry, error) {
	var entries []AllowEntry
	seen := make(map[string]int)
	scanner := bufio.NewScanner(r)
	n := 0
	for scanner.Scan() {
		n++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := allowLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("%w: line %d: want `path:line reason`, got %q", ErrAllowList, n, line)
		}
		if prev, dup := seen[m[1]]; dup {
			return nil, fmt.Errorf("%w: line %d: %s already allowed on line %d", ErrAllowList, n, m[1], prev)
		}
		seen[m[1]] = n
		entries = append(entries, AllowEntry{Key: m[1], Reason: m[2], Line: n})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read allow-list: %w", err)
	}
	return entries, nil
}

// Judge splits the findings against the allow-list: the findings no entry
// allows, and the stale entries whose line no longer holds an unscoped
// statement (so the list cannot quietly outlive the code it excuses).
func Judge(findings []Finding, entries []AllowEntry) (unallowed []Finding, stale []AllowEntry) {
	allowed := make(map[string]bool, len(entries))
	for _, e := range entries {
		allowed[e.Key] = false
	}
	for _, f := range findings {
		if _, ok := allowed[f.Key()]; ok {
			allowed[f.Key()] = true
			continue
		}
		unallowed = append(unallowed, f)
	}
	for _, e := range entries {
		if !allowed[e.Key] {
			stale = append(stale, e)
		}
	}
	return unallowed, stale
}
