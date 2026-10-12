package comments

import (
	"regexp"
	"strings"
)

// MentionScheme is the pseudo-URL scheme a mention token links to: the
// account's uid, so `@[Tomáš Kozák](user:us3f9k…)` survives a rename — the
// name in the token is only what to show once the account is gone.
const MentionScheme = "user:"

// MaxExcerptLen is how many characters of a comment a notification repeats.
const MaxExcerptLen = 140

// mentionPattern is the mention token as the composer writes it: an `@`
// straight before a Markdown link whose destination is MentionScheme + the
// uid. The name may hold anything but brackets and a line break, and the uid
// is the account uid's alphabet (letters, digits, `_` and `-`). A bare
// `@Tomáš` typed without picking from the list is deliberately not matched:
// a mention is a chosen account, never a guess at one.
var mentionPattern = regexp.MustCompile(`@\[([^\[\]\n]+)\]\(` + MentionScheme + `([A-Za-z0-9_-]{1,32})\)`)

// MentionRef is one mention token as it stands in a body: the account's uid
// and the name the token carries. It says nothing about whether the account
// exists — that is the store's resolution (Mention).
type MentionRef struct {
	UserUID string
	Name    string
}

// MentionToken returns the token the composer inserts for the account uid
// shown as name — the one syntax ParseMentions recognises. Brackets in the
// name would end the token early, so they are dropped.
func MentionToken(name, uid string) string {
	clean := strings.NewReplacer("[", "", "]", "", "\n", " ").Replace(strings.TrimSpace(name))
	return "@[" + clean + "](" + MentionScheme + uid + ")"
}

// ParseMentions returns the accounts body mentions, each once, in order of
// first mention. Only the token syntax counts (see mentionPattern), and only
// outside code: a token inside a fenced or indented code block or an inline
// code span is shown as code by the renderer and is not a mention here
// either, so the two never disagree about who was named.
func ParseMentions(body string) []MentionRef {
	matches := mentionPattern.FindAllStringSubmatch(stripCode(body), -1)
	seen := make(map[string]bool, len(matches))
	out := make([]MentionRef, 0, len(matches))
	for _, m := range matches {
		uid := m[2]
		if seen[uid] {
			continue
		}
		seen[uid] = true
		out = append(out, MentionRef{UserUID: uid, Name: strings.TrimSpace(m[1])})
	}
	return out
}

// Excerpt returns body as a notification repeats it: every mention token
// reduced to "@name", whitespace collapsed to single spaces, and the result
// cut to limit characters with an ellipsis. A limit of zero or less means
// MaxExcerptLen.
func Excerpt(body string, limit int) string {
	if limit <= 0 {
		limit = MaxExcerptLen
	}
	text := mentionPattern.ReplaceAllString(body, "@$1")
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

// stripCode returns body with every code block and code span blanked, line
// structure kept, so a token search over the result sees only prose. It
// follows CommonMark closely enough for a comment: a fence of three or more
// backticks or tildes (indented up to three spaces) runs to a closing fence of
// the same character at least as long, or to the end; an indented code block
// (four spaces or a tab) starts only after a blank line or at the top, and
// runs through indented and blank lines; a code span is a run of backticks
// closed by a run of the same length, and an unclosed run is literal.
func stripCode(body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	state := codeState{prevBlank: true}
	for i, line := range lines {
		if state.prose(line) {
			out[i] = stripCodeSpans(line)
		}
	}
	return strings.Join(out, "\n")
}

// codeState is where a line-by-line walk of a body stands with respect to
// block-level code: inside a fence, inside an indented block, and whether the
// line before was blank (an indented block may only start after one).
type codeState struct {
	fence     fenceState
	indented  bool
	prevBlank bool
}

// prose advances the state over line and reports whether the line is prose —
// outside every code block — and so may hold a mention.
func (s *codeState) prose(line string) bool {
	keep := s.classify(line)
	s.prevBlank = isBlank(line)
	return keep
}

// classify applies the block rules to line, updating the fence and indented
// states, and reports whether the line is prose.
func (s *codeState) classify(line string) bool {
	switch {
	case s.fence.open:
		if s.fence.closes(line) {
			s.fence = fenceState{}
		}
		return false
	case s.fence.opens(line):
		s.fence = openFence(line)
		return false
	case s.indented && (isIndented(line) || isBlank(line)):
		return false
	case s.prevBlank && isIndented(line):
		s.indented = true
		return false
	default:
		s.indented = false
		return true
	}
}

// fenceState is an open fenced code block: which character fenced it and how
// many of them, which the closing fence must at least match.
type fenceState struct {
	open  bool
	char  byte
	count int
}

// fenceRun returns the fence character and run length a line starts with
// (after up to three spaces of indentation), or 0 when it starts no fence.
func fenceRun(line string) (byte, int) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0
	}
	char := trimmed[0]
	n := 0
	for n < len(trimmed) && trimmed[n] == char {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	// A backtick fence's info string may not hold a backtick — such a line is a
	// code span, not a fence.
	if char == '`' && strings.Contains(trimmed[n:], "`") {
		return 0, 0
	}
	return char, n
}

// opens reports whether line opens a fenced code block.
func (fenceState) opens(line string) bool {
	_, n := fenceRun(line)
	return n > 0
}

// openFence returns the state of the fence line opens.
func openFence(line string) fenceState {
	char, n := fenceRun(line)
	return fenceState{open: true, char: char, count: n}
}

// closes reports whether line closes the open fence: the same character, at
// least as many of them, and nothing else on the line.
func (f fenceState) closes(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(line)-len(strings.TrimLeft(line, " ")) > 3 || trimmed == "" {
		return false
	}
	return strings.Trim(trimmed, string(f.char)) == "" && len(trimmed) >= f.count
}

// isIndented reports whether line starts an indented code line: a tab, or four
// spaces.
func isIndented(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

// isBlank reports whether line holds only whitespace.
func isBlank(line string) bool {
	return strings.TrimSpace(line) == ""
}

// stripCodeSpans returns line with every inline code span blanked.
func stripCodeSpans(line string) string {
	var out strings.Builder
	out.Grow(len(line))
	for i := 0; i < len(line); {
		if line[i] != '`' {
			out.WriteByte(line[i])
			i++
			continue
		}
		run := backtickRun(line, i)
		end := closingRun(line, i+run, run)
		if end < 0 {
			// Unclosed: the backticks are literal, and so is what follows.
			out.WriteString(line[i : i+run])
			i += run
			continue
		}
		i = end + run
	}
	return out.String()
}

// backtickRun returns how many backticks line holds from index i on.
func backtickRun(line string, i int) int {
	n := 0
	for i+n < len(line) && line[i+n] == '`' {
		n++
	}
	return n
}

// closingRun returns the index of the first run of exactly n backticks in
// line from index from on, or -1 when there is none.
func closingRun(line string, from, n int) int {
	for i := from; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		run := backtickRun(line, i)
		if run == n {
			return i
		}
		i += run
	}
	return -1
}
