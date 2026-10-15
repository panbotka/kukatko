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
	// usings are the JOIN … USING (cols) of the level.
	usings []usingJoin
}

// usingJoin is one `JOIN ref USING (cols)`: ref equals each item joined
// before it on every one of cols.
type usingJoin struct {
	ref  *sqlRef
	cols []string
}

// sqlStatement is the analysis of one statement: its levels, the equalities
// between two items' project columns found in their conditions, and the
// equalities that key a child table to another item.
type sqlStatement struct {
	levels []*sqlLevel
	edges  [][2]*sqlRef
	// keyEdges are (source, child) pairs: the child's parent key equals a
	// column of the source, so the child is keyed once the source is scoped.
	keyEdges [][2]*sqlRef
	// anyCase: the statement is shaped like SQL (sqlShaped), so an item
	// counts after a keyword in any case.
	anyCase bool
}

// unscopedRefs returns, in statement order, the items of stmt the guard judges
// (judged) that no predicate ties to one project. An item is scoped by a
// condition conjunct — a top-level AND term of a WHERE or JOIN … ON, never one
// under OR (which binds looser than AND), NOT or CASE — of the form
// `<alias>.<project column> = <value>`, where the value is a parameter, a
// literal, a cast, a call over such values, a subquery or a piece the scan
// cannot read — never a column, NULL or an ANY/ALL set — or `= <another scoped
// item's project column>`; and by its project column in an INSERT column list
// or ON CONFLICT target (the INSERT's own target only). A keyed child table is
// scoped by a conjunct `<its parent key> = <value>` (`= ANY(…)` included),
// `<its parent key> IN (<values or a subquery>)`, or `<its parent key> =
// <a column of another item>` once that item is scoped (or is none of the
// guard's tables). A project column in a select list, an ORDER BY, a SET list,
// a comment, a `IS NOT NULL` test or any other comparison scopes nothing, and
// a predicate on one alias scopes no other.
//
// It is a pragmatic parser, not a SQL grammar: it understands the statements
// this repository writes, and treats anything else conservatively.
func unscopedRefs(stmt string) []*sqlRef {
	toks, _ := tokenize(blankComments(stmt), 0, false)
	st := sqlStatement{anyCase: sqlShaped(toks)}
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

// sqlShaped reports whether a statement is SQL rather than prose: it opens
// with a statement verb followed by its partner — SELECT … FROM, WITH x AS,
// INSERT INTO, UPDATE x [alias] SET, DELETE FROM — in any case. An error
// message ("update photo: %w", "select photos") has no partner.
func sqlShaped(toks []sqlToken) bool {
	if len(toks) < 2 || toks[0].kind != tokWord {
		return false
	}
	partner, ok := verbPartners[toks[0].upper]
	return ok && partner(toks)
}

// verbPartners maps each statement verb to the test for the partner that
// makes a statement opening with it SQL (sqlShaped).
var verbPartners = map[string]func(toks []sqlToken) bool{
	"SELECT": func(toks []sqlToken) bool {
		return slices.ContainsFunc(toks[1:], func(t sqlToken) bool { return t.is(kwFrom) })
	},
	"WITH": func(toks []sqlToken) bool {
		return peek(toks, 2).is("AS") || toks[1].is("RECURSIVE") && peek(toks, 3).is("AS")
	},
	"INSERT": func(toks []sqlToken) bool { return toks[1].is("INTO") },
	kwUpdate: func(toks []sqlToken) bool { return peek(toks, 2).is("SET") || peek(toks, 3).is("SET") },
	kwDelete: func(toks []sqlToken) bool { return toks[1].is(kwFrom) },
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

// resolve applies every condition conjunct of every level, then the INSERT
// column lists, then spreads scope along the project-column equalities and
// the parent-key equalities until nothing changes.
func (st *sqlStatement) resolve() {
	for _, lv := range st.levels {
		for _, seg := range lv.conds {
			for _, c := range conjuncts(seg) {
				st.fact(lv, c)
			}
		}
	}
	for _, lv := range st.levels {
		st.joins(lv)
	}
	for st.spread() {
	}
}

// joins applies what lv's FROM list states besides its conditions: an INSERT
// target's column list and every JOIN … USING.
func (st *sqlStatement) joins(lv *sqlLevel) {
	for _, r := range lv.refs {
		if r.insert && namesProject(lv.targetCols, r) {
			r.scoped = true
		}
	}
	for _, u := range lv.usings {
		st.usingEdges(lv, u)
	}
}

// spread passes scope across every project-column equality, and along every
// parent-key equality from its source to its child, once and reports whether
// it scoped anything new.
func (st *sqlStatement) spread() bool {
	changed := false
	for _, e := range st.edges {
		if e[0].scoped != e[1].scoped {
			e[0].scoped, e[1].scoped = true, true
			changed = true
		}
	}
	for _, e := range st.keyEdges {
		if !e[1].scoped && (e[0].scoped || !e[0].guarded()) {
			e[1].scoped = true
			changed = true
		}
	}
	return changed
}

// usingEdges ties the item of a JOIN … USING to every item joined before it:
// a shared project column ties their scope both ways, a shared parent key keys
// the child that carries it.
func (st *sqlStatement) usingEdges(lv *sqlLevel, u usingJoin) {
	for _, prev := range lv.refs {
		if prev == u.ref {
			return
		}
		if !prev.insert {
			for _, col := range u.cols {
				st.usingEdge(prev, u.ref, col)
			}
		}
	}
}

// usingEdge ties joined to prev, an item joined before it, on the shared
// column col.
func (st *sqlStatement) usingEdge(prev, joined *sqlRef, col string) {
	if prev.projectColumn(col) && joined.projectColumn(col) {
		st.edges = append(st.edges, [2]*sqlRef{prev, joined})
	}
	if joined.hasKey(col) {
		st.keyEdges = append(st.keyEdges, [2]*sqlRef{prev, joined})
	}
	if prev.hasKey(col) {
		st.keyEdges = append(st.keyEdges, [2]*sqlRef{joined, prev})
	}
}

// guarded reports whether r is one of the guard's tables: a root, read-scoped
// or keyed child table. Anything else — an account table, a CTE, a derived
// table, a set-returning function — holds no project's rows of its own (a CTE
// or derived table is judged in its own body), so a child keyed to it is keyed.
func (r *sqlRef) guarded() bool {
	if _, ok := RootTables[r.table]; ok {
		return true
	}
	if _, ok := ReadScopedTables[r.table]; ok {
		return true
	}
	_, ok := KeyedChildTables[r.table]
	return ok
}

// namesProject reports whether an INSERT column list names r's project column.
func namesProject(cols []sqlToken, r *sqlRef) bool {
	return slices.ContainsFunc(cols, func(t sqlToken) bool {
		return t.kind == tokWord && r.projectColumn(t.text)
	})
}

// conjuncts splits a condition into its top-level AND terms. OR binds looser
// than AND, so a condition with a top-level OR has no term every row
// satisfies and yields none; a term that is one bracketed condition is split
// in turn. BETWEEN's own AND splits nothing, and nothing inside CASE … END
// splits or is a term.
func conjuncts(toks []sqlToken) [][]sqlToken {
	var out [][]sqlToken
	var sc conjunctScan
	start := 0
	for i, t := range toks {
		switch sc.step(t) {
		case conjunctOr:
			return nil
		case conjunctSplit:
			out = appendConjunct(out, toks[start:i])
			start = i + 1
		case conjunctNone:
		}
	}
	return appendConjunct(out, toks[start:])
}

// conjunctStep is what one token of a condition does to its split.
type conjunctStep int

const (
	// conjunctNone: the token belongs to the current term.
	conjunctNone conjunctStep = iota
	// conjunctSplit: a top-level AND ends the current term.
	conjunctSplit
	// conjunctOr: a top-level OR — the condition has no term every row satisfies.
	conjunctOr
)

// conjunctScan is the state of splitting one condition: inside BETWEEN … AND,
// and how deep inside CASE … END.
type conjunctScan struct {
	between bool
	cases   int
}

// step applies the token t and returns what it does to the split.
func (sc *conjunctScan) step(t sqlToken) conjunctStep {
	switch {
	case t.is("CASE"):
		sc.cases++
	case t.is("END") && sc.cases > 0:
		sc.cases--
	case sc.cases > 0:
	case t.is("OR"):
		return conjunctOr
	case t.is("BETWEEN"):
		sc.between = true
	case t.is("AND") && sc.between:
		sc.between = false
	case t.is("AND"):
		return conjunctSplit
	}
	return conjunctNone
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

// fact applies one conjunct of a condition of lv: an equality `lhs = rhs`, or
// a membership `col IN (…)`. A term under NOT, or holding a CASE or an IS
// test, states nothing the guard relies on.
func (st *sqlStatement) fact(lv *sqlLevel, c []sqlToken) {
	if slices.ContainsFunc(c, func(t sqlToken) bool { return t.is("NOT") || t.is("CASE") || t.is("IS") }) {
		return
	}
	if eq := slices.IndexFunc(c, func(t sqlToken) bool { return t.isOp("=") }); eq >= 0 {
		st.equality(lv, c[:eq], c[eq+1:])
		return
	}
	if in := slices.IndexFunc(c, func(t sqlToken) bool { return t.is("IN") }); in >= 0 {
		st.membership(lv, c[:in], c[in+1:])
	}
}

// equality applies `lhs = rhs`: a project column equal to a value scopes the
// column's item, two project columns equal to each other tie their items; a
// parent key equal to a value (or an ANY set) keys its child table, equal to
// another item's column it keys the child once that item is scoped.
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
		st.keyEdge(lv, lq, lc, rq)
		st.keyEdge(lv, rq, rc, lq)
	case lok:
		st.valueEquality(lv, lq, lc, rhs)
	case rok:
		st.valueEquality(lv, rq, rc, lhs)
	}
}

// valueEquality applies `qual.col = value`: a project column equal to a value
// scopes its item, a parent key equal to a value (or an ANY set) keys its
// child table.
func (st *sqlStatement) valueEquality(lv *sqlLevel, qual, col string, value []sqlToken) {
	if isValue(value, false) {
		scopeAll(st.bind(lv, qual, col))
	}
	if isValue(value, true) {
		scopeAll(keyed(lv, qual, col))
	}
}

// keyEdge records that the child tables whose parent key `qual.col` names are
// keyed by the item named other (qualified only: a bare column on the other
// side names no item the guard can tell). A qualifier the statement does not
// define is an outer item of a correlated fragment built apart from its
// statement (`EXISTS (SELECT 1 FROM user_ratings ur WHERE ur.photo_uid =
// photos.uid …)` appended to a photo listing): that item is judged where the
// statement is assembled, so it keys the child here.
func (st *sqlStatement) keyEdge(lv *sqlLevel, qual, col, other string) {
	if other == "" {
		return
	}
	src := lookup(lv, other)
	if src == nil {
		src = &sqlRef{} // outside the text: none of the guard's tables here
	}
	for _, child := range keyed(lv, qual, col) {
		if child != src {
			st.keyEdges = append(st.keyEdges, [2]*sqlRef{src, child})
		}
	}
}

// membership applies `col IN (…)`: a parent key in a list of values or in a
// subquery keys its child table. A project column in a list of projects scopes
// nothing.
func (st *sqlStatement) membership(lv *sqlLevel, lhs, rhs []sqlToken) {
	q, c, ok := columnRef(lhs)
	if !ok || len(rhs) != 1 || rhs[0].kind != tokGroup {
		return
	}
	if isSubquery(rhs[0].inner) || isValueGroup(rhs[0].inner) {
		scopeAll(keyed(lv, q, c))
	}
}

// scopeAll marks every item of refs scoped.
func scopeAll(refs []*sqlRef) {
	for _, r := range refs {
		r.scoped = true
	}
}

// keyed returns the keyed child tables the parent key `qual.col` names: the
// one its qualifier resolves to, or — bare — every one of lv carrying that key.
func keyed(lv *sqlLevel, qual, col string) []*sqlRef {
	if qual != "" {
		if r := lookup(lv, qual); r != nil && r.hasKey(col) {
			return []*sqlRef{r}
		}
		return nil
	}
	var out []*sqlRef
	for _, r := range lv.refs {
		if !r.insert && r.hasKey(col) {
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

// isValue reports whether side is one value a project column (or, with set,
// a parent key) may be compared with: built only of parameters, literals,
// casts, calls whose arguments are such values, subqueries and pieces the
// scan cannot read — no column anywhere (`COALESCE(NULLIF($1, ”),
// project_id)` is a tautology, not a scope), never NULL, and an ANY/SOME set
// only with set.
func isValue(side []sqlToken, set bool) bool {
	if len(side) == 0 {
		return false
	}
	for i := range side {
		if !isValueToken(side, i, set) {
			return false
		}
	}
	return true
}

// isValueToken reports whether side[i] may stand in a value (isValue): a
// word that is part of one, an ANY/SOME set (with set) over a bracket, a
// subquery or a bracket of values, or anything else that is no word.
func isValueToken(side []sqlToken, i int, set bool) bool {
	t := side[i]
	switch {
	case t.is("ANY") || t.is("SOME"):
		return set && peek(side, i+1).kind == tokGroup
	case t.kind == tokWord:
		return isValueWord(side, i)
	case t.kind == tokGroup:
		return isSubquery(t.inner) || isValueGroup(t.inner)
	}
	return true
}

// isValueGroup reports whether the inside of a bracket holds only values: a
// list of them, a bracketed one, or a CAST's `value AS type`.
func isValueGroup(inner []sqlToken) bool {
	for i, t := range inner {
		switch {
		case t.is("AS") || i > 0 && inner[i-1].is("AS"):
		case t.kind == tokWord && !isValueWord(inner, i):
			return false
		case t.kind == tokGroup && !isSubquery(t.inner) && !isValueGroup(t.inner):
			return false
		}
	}
	return true
}

// isValueWord reports whether the word side[i] is part of a value rather than
// a column: a number, a type after `::` or a function name — never NULL.
func isValueWord(side []sqlToken, i int) bool {
	t := side[i]
	if t.is("NULL") {
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
