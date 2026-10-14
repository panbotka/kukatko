package tenancyguard

import (
	"slices"
	"strings"
)

// clause is the part of a statement a token sits in, as far as scoping goes.
type clause int

const (
	// clauseOther is a select list, VALUES, ordering, grouping, RETURNING and
	// the like: a project column there neither constrains nor writes the row.
	clauseOther clause = iota
	// clauseFrom is a FROM or JOIN list. A project column there names no
	// condition, but an opaque project-named piece may be a whole JOIN … ON.
	clauseFrom
	// clauseScope is the WHERE predicate, a JOIN's ON condition (ON CONFLICT
	// included, whose target follows), an INSERT's column list after INTO and
	// an UPDATE's SET list: a project column there constrains or writes the row.
	clauseScope
)

// clauseKeywords maps the keywords that open a clause to the clause they open.
var clauseKeywords = map[string]clause{
	"WHERE": clauseScope, "ON": clauseScope, "CONFLICT": clauseScope, "INTO": clauseScope, "SET": clauseScope,
	"FROM": clauseFrom, "JOIN": clauseFrom, "USING": clauseFrom,
	"SELECT": clauseOther, "VALUES": clauseOther, "RETURNING": clauseOther, "GROUP": clauseOther,
	"ORDER": clauseOther, "HAVING": clauseOther, "LIMIT": clauseOther, "OFFSET": clauseOther,
	"UNION": clauseOther, "INTERSECT": clauseOther, "EXCEPT": clauseOther, "WITH": clauseOther,
	"UPDATE": clauseOther, "DELETE": clauseOther, "INSERT": clauseOther, "WINDOW": clauseOther,
	"DO": clauseOther,
}

// clauseScan is the state of one predicateColumns pass: the clause each open
// parenthesis level is in (a nested level starts in its parent's clause, so a
// bracketed condition stays a condition and a subquery's own keywords take
// over), the previous word, and the project columns found in a scope clause.
type clauseScan struct {
	levels []clause
	prev   string
	found  map[string]bool
}

// predicateColumns returns the project columns (and keyed child tables' parent
// keys) that stmt names in a position
// that constrains or writes its rows: after WHERE, in a JOIN … ON condition, in
// an INSERT column list, in an UPDATE … SET list or in an ON CONFLICT target —
// at any depth, so a CTE's or subquery's own predicate counts. A project column
// only in a select list, ORDER BY, RETURNING or the like is not returned.
// projectMark is returned when it stands in such a position or in a FROM/JOIN
// list, where the opaque piece may itself be a join condition. It is a token-position heuristic, not a SQL parser:
// keywords are matched case-insensitively and quoted SQL strings are skipped.
func predicateColumns(stmt string) map[string]bool {
	s := clauseScan{levels: []clause{clauseOther}, found: make(map[string]bool)}
	for i := 0; i < len(stmt); {
		i = s.step(stmt, i)
	}
	return s.found
}

// step consumes the token of stmt at i and returns the offset after it.
func (s *clauseScan) step(stmt string, i int) int {
	switch c := stmt[i]; {
	case strings.HasPrefix(stmt[i:], projectMark):
		s.word(projectMark)
		return i + len(projectMark)
	case c == '\'':
		if end := strings.IndexByte(stmt[i+1:], '\''); end >= 0 {
			return i + end + 2
		}
		return len(stmt)
	case c == '(':
		s.levels = append(s.levels, s.levels[len(s.levels)-1])
	case c == ')':
		if len(s.levels) > 1 {
			s.levels = s.levels[:len(s.levels)-1]
		}
	case isWordByte(c):
		end := i
		for end < len(stmt) && isWordByte(stmt[end]) {
			end++
		}
		s.word(stmt[i:end])
		return end
	}
	return i + 1
}

// word applies one word: a clause keyword switches the current level's clause
// (DISTINCT ON is a select list, IS DISTINCT FROM a comparison — neither opens
// a clause); a project column in a scope clause, or projectMark in a scope or
// FROM clause, is recorded.
func (s *clauseScan) word(w string) {
	upper := strings.ToUpper(w)
	top := len(s.levels) - 1
	opens, isKeyword := clauseKeywords[upper]
	switch {
	case (upper == "ON" || upper == "FROM") && s.prev == "DISTINCT":
	case isKeyword:
		s.levels[top] = opens
	case w == projectMark && s.levels[top] != clauseOther:
		s.found[w] = true
	case s.levels[top] == clauseScope && (slices.Contains(projectColumns, w) || isChildKey(w)):
		s.found[w] = true
	}
	s.prev = upper
}

// isChildKey reports whether w is the parent key of a keyed child table, which
// scopes that table's read the way a project column scopes a root table's.
func isChildKey(w string) bool {
	for _, keys := range KeyedChildTables {
		if slices.Contains(keys, w) {
			return true
		}
	}
	return false
}

// isWordByte reports whether c belongs to a SQL identifier or keyword.
func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
