package tenancyguard

import (
	"slices"
	"sort"
)

// sqlRef is one item a statement reads or writes: a table named after FROM,
// JOIN, USING, UPDATE or INSERT INTO (or a derived table or set-returning
// function, whose table is empty), with the alias it goes by.
type sqlRef struct {
	table, alias string
	start, end   int
	// counted: named after an upper-case keyword. Lower-case "update photos"
	// is the prose of an error message, not a statement.
	counted bool
	// write: the target of an INSERT, UPDATE or DELETE; insert: of an INSERT.
	write, insert bool
	// scoped: a predicate (or an INSERT column list) ties the item to one
	// project, directly or through an equality with another scoped item.
	scoped bool
}

// judged reports whether the guard requires r to be scoped: every reference
// to a root table, and every read of a read-scoped or keyed child table.
func (r *sqlRef) judged() bool {
	if !r.counted {
		return false
	}
	if _, ok := RootTables[r.table]; ok {
		return true
	}
	_, read := ReadScopedTables[r.table]
	_, child := KeyedChildTables[r.table]
	return (read || child) && !r.write
}

// projectColumn reports whether col names r's project: a root or read-scoped
// table's own column, `projects.uid`, or — for a table the guard does not
// know (project_members, a CTE, a derived table) — any project column name.
// A keyed child table has none.
func (r *sqlRef) projectColumn(col string) bool {
	if c, ok := RootTables[r.table]; ok {
		return col == c
	}
	if c, ok := ReadScopedTables[r.table]; ok {
		return col == c
	}
	if _, ok := KeyedChildTables[r.table]; ok {
		return false
	}
	if r.table == "projects" {
		return col == "uid"
	}
	return slices.Contains(projectColumns, col)
}

// hasKey reports whether key is a parent key of r, a keyed child table.
func (r *sqlRef) hasKey(key string) bool {
	return slices.Contains(KeyedChildTables[r.table], key)
}

// sqlLevel is one query level: a SELECT (one arm of a UNION), a subquery, a
// CTE body, or the INSERT/UPDATE/DELETE itself. A predicate binds to the items
// of its own level, and — qualified by an alias — to an enclosing level's.
type sqlLevel struct {
	parent *sqlLevel
	refs   []*sqlRef
	// conds are the condition segments: the WHERE and every JOIN's ON/USING.
	conds [][]sqlToken
	// targetCols are an INSERT's column list and ON CONFLICT target.
	targetCols []sqlToken
	// marked: projectMark stands in the FROM list or as a top-level conjunct
	// of a condition, so an opaque project predicate covers the level.
	marked bool
}

// sqlStatement is the analysis of one statement: its levels, and the
// equalities between two items' project columns found in their conditions.
type sqlStatement struct {
	levels []*sqlLevel
	edges  [][2]*sqlRef
}

// unscopedRefs returns, in statement order, the items of stmt the guard judges
// (judged) that no predicate ties to one project. An item is scoped by a
// condition conjunct — a top-level AND term of a WHERE or JOIN … ON, never one
// under OR or NOT — of the form `<alias>.<project column> = <value>` (a
// parameter, a literal, a function or subquery, an opaque project-named piece)
// or `= <another scoped item's project column>`; by projectMark in its level's
// FROM list or as a conjunct; by its project column in an INSERT column list
// or ON CONFLICT target (the INSERT's own target only); and a keyed child
// table by a conjunct naming one of its parent keys. A project column in a
// select list, an ORDER BY, a SET list, a comment or a `IS NOT NULL` test
// scopes nothing, and a predicate on one alias scopes no other.
//
// It is a pragmatic parser, not a SQL grammar: it understands the statements
// this repository writes, and treats anything else conservatively.
func unscopedRefs(stmt string) []*sqlRef {
	toks, _ := tokenize(blankComments(stmt), 0, false)
	var st sqlStatement
	st.query(toks, nil)
	st.resolve()
	var out []*sqlRef
	for _, lv := range st.levels {
		for _, r := range lv.refs {
			if r.judged() && !r.scoped {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

// query analyses one query (a statement, subquery or CTE body) whose
// correlated names resolve in parent: each UNION/INTERSECT/EXCEPT arm is a
// level of its own.
func (st *sqlStatement) query(toks []sqlToken, parent *sqlLevel) {
	start := 0
	for i, t := range toks {
		if t.is("UNION") || t.is("INTERSECT") || t.is("EXCEPT") {
			st.arm(toks[start:i], parent)
			start = i + 1
		}
	}
	st.arm(toks[start:], parent)
}

// group analyses a parenthesised group seen inside lv: a subquery is a query
// of its own, anything else (a function's arguments, a bracketed expression)
// is searched for nested subqueries.
func (st *sqlStatement) group(t sqlToken, lv *sqlLevel) {
	if isSubquery(t.inner) {
		st.query(t.inner, lv)
		return
	}
	for _, in := range t.inner {
		if in.kind == tokGroup {
			st.group(in, lv)
		}
	}
}

// isSubquery reports whether a group's tokens are a query of their own.
func isSubquery(toks []sqlToken) bool {
	if len(toks) == 0 || toks[0].kind != tokWord {
		return false
	}
	switch toks[0].upper {
	case "SELECT", "WITH", "VALUES", "INSERT", "UPDATE", "DELETE":
		return true
	}
	return false
}

// resolve applies every condition conjunct of every level, then the marks and
// INSERT column lists, then spreads scope along the project-column equalities
// until nothing changes.
func (st *sqlStatement) resolve() {
	for _, lv := range st.levels {
		for _, seg := range lv.conds {
			for _, c := range conjuncts(seg) {
				st.fact(lv, c)
			}
		}
	}
	for _, lv := range st.levels {
		for _, r := range lv.refs {
			if lv.marked || r.insert && namesProject(lv.targetCols, r) {
				r.scoped = true
			}
		}
	}
	for st.spread() {
	}
}

// spread passes scope across every project-column equality once and reports
// whether it scoped anything new.
func (st *sqlStatement) spread() bool {
	changed := false
	for _, e := range st.edges {
		if e[0].scoped != e[1].scoped {
			e[0].scoped, e[1].scoped = true, true
			changed = true
		}
	}
	return changed
}

// namesProject reports whether an INSERT column list names r's project column
// (or carries projectMark).
func namesProject(cols []sqlToken, r *sqlRef) bool {
	return slices.ContainsFunc(cols, func(t sqlToken) bool {
		return t.kind == tokMark || t.kind == tokWord && r.projectColumn(t.text)
	})
}

// conjuncts splits a condition into its top-level AND terms. A term that is
// one bracketed condition is split in turn; BETWEEN's own AND splits nothing.
func conjuncts(toks []sqlToken) [][]sqlToken {
	var out [][]sqlToken
	start, between := 0, false
	for i, t := range toks {
		switch {
		case t.is("BETWEEN"):
			between = true
		case t.is("AND") && between:
			between = false
		case t.is("AND"):
			out = appendConjunct(out, toks[start:i])
			start = i + 1
		}
	}
	return appendConjunct(out, toks[start:])
}

// appendConjunct appends one term, flattening a bracketed condition.
func appendConjunct(out [][]sqlToken, c []sqlToken) [][]sqlToken {
	switch {
	case len(c) == 0:
		return out
	case len(c) == 1 && c[0].kind == tokGroup && !isSubquery(c[0].inner):
		return append(out, conjuncts(c[0].inner)...)
	}
	return append(out, c)
}

// fact applies one conjunct of a condition of lv. A term under OR or NOT
// scopes nothing; projectMark in it marks the level; a parent key scopes a
// keyed child table; an equality is applied by equality.
func (st *sqlStatement) fact(lv *sqlLevel, c []sqlToken) {
	if c[0].is("NOT") || slices.ContainsFunc(c, func(t sqlToken) bool { return t.is("OR") }) {
		return
	}
	if slices.ContainsFunc(c, func(t sqlToken) bool { return t.kind == tokMark }) {
		lv.marked = true
	}
	st.keys(lv, c)
	if eq := slices.IndexFunc(c, func(t sqlToken) bool { return t.isOp("=") }); eq >= 0 {
		st.equality(lv, c[:eq], c[eq+1:])
	}
}

// equality applies `lhs = rhs`: a project column equal to a value scopes the
// column's item, two project columns equal to each other tie their items.
func (st *sqlStatement) equality(lv *sqlLevel, lhs, rhs []sqlToken) {
	lq, lc, lok := columnRef(lhs)
	rq, rc, rok := columnRef(rhs)
	switch {
	case lok && rok:
		for _, x := range st.bind(lv, lq, lc) {
			for _, y := range st.bind(lv, rq, rc) {
				st.edges = append(st.edges, [2]*sqlRef{x, y})
			}
		}
	case lok && isValue(rhs):
		scopeAll(st.bind(lv, lq, lc))
	case rok && isValue(lhs):
		scopeAll(st.bind(lv, rq, rc))
	}
}

// scopeAll marks every item of refs scoped.
func scopeAll(refs []*sqlRef) {
	for _, r := range refs {
		r.scoped = true
	}
}

// keys scopes the keyed child tables whose parent key the conjunct names —
// qualified by the table's alias, or bare for the tables of lv. A NULL test
// (`m.subject_uid IS NULL`) keys nothing: it selects every such row.
func (st *sqlStatement) keys(lv *sqlLevel, c []sqlToken) {
	if slices.ContainsFunc(c, func(t sqlToken) bool { return t.is("IS") }) {
		return
	}
	for i, t := range c {
		if t.kind == tokWord && isChildKey(t.text) {
			scopeAll(keyed(lv, c, i))
		}
	}
}

// keyed returns the keyed child tables the parent key at c[i] names: the one
// its qualifier resolves to, or — bare — every one of lv carrying that key.
func keyed(lv *sqlLevel, c []sqlToken, i int) []*sqlRef {
	key := c[i].text
	if i >= 2 && c[i-1].isOp(".") && c[i-2].kind == tokWord {
		if r := lookup(lv, c[i-2].text); r != nil && r.hasKey(key) {
			return []*sqlRef{r}
		}
		return nil
	}
	var out []*sqlRef
	for _, r := range lv.refs {
		if r.hasKey(key) {
			out = append(out, r)
		}
	}
	return out
}

// columnRef reports whether side is one column reference, `col` or
// `alias.col`, and returns its qualifier and column.
func columnRef(side []sqlToken) (qual, col string, ok bool) {
	switch {
	case len(side) == 1 && isColumnWord(side[0]):
		return "", side[0].text, true
	case len(side) == 3 && side[0].kind == tokWord && side[1].isOp(".") && isColumnWord(side[2]):
		return side[0].text, side[2].text, true
	}
	return "", "", false
}

// isColumnWord reports whether t can name a column: a word that is not a
// number.
func isColumnWord(t sqlToken) bool {
	return t.kind == tokWord && !isDigit(t.text[0])
}

// isValue reports whether side is one value a project column may be compared
// with: no column of its own at the top level (a parameter, a literal, a
// cast, a function call, a subquery, an opaque piece), and neither NULL nor
// an ANY/ALL/SOME set of projects.
func isValue(side []sqlToken) bool {
	if len(side) == 0 {
		return false
	}
	for i, t := range side {
		if t.kind == tokWord && !isValueWord(side, i) {
			return false
		}
	}
	return true
}

// isValueWord reports whether the word side[i] is part of a value rather than
// a column: a number, a type after `::` or a function name — never NULL or a
// set quantifier.
func isValueWord(side []sqlToken, i int) bool {
	t := side[i]
	switch t.upper {
	case "NULL", "ANY", "ALL", "SOME":
		return false
	}
	return isDigit(t.text[0]) || i > 0 && side[i-1].isOp("::") || peek(side, i+1).kind == tokGroup
}

// lookup returns the item named qual — by alias, or by table when it has
// none — in lv or the nearest enclosing level, or nil.
func lookup(lv *sqlLevel, qual string) *sqlRef {
	for l := lv; l != nil; l = l.parent {
		for _, r := range l.refs {
			if !r.insert && (r.alias == qual || r.alias == "" && r.table == qual) {
				return r
			}
		}
	}
	return nil
}

// bind returns the items a project column reference names: the one its
// qualifier resolves to, or — unqualified — the items of lv that carry that
// column (the guard's tables before any other). The column must name the
// item's project.
func (st *sqlStatement) bind(lv *sqlLevel, qual, col string) []*sqlRef {
	if qual != "" {
		if r := lookup(lv, qual); r != nil && r.projectColumn(col) {
			return []*sqlRef{r}
		}
		return nil
	}
	var known, other []*sqlRef
	for _, r := range lv.refs {
		if r.insert || !r.projectColumn(col) {
			continue
		}
		if _, ok := RootTables[r.table]; ok {
			known = append(known, r)
		} else if _, ok := ReadScopedTables[r.table]; ok {
			known = append(known, r)
		} else {
			other = append(other, r)
		}
	}
	if len(known) > 0 {
		return known
	}
	return other
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
