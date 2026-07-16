package query

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// tchar is one rune of a token together with its literalness: a rune that
// arrived escaped with '\' or inside double quotes is literal and never acts
// as an operator (':', '|', '!', '-', '*').
type tchar struct {
	r   rune
	lit bool
}

// token is one whitespace-delimited unit of the input: its analysed runes plus
// the raw source text (used verbatim when the token degrades to free text and
// when it is reported as not understood).
type token struct {
	raw    string
	chars  []tchar
	quoted bool
}

// Parse parses a search input into free-text terms, recognised filters and
// unknown filter-shaped tokens. It never returns an error: malformed or
// unrecognised filters degrade to free text (and are reported in Unknown), so
// a stray colon in a caption still finds the photo.
func Parse(input string) Query {
	var q Query
	runes := []rune(input)
	i := 0
	for i < len(runes) {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}
		tok, next := scanToken(runes, i)
		analyze(tok, &q)
		i = next
	}
	return q
}

// scanToken consumes one token starting at start, honouring double quotes
// (their content is literal, quotes themselves are dropped) and backslash
// escapes (the next rune becomes literal). It returns the token and the index
// right after it. An unterminated quote closes at the end of the input.
func scanToken(runes []rune, start int) (token, int) {
	var chars []tchar
	quoted, inQuote := false, false
	i := start
	for i < len(runes) {
		r := runes[i]
		if r == '\\' && i+1 < len(runes) {
			chars = append(chars, tchar{runes[i+1], true})
			i += 2
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			quoted = true
			i++
			continue
		}
		if !inQuote && unicode.IsSpace(r) {
			break
		}
		chars = append(chars, tchar{r, inQuote})
		i++
	}
	return token{raw: string(runes[start:i]), chars: chars, quoted: quoted}, i
}

// analyze classifies one token and appends it to the query: a recognised
// key:value filter, a free-text term, or — for a filter-shaped token that was
// not understood — a free-text term plus an entry in Unknown.
func analyze(tok token, q *Query) {
	if len(tok.chars) == 0 {
		return
	}
	key, valChars, isFilter := splitFilter(tok)
	if !isFilter {
		if t, ok := freeTerm(tok); ok {
			q.Terms = append(q.Terms, t)
		}
		return
	}
	canonical, known := lookupKey(key)
	if !known {
		degrade(q, tok)
		return
	}
	values, ok := parseValues(canonical, valChars)
	if !ok {
		degrade(q, tok)
		return
	}
	q.Filters = append(q.Filters, Filter{Key: canonical, Values: values})
}

// degrade records a filter-shaped token the parser did not understand: the
// token joins the free text (so it can still match a caption verbatim) and its
// raw form is reported in Unknown so the UI can hint at it.
func degrade(q *Query, tok token) {
	q.Unknown = append(q.Unknown, tok.raw)
	text := charsText(tok.chars)
	q.Terms = append(q.Terms, Term{Text: text, Phrase: strings.ContainsRune(text, ' ')})
}

// splitFilter splits a token at its first operator colon into a lowercased
// filter-key candidate and the value runes. A token is filter-shaped only when
// such a colon exists, at least one rune precedes it, and every key rune is an
// unescaped ASCII letter — so "title:x" qualifies while "12:30", "\:x" and a
// fully quoted "a:b" do not.
func splitFilter(tok token) (string, []tchar, bool) {
	for i, c := range tok.chars {
		if c.r != ':' || c.lit {
			continue
		}
		if i == 0 {
			return "", nil, false
		}
		key := make([]rune, 0, i)
		for _, kc := range tok.chars[:i] {
			if kc.lit || !isKeyRune(kc.r) {
				return "", nil, false
			}
			key = append(key, kc.r)
		}
		return strings.ToLower(string(key)), tok.chars[i+1:], true
	}
	return "", nil, false
}

// isKeyRune reports whether r may appear in a filter key: ASCII letters only.
func isKeyRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// freeTerm turns a non-filter token into a free-text term, honouring the
// leading '-' negation and the quoted-phrase form. ok is false for a token
// that holds no text (e.g. a lone '-').
func freeTerm(tok token) (Term, bool) {
	chars := tok.chars
	not := false
	if len(chars) > 1 && chars[0].r == '-' && !chars[0].lit {
		not = true
		chars = chars[1:]
	}
	text := charsText(chars)
	if strings.TrimSpace(text) == "" {
		return Term{}, false
	}
	return Term{Text: text, Phrase: tok.quoted, Not: not}, true
}

// parseValues splits a filter's value on its unescaped '|' separators and
// parses each alternative against the key's spec. ok is false when the value
// is empty or any alternative is malformed, in which case the whole token
// degrades to free text.
func parseValues(key Key, chars []tchar) ([]Value, bool) {
	alts := splitAlternatives(chars)
	values := make([]Value, 0, len(alts))
	for _, alt := range alts {
		v, ok := parseAlternative(key, alt)
		if !ok {
			return nil, false
		}
		values = append(values, v)
	}
	return values, true
}

// splitAlternatives splits value runes on unescaped '|' into the OR-ed
// alternatives. It always returns at least one (possibly empty) alternative.
func splitAlternatives(chars []tchar) [][]tchar {
	var alts [][]tchar
	cur := make([]tchar, 0, len(chars))
	for _, c := range chars {
		if c.r == '|' && !c.lit {
			alts = append(alts, cur)
			cur = nil
			continue
		}
		cur = append(cur, c)
	}
	return append(alts, cur)
}

// parseAlternative parses one OR-alternative: an optional leading unescaped
// '!' negation followed by a value of the key's kind. ok is false when the
// alternative is empty or its value does not fit the kind.
func parseAlternative(key Key, chars []tchar) (Value, bool) {
	not := false
	if len(chars) > 0 && chars[0].r == '!' && !chars[0].lit {
		not = true
		chars = chars[1:]
	}
	if len(chars) == 0 {
		return Value{}, false
	}
	v, ok := parseKindValue(specs[key], chars)
	if !ok {
		return Value{}, false
	}
	v.Not = not
	return v, true
}

// parseKindValue parses the alternative's runes according to the spec's kind.
func parseKindValue(sp spec, chars []tchar) (Value, bool) {
	switch sp.kind {
	case KindText, KindID:
		return Value{Text: charsText(chars)}, true
	case KindBool:
		return parseBoolValue(charsText(chars))
	case KindEnum:
		return parseEnumValue(sp, charsText(chars))
	case KindNumber:
		return parseNumberValue(sp, chars)
	case KindDate:
		return parseDateValue(charsText(chars))
	case KindCount:
		return parseCountValue(sp, chars)
	default:
		return Value{}, false
	}
}

// parseBoolValue parses a yes/no word (also true/false, 1/0), case-
// insensitively.
func parseBoolValue(text string) (Value, bool) {
	switch strings.ToLower(text) {
	case "yes", "true", "1":
		b := true
		return Value{Bool: &b}, true
	case "no", "false", "0":
		b := false
		return Value{Bool: &b}, true
	default:
		return Value{}, false
	}
}

// parseEnumValue matches the value against the spec's fixed word set,
// case-insensitively, storing the canonical lowercased word.
func parseEnumValue(sp spec, text string) (Value, bool) {
	word := strings.ToLower(text)
	if slices.Contains(sp.enum, word) {
		return Value{Text: word}, true
	}
	return Value{}, false
}

// parseNumberValue parses a number or a lo-hi range (either end may be open:
// "800-", "-200"). A single value sets both bounds; a reversed range is
// normalised. Bounds must fit the spec's limits and, when the spec demands
// integers, be whole numbers.
func parseNumberValue(sp spec, chars []tchar) (Value, bool) {
	lo, hi, _, ok := splitRange(chars)
	if !ok || (lo == "" && hi == "") {
		return Value{}, false
	}
	var v Value
	if v.Min, ok = optionalBound(sp, lo); !ok {
		return Value{}, false
	}
	if v.Max, ok = optionalBound(sp, hi); !ok {
		return Value{}, false
	}
	if v.Min != nil && v.Max != nil && *v.Min > *v.Max {
		v.Min, v.Max = v.Max, v.Min
	}
	return v, true
}

// optionalBound parses one side of a range: an empty side is a valid open
// bound (nil), anything else must parse and validate as a number.
func optionalBound(sp spec, raw string) (*float64, bool) {
	if raw == "" {
		// An open range end is legitimately absent: nil means "no bound".
		return nil, true
	}
	n, ok := parseBound(sp, raw)
	if !ok {
		return nil, false
	}
	return &n, true
}

// parseCountValue parses a KindCount value: the yes/no words, or a numeric
// form where a bare number is a minimum ("faces:3" means at least three) and a
// range bounds both ends.
func parseCountValue(sp spec, chars []tchar) (Value, bool) {
	if v, ok := parseBoolValue(charsText(chars)); ok {
		return v, true
	}
	lo, hi, ranged, ok := splitRange(chars)
	if !ok || (lo == "" && hi == "") {
		return Value{}, false
	}
	v, ok := parseNumberValue(sp, chars)
	if !ok {
		return Value{}, false
	}
	if !ranged && lo == hi {
		// A bare count is a minimum, not an exact match.
		v.Max = nil
	}
	return v, true
}

// splitRange splits a numeric value on its unescaped range dash. Without a
// dash both bounds return the whole text and ranged is false; with one dash
// the (possibly empty) sides become the bounds. ok is false when the dash
// appears more than once.
func splitRange(chars []tchar) (lo, hi string, ranged, ok bool) {
	dash := -1
	for i, c := range chars {
		if c.r != '-' || c.lit {
			continue
		}
		if dash >= 0 {
			return "", "", false, false
		}
		dash = i
	}
	if dash < 0 {
		s := charsText(chars)
		return s, s, false, true
	}
	return charsText(chars[:dash]), charsText(chars[dash+1:]), true, true
}

// parseBound parses one numeric bound and validates it against the spec's
// limits and integer requirement.
func parseBound(sp spec, raw string) (float64, bool) {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, false
	}
	if n < sp.lo || n > sp.hi {
		return 0, false
	}
	if sp.integer && n != math.Trunc(n) {
		return 0, false
	}
	return n, true
}

// dateLayouts are the accepted calendar forms, most specific first, each with
// the step that yields the exclusive end of the half-open range it denotes.
var dateLayouts = []struct {
	layout string
	next   func(t time.Time) time.Time
}{
	{"2006-01-02", func(t time.Time) time.Time { return t.AddDate(0, 0, 1) }},
	{"2006-01", func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }},
	{"2006", func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }},
}

// parseDateValue parses a calendar value (YYYY, YYYY-MM or YYYY-MM-DD, in UTC)
// into the half-open [From, Until) range it covers.
func parseDateValue(text string) (Value, bool) {
	for _, dl := range dateLayouts {
		t, err := time.Parse(dl.layout, text)
		if err != nil {
			continue
		}
		from := t.UTC()
		until := dl.next(from)
		return Value{From: &from, Until: &until}, true
	}
	return Value{}, false
}

// charsText returns the plain string of the runes, dropping literalness.
func charsText(chars []tchar) string {
	runes := make([]rune, len(chars))
	for i, c := range chars {
		runes[i] = c.r
	}
	return string(runes)
}
