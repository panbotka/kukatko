package tenancyguard

// clause is the part of a query level a token sits in, as far as scoping goes.
type clause int

const (
	// clauseOther is a select list, VALUES, SET, ordering, grouping,
	// RETURNING and the like: nothing there constrains the rows.
	clauseOther clause = iota
	// clauseFrom is a FROM or JOIN list (a DELETE's USING list included).
	clauseFrom
	// clauseCond is a WHERE or a JOIN's ON/USING condition.
	clauseCond
	// clauseConflict is an INSERT's ON CONFLICT target.
	clauseConflict
)

// The keywords an item follows, spelled once.
const (
	kwFrom   = "FROM"
	kwUpdate = "UPDATE"
	kwUsing  = "USING"
	kwDelete = "DELETE"
)

// itemKind is how the next FROM/JOIN/UPDATE/INTO item is named.
type itemKind struct {
	counted, write, insert bool
}

// reservedWords end a FROM item: none of them is a table name or an alias.
var reservedWords = map[string]bool{
	"WHERE": true, "JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "FULL": true,
	"CROSS": true, "NATURAL": true, "ON": true, "USING": true, "GROUP": true, "ORDER": true, "LIMIT": true,
	"OFFSET": true, "RETURNING": true, "SET": true, "VALUES": true, "UNION": true, "INTERSECT": true,
	"EXCEPT": true, "WINDOW": true, "FOR": true, "HAVING": true, "SELECT": true, "AS": true, "LATERAL": true,
	"DEFAULT": true, "DO": true, "FETCH": true, "TABLESAMPLE": true, "WITH": true, "OVERRIDING": true,
	"AND": true, "OR": true, "NOT": true, "FROM": true,
}

// otherClauseWords open a clause in which nothing constrains the rows.
var otherClauseWords = map[string]bool{
	"WITH": true, "SELECT": true, "VALUES": true, "RETURNING": true, "GROUP": true, "ORDER": true,
	"HAVING": true, "LIMIT": true, "OFFSET": true, "WINDOW": true, "FETCH": true, "DO": true, "SET": true,
	"INSERT": true, kwDelete: true, "FOR": true,
}

// joinWords qualify a JOIN; they are skipped unless they call a function
// (`left(name, 3)`).
var joinWords = map[string]bool{
	"LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true, "FULL": true, "CROSS": true, "NATURAL": true,
}

// armWalk is the state of the walk over one query level's tokens.
type armWalk struct {
	st     *sqlStatement
	lv     *sqlLevel
	clause clause
	// expect: the next token starts an item, named as next says.
	expect bool
	next   itemKind
	prev   string
}

// arm analyses one query level (a statement, or one arm of a set operation)
// whose correlated names resolve in parent.
func (st *sqlStatement) arm(toks []sqlToken, parent *sqlLevel) {
	lv := &sqlLevel{parent: parent}
	st.levels = append(st.levels, lv)
	w := armWalk{st: st, lv: lv}
	for i := 0; i < len(toks); {
		i = w.step(toks, i)
	}
}

// step consumes the token at i (an item from there, when one is expected)
// and returns the offset after what it consumed.
func (w *armWalk) step(toks []sqlToken, i int) int {
	if w.expect {
		if next := w.item(toks, i); next > i {
			return next
		}
	}
	t := toks[i]
	if t.kind == tokWord && w.keyword(t, peek(toks, i+1)) {
		w.prev = t.upper
		return i + 1
	}
	w.plain(t)
	w.prev = t.upper
	return i + 1
}

// peek returns toks[i], or the zero token past the end.
func peek(toks []sqlToken, i int) sqlToken {
	if i < len(toks) {
		return toks[i]
	}
	return sqlToken{kind: tokOp}
}

// plain applies a token that is no keyword: a group is searched for
// subqueries (and is an ON CONFLICT target), a comma in a FROM list expects
// the next item, and a condition collects it.
func (w *armWalk) plain(t sqlToken) {
	switch {
	case t.kind == tokGroup:
		if w.clause == clauseConflict {
			w.lv.targetCols = append(w.lv.targetCols, t.inner...)
		}
		w.st.group(t, w.lv)
	case t.isOp(",") && w.clause == clauseFrom:
		w.expect = true
	}
	if w.clause == clauseCond {
		last := len(w.lv.conds) - 1
		w.lv.conds[last] = append(w.lv.conds[last], t)
	}
}

// counts reports whether an item named after the keyword t is a reference:
// t is written in upper case, or the whole statement is shaped like SQL. A
// lower-case "update photos" in the prose of an error message is neither.
func (w *armWalk) counts(t sqlToken) bool {
	return t.text == t.upper || w.st.anyCase
}

// using records `JOIN … USING (cols)`: the item just joined equals every item
// before it in lv on each of cols.
func (w *armWalk) using(cols sqlToken) {
	w.clause = clauseOther
	if len(w.lv.refs) == 0 {
		return
	}
	u := usingJoin{ref: w.lv.refs[len(w.lv.refs)-1]}
	for _, t := range cols.inner {
		if t.kind == tokWord {
			u.cols = append(u.cols, t.text)
		}
	}
	w.lv.usings = append(w.lv.usings, u)
}

// open switches to clause c and expects an item named as kind says.
func (w *armWalk) open(c clause, kind itemKind) {
	w.clause, w.expect, w.next = c, true, kind
}

// condition opens a new condition segment.
func (w *armWalk) condition() {
	w.clause = clauseCond
	w.lv.conds = append(w.lv.conds, nil)
}

// keyword applies the keyword t (followed by next) and reports whether it was
// one: an item keyword, or one that opens a clause.
func (w *armWalk) keyword(t, next sqlToken) bool {
	if w.itemKeyword(t, next) {
		return true
	}
	return w.clauseKeyword(t, next)
}

// itemKeyword applies a keyword an item follows — FROM, JOIN, a DELETE's
// USING, INSERT's INTO, UPDATE — and reports whether t was one. The item
// counts as a reference after an upper-case keyword, or after one in any case
// in a statement shaped like SQL (sqlShaped).
func (w *armWalk) itemKeyword(t, next sqlToken) bool {
	switch t.upper {
	case kwUpdate:
		w.update(t)
	case kwFrom:
		if w.prev == "DISTINCT" { // IS DISTINCT FROM: a comparison
			return false
		}
		w.open(clauseFrom, itemKind{counted: w.counts(t), write: w.prev == kwDelete})
	case "JOIN":
		w.open(clauseFrom, itemKind{counted: w.counts(t)})
	case kwUsing:
		if next.kind == tokGroup { // JOIN … USING (col): an equality of the joined items
			w.using(next)
			return true
		}
		w.open(clauseFrom, itemKind{counted: w.counts(t)})
	case "INTO":
		w.open(clauseOther, itemKind{counted: w.counts(t), write: true, insert: true})
	default:
		return false
	}
	return true
}

// clauseKeyword applies a keyword that opens a clause and reports whether t
// was one. A JOIN qualifier is skipped unless it calls a function.
func (w *armWalk) clauseKeyword(t, next sqlToken) bool {
	switch {
	case otherClauseWords[t.upper]:
		w.clause = clauseOther
	case t.upper == "WHERE":
		w.condition()
	case t.upper == "ON":
		w.on(next.is("CONFLICT"))
	case t.upper == "CONFLICT":
		w.clause = clauseConflict
	case joinWords[t.upper]:
		return next.kind != tokGroup
	default:
		return false
	}
	return true
}

// update applies UPDATE: a statement's target, unless it is the row lock of
// FOR [NO KEY] UPDATE or the ON CONFLICT DO UPDATE action.
func (w *armWalk) update(t sqlToken) {
	switch w.prev {
	case "FOR", "KEY", "DO":
		w.clause = clauseOther
	default:
		w.open(clauseOther, itemKind{counted: w.counts(t), write: true})
	}
}

// on applies ON: a JOIN's condition — unless it is DISTINCT ON (a select
// list), ON CONFLICT or ON CONFLICT ON CONSTRAINT.
func (w *armWalk) on(conflictNext bool) {
	if w.prev == "DISTINCT" || w.prev == "CONFLICT" || conflictNext {
		return
	}
	w.condition()
}

// item reads the FROM/JOIN/UPDATE/INTO item at i and returns the offset after
// it, or i when no item starts there (a keyword ends the list).
func (w *armWalk) item(toks []sqlToken, i int) int {
	t := toks[i]
	switch {
	case t.is("LATERAL") || t.is("ONLY"):
		return i + 1
	case t.kind == tokGroup:
		w.expect = false
		w.st.group(t, w.lv)
		ref := &sqlRef{start: t.start, end: t.end}
		i = w.alias(toks, i+1, ref)
		w.lv.refs = append(w.lv.refs, ref)
		return i
	case t.kind != tokWord || reservedWords[t.upper]:
		w.expect = false
		return i
	}
	w.expect = false
	return w.table(toks, i)
}

// table reads the named item at i — a table (schema-qualified or not), or a
// set-returning function — with its alias and, for an INSERT target, its
// column list, and returns the offset after it.
func (w *armWalk) table(toks []sqlToken, i int) int {
	t := toks[i]
	ref := &sqlRef{table: t.text, start: t.start, end: t.end,
		counted: w.next.counted, write: w.next.write, insert: w.next.insert}
	i++
	if peek(toks, i).isOp(".") && peek(toks, i+1).kind == tokWord { // schema.table
		name := toks[i+1]
		ref.table, ref.start, ref.end = name.text, name.start, name.end
		i += 2
	}
	if !ref.insert && peek(toks, i).kind == tokGroup { // a function: unnest($1)
		w.st.group(toks[i], w.lv)
		ref.table = ""
		i++
	}
	i = w.alias(toks, i, ref)
	if ref.insert && peek(toks, i).kind == tokGroup {
		w.lv.targetCols = append(w.lv.targetCols, toks[i].inner...)
		i++
	}
	w.lv.refs = append(w.lv.refs, ref)
	return i
}

// alias reads an item's optional `[AS] alias` (and a derived table's column
// alias list) at i into ref and returns the offset after it.
func (w *armWalk) alias(toks []sqlToken, i int, ref *sqlRef) int {
	if i < len(toks) && toks[i].is("AS") {
		i++
	}
	if i < len(toks) && toks[i].kind == tokWord && !reservedWords[toks[i].upper] {
		ref.alias = toks[i].text
		i++
		if !ref.insert && i < len(toks) && toks[i].kind == tokGroup {
			i++
		}
	}
	return i
}
