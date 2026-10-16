package privacyguard

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// PrivatePatternsPath is the gitignored file of private patterns, relative to
// the repository root: one regular expression per line, `#` lines comments.
const PrivatePatternsPath = ".secrets/privacy-patterns.txt"

// ErrPrivatePattern reports a line of the private pattern file that is not a
// valid regular expression. Its message names the line, never the pattern.
var ErrPrivatePattern = errors.New("invalid private pattern")

// LoadPrivateRules reads the private patterns at path into rules named
// `private pattern #<line>` — the line in that file, the only thing about a
// private rule the guard ever prints. A missing file is not an error: it
// returns found == false, and the caller runs the generic rules alone.
// Patterns use Go's RE2 syntax, which covers the extended regular expressions
// `grep -E` takes.
func LoadPrivateRules(path string) (rules []Rule, found bool, err error) {
	f, err := os.Open(path) //nolint:gosec // the path is the gate's own pattern file
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open private patterns: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	n := 0
	for scanner.Scan() {
		n++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile(line)
		if err != nil {
			// Not wrapped: regexp's message quotes the pattern.
			return nil, true, fmt.Errorf("%w: line %d", ErrPrivatePattern, n)
		}
		rules = append(rules, Rule{
			Name:    "private pattern #" + strconv.Itoa(n),
			Reason:  "a value listed in " + PrivatePatternsPath,
			Pattern: re,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, true, fmt.Errorf("read private patterns: %w", err)
	}
	return rules, true, nil
}
