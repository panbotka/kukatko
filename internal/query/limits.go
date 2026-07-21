package query

// MaxLength caps the raw search string a request may submit. It is a cheap
// guard meant to be applied before parsing, bounding the work an over-long
// input forces before the tighter complexity cap even runs. It is far above
// any legitimate search — a real query is a handful of words and filters, not
// kilobytes — so honest callers never approach it.
const MaxLength = 8192

// MaxComplexity caps the number of SQL conditions a parsed query may compile
// to downstream. Every free-text term and every filter OR-alternative ('|')
// becomes its own condition — an ILIKE term or comparison with a bound
// parameter — in the photos store, evaluated per row and, for a listing, twice
// per request (the page and its count). Without a cap a single token such as
// "title:a|a|…|a" packing tens of thousands of alternatives forces an
// arbitrarily expensive scan and, past PostgreSQL's parameter limit, a 500
// instead. A few hundred is orders of magnitude above any legitimate query, so
// the cap bites only abuse and leaves normal searches untouched.
const MaxComplexity = 256

// Complexity returns the number of SQL conditions the parsed query compiles to
// downstream: one per free-text term plus one per filter OR-alternative. It is
// the cost measure a trust boundary caps (against MaxComplexity) to keep a
// single request's query bounded; it counts the AST cheaply, without touching
// the database.
func (q Query) Complexity() int {
	n := len(q.Terms)
	for _, f := range q.Filters {
		n += len(f.Values)
	}
	return n
}
