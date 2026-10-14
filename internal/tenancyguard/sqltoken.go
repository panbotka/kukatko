package tenancyguard

import "strings"

// tokenKind is the kind of one SQL token.
type tokenKind int

const (
	// tokWord is an identifier, a keyword or a number.
	tokWord tokenKind = iota
	// tokParam is a positional parameter, `$1`.
	tokParam
	// tokString is a quoted '…' literal.
	tokString
	// tokOp is an operator run (`=`, `<>`, `::`) or one punctuation byte (`.`, `,`).
	tokOp
	// tokMark is projectMark: an opaque piece named after the project.
	tokMark
	// tokGroup is a parenthesised group; its tokens are in inner.
	tokGroup
)

// sqlToken is one token of a statement, located by its byte offsets in it.
type sqlToken struct {
	kind tokenKind
	// text is the word (a quoted identifier without its quotes) or operator.
	text string
	// upper is text in upper case, for matching keywords.
	upper      string
	start, end int
	inner      []sqlToken
}

// is reports whether t is the keyword kw, in any case.
func (t sqlToken) is(kw string) bool {
	return t.kind == tokWord && t.upper == kw
}

// isOp reports whether t is exactly the operator op.
func (t sqlToken) isOp(op string) bool {
	return t.kind == tokOp && t.text == op
}

// opBytes are the bytes that run together into one operator token, so `<=`
// is never read as an `=`.
const opBytes = "+-*/<>=~!@#%^&|?:"

// blankComments returns stmt with every SQL comment (`-- …` to the end of
// the line, `/* … */`) replaced by spaces of the same length, so offsets into
// it still locate the source and a project column inside a comment names
// nothing. Quoted '…' text is left alone; newlines are kept.
func blankComments(stmt string) string {
	b := []byte(stmt)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\'':
			end := strings.IndexByte(stmt[i+1:], '\'')
			if end < 0 {
				return string(b)
			}
			i += end + 1
		case strings.HasPrefix(stmt[i:], "--") || strings.HasPrefix(stmt[i:], "/*"):
			end := commentEnd(stmt, i)
			for k := i; k < end; k++ {
				if b[k] != '\n' {
					b[k] = ' '
				}
			}
			i = end - 1
		}
	}
	return string(b)
}

// commentEnd returns the offset past the comment that starts at i: the end of
// its line for `--`, past the closing `*/` (or the end) for `/*`.
func commentEnd(stmt string, i int) int {
	if stmt[i+1] == '-' {
		if j := strings.IndexByte(stmt[i:], '\n'); j >= 0 {
			return i + j
		}
		return len(stmt)
	}
	if j := strings.Index(stmt[i+2:], "*/"); j >= 0 {
		return i + 2 + j + 2
	}
	return len(stmt)
}

// tokenize splits stmt from offset i into tokens, nesting each parenthesised
// group. Inside a group (nested) it stops after the closing `)` and returns
// the offset past it; an unbalanced `)` at the top is skipped.
func tokenize(stmt string, i int, nested bool) ([]sqlToken, int) {
	var toks []sqlToken
	for i < len(stmt) {
		c := stmt[i]
		switch {
		case strings.HasPrefix(stmt[i:], projectMark):
			toks = append(toks, sqlToken{kind: tokMark, start: i, end: i + len(projectMark)})
			i += len(projectMark)
		case isSpace(c):
			i++
		case c == '(':
			inner, next := tokenize(stmt, i+1, true)
			toks = append(toks, sqlToken{kind: tokGroup, start: i, end: next, inner: inner})
			i = next
		case c == ')':
			if nested {
				return toks, i + 1
			}
			i++
		default:
			var t sqlToken
			t, i = scanToken(stmt, i)
			toks = append(toks, t)
		}
	}
	return toks, i
}

// scanToken reads the one non-group token of stmt at i and returns it with the
// offset after it.
func scanToken(stmt string, i int) (sqlToken, int) {
	c := stmt[i]
	end := i + 1
	kind := tokOp
	switch {
	case c == '\'' || c == '"':
		return scanQuoted(stmt, i)
	case c == '$' && end < len(stmt) && isDigit(stmt[end]):
		end = skip(stmt, end, isDigit)
		kind = tokParam
	case isWordByte(c):
		end = skip(stmt, end, isWordByte)
		kind = tokWord
	case strings.IndexByte(opBytes, c) >= 0:
		end = skip(stmt, end, func(b byte) bool { return strings.IndexByte(opBytes, b) >= 0 })
	}
	text := stmt[i:end]
	return sqlToken{kind: kind, text: text, upper: strings.ToUpper(text), start: i, end: end}, end
}

// scanQuoted reads the quoted token at i: a '…' literal, or a "…" identifier
// (a word, without its quotes). An unterminated one runs to the end.
func scanQuoted(stmt string, i int) (sqlToken, int) {
	end := len(stmt)
	if j := strings.IndexByte(stmt[i+1:], stmt[i]); j >= 0 {
		end = i + j + 2
	}
	if stmt[i] == '\'' {
		return sqlToken{kind: tokString, start: i, end: end}, end
	}
	word := strings.Trim(stmt[i:end], `"`)
	return sqlToken{kind: tokWord, text: word, upper: strings.ToUpper(word), start: i, end: end}, end
}

// skip returns the offset of the first byte from i on that is not in the class.
func skip(stmt string, i int, in func(byte) bool) int {
	for i < len(stmt) && in(stmt[i]) {
		i++
	}
	return i
}

// isSpace reports whether c is SQL whitespace.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isWordByte reports whether c belongs to a SQL identifier or keyword.
func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || isDigit(c)
}

// isDigit reports whether c is an ASCII digit.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
