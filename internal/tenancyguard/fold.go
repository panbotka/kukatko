package tenancyguard

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
)

// unknownMark stands in the folded text for a piece the scan cannot read.
const unknownMark = " ? "

// piece is one part of a folded string expression: text taken from a string
// literal (lit set, so a reference in it can be located) or a stand-in.
type piece struct {
	text string
	// lit is the literal the text comes from; nil for a stand-in.
	lit *ast.BasicLit
	// raw is the literal's whole unquoted value and off the text's offset in it.
	raw string
	off int
	// use is where the package-level constant or variable holding the literal
	// is named by the expression being folded (the outermost such name), or
	// token.NoPos for a literal written in the expression itself. A finding
	// is anchored there: a shared constant is judged, and allow-listed, at
	// each statement that uses it, never once for all of them.
	use token.Pos
	// cond: the piece was appended to a statement under a branch or loop the
	// statement's start is not under (`if x { q += " AND …" }`), so the
	// statement may run without it.
	cond bool
}

// usedAt anchors pieces folded from a package-level name at its use.
func usedAt(pieces []piece, use token.Pos) []piece {
	for i := range pieces {
		pieces[i].use = use
	}
	return pieces
}

// isStringExpr reports whether e is an expression the scan folds: a string
// literal, a concatenation, an Sprintf/Join call, or a name of such a value.
func (p *pkgScan) isStringExpr(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING
	case *ast.BinaryExpr:
		return e.Op == token.ADD
	case *ast.ParenExpr:
		return p.isStringExpr(e.X)
	case *ast.CallExpr:
		return isCall(e, "fmt", "Sprintf") || isCall(e, "strings", "Join") || p.helperOf(e) != nil
	}
	return false
}

// fold turns a string expression into its pieces. seen lists the names being
// folded, so a value defined in terms of itself stops there.
func (p *pkgScan) fold(e ast.Expr, seen []string) []piece {
	switch e := e.(type) {
	case *ast.BasicLit:
		return foldLit(e)
	case *ast.ParenExpr:
		return p.fold(e.X, seen)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return append(p.fold(e.X, seen), p.fold(e.Y, seen)...)
		}
	case *ast.Ident:
		return p.foldIdent(e, seen)
	case *ast.CallExpr:
		return p.foldCall(e, seen)
	}
	return []piece{p.unknown(e)}
}

// foldLit folds a literal: a string becomes a located piece, anything else its
// source text.
func foldLit(lit *ast.BasicLit) []piece {
	if lit.Kind != token.STRING {
		return []piece{{text: lit.Value}}
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return []piece{{text: unknownMark}}
	}
	return []piece{{text: s, lit: lit, raw: s}}
}

// foldCall folds the calls that build strings the scan understands —
// fmt.Sprintf, strings.Join and a SQL helper of the package (helperOf); any
// other call is unknown.
func (p *pkgScan) foldCall(call *ast.CallExpr, seen []string) []piece {
	if isCall(call, "fmt", "Sprintf") && len(call.Args) > 0 {
		return p.foldSprintf(call, seen)
	}
	if isCall(call, "strings", "Join") && len(call.Args) == 2 {
		return p.foldJoin(call, seen)
	}
	if fn := p.helperOf(call); fn != nil && !slices.Contains(seen, "func "+fn.Name.Name) {
		return p.foldHelper(fn, call, seen)
	}
	return []piece{p.unknown(call)}
}

// helperOf returns the package's SQL helper call calls — a function (not a
// method) returning a string whose body is one `return <string expression>`,
// such as `func projectClause(ph string) string { return "photos.project_id
// = " + ph }` — or nil. Its argument count must match its parameters.
func (p *pkgScan) helperOf(call *ast.CallExpr) *ast.FuncDecl {
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return nil
	}
	fn := p.helpers[id.Name]
	if fn == nil || len(call.Args) != len(paramNames(fn)) || call.Ellipsis.IsValid() {
		return nil
	}
	return fn
}

// paramNames returns a function's parameter names in order ("_" for an
// unnamed one).
func paramNames(fn *ast.FuncDecl) []string {
	var names []string
	for _, f := range fn.Type.Params.List {
		if len(f.Names) == 0 {
			names = append(names, "_")
		}
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	return names
}

// isHelper reports whether fn is a SQL helper: a function returning one
// string whose body is a single return of a string expression.
func isHelper(fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Body == nil || len(fn.Body.List) != 1 || fn.Type.Results == nil ||
		len(fn.Type.Results.List) != 1 || len(fn.Type.Results.List[0].Names) > 1 {
		return false
	}
	if id, ok := fn.Type.Results.List[0].Type.(*ast.Ident); !ok || id.Name != "string" {
		return false
	}
	return helperReturn(fn) != nil
}

// helperReturn returns the expression a function whose body is a single
// one-value return returns, or nil.
func helperReturn(fn *ast.FuncDecl) ast.Expr {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return nil
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return nil
	}
	return ret.Results[0]
}

// foldHelper folds a call of a SQL helper in place: its returned expression,
// with each parameter standing for the folded argument the call passes. The
// helper's own literals are anchored at the call, like a constant's at its
// use; the arguments keep their own anchors.
func (p *pkgScan) foldHelper(fn *ast.FuncDecl, call *ast.CallExpr, seen []string) []piece {
	args := make(map[string][]piece, len(call.Args))
	for i, name := range paramNames(fn) {
		args[name] = p.fold(call.Args[i], seen)
	}
	p.used["func "+fn.Name.Name] = true
	outer, outerArgs := p.scope, p.args
	p.scope, p.args = nil, args
	p.resolving++
	pieces := p.fold(helperReturn(fn), append(seen, "func "+fn.Name.Name))
	p.resolving--
	p.scope, p.args = outer, outerArgs
	for i := range pieces {
		if pc := &pieces[i]; pc.lit != nil && !pc.use.IsValid() && pc.lit.Pos() >= fn.Pos() && pc.lit.End() <= fn.End() {
			pc.use = call.Pos()
		}
	}
	return pieces
}

// foldIdent folds a name: a local variable becomes every value assigned to it,
// a package-level string constant or variable its value. A name already being
// folded (`q = q + …`) contributes nothing; anything else is unknown.
func (p *pkgScan) foldIdent(id *ast.Ident, seen []string) []piece {
	if arg, ok := p.args[id.Name]; ok {
		return slices.Clone(arg)
	}
	if slices.Contains(seen, id.Name) {
		return nil
	}
	p.resolving++
	defer func() { p.resolving-- }()
	if p.isLocal(id.Name) {
		p.used[p.localKey(id.Name)] = true
		var out []piece
		for _, v := range p.scope.values[id.Name] {
			pieces := p.fold(v.expr, append(seen, id.Name))
			if v.cond {
				pieces = conditional(pieces)
			}
			out = append(out, pieces...)
		}
		return out
	}
	if v, ok := p.decls[id.Name]; ok && p.isStringExpr(v) {
		p.used[id.Name] = true
		return usedAt(p.fold(v, append(seen, id.Name)), id.Pos())
	}
	return []piece{p.unknown(id)}
}

// unknown stands in for an expression the scan cannot read. Literals nested in
// it are still scanned as appearances of their own — except while a name is
// being resolved, as the walk reaches that value where it is defined. What the
// expression is called says nothing: `projectFilter` folds to the same
// stand-in as any other unreadable piece.
func (p *pkgScan) unknown(e ast.Expr) piece {
	if p.resolving == 0 {
		ast.Inspect(e, p.visit)
	}
	return piece{text: unknownMark}
}

// conditional marks pieces as appended under a branch the statement's start
// is not under.
func conditional(pieces []piece) []piece {
	for i := range pieces {
		pieces[i].cond = true
	}
	return pieces
}

// verb matches one fmt verb, with its optional explicit argument index.
var verb = regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0]*\d*(?:\.\d*)?([a-zA-Z%])`)

// foldSprintf folds fmt.Sprintf(format, args...): the format's literal text with
// each verb replaced by the folded argument it consumes.
func (p *pkgScan) foldSprintf(call *ast.CallExpr, seen []string) []piece {
	f := sprintfFold{args: make([][]piece, len(call.Args)-1)}
	for i, a := range call.Args[1:] {
		f.args[i] = p.fold(a, seen)
	}
	for _, pc := range p.fold(call.Args[0], seen) {
		if pc.lit == nil {
			f.out = append(f.out, pc)
			continue
		}
		f.literal(pc)
	}
	return f.out
}

// sprintfFold is the state of one Sprintf fold: the folded arguments, the
// index of the next one a verb consumes, and the pieces so far.
type sprintfFold struct {
	args [][]piece
	next int
	out  []piece
}

// literal copies one literal piece of the format, replacing each verb in it.
func (f *sprintfFold) literal(pc piece) {
	last := 0
	for _, m := range verb.FindAllStringSubmatchIndex(pc.text, -1) {
		f.out = append(f.out, sub(pc, last, m[0]))
		last = m[1]
		if pc.text[m[4]:m[5]] == "%" {
			f.out = append(f.out, piece{text: "%"})
			continue
		}
		if m[2] >= 0 {
			n, _ := strconv.Atoi(pc.text[m[2]:m[3]])
			f.next = n - 1
		}
		if f.next >= 0 && f.next < len(f.args) {
			f.out = append(f.out, f.args[f.next]...)
		} else {
			f.out = append(f.out, piece{text: unknownMark})
		}
		f.next++
	}
	f.out = append(f.out, sub(pc, last, len(pc.text)))
}

// sub cuts text[from:to] out of a literal piece, keeping its location.
func sub(pc piece, from, to int) piece {
	return piece{text: pc.text[from:to], lit: pc.lit, raw: pc.raw, off: pc.off + from, use: pc.use, cond: pc.cond}
}

// foldJoin folds strings.Join(elems, sep) when elems is a composite literal
// (directly or through a package-level variable); anything else is unknown.
func (p *pkgScan) foldJoin(call *ast.CallExpr, seen []string) []piece {
	elems := call.Args[0]
	if id, ok := elems.(*ast.Ident); ok {
		if v, ok := p.decls[id.Name].(*ast.CompositeLit); ok {
			p.used[id.Name] = true
			elems = v
		}
	}
	lit, ok := elems.(*ast.CompositeLit)
	if !ok {
		return append(p.fold(elems, seen), p.fold(call.Args[1], seen)...)
	}
	sep := p.fold(call.Args[1], seen)
	var out []piece
	for i, el := range lit.Elts {
		if i > 0 {
			out = append(out, sep...)
		}
		pieces := p.fold(el, seen)
		if lit != call.Args[0] {
			pieces = usedAt(pieces, call.Args[0].Pos())
		}
		out = append(out, pieces...)
	}
	return out
}

// isCall reports whether call is pkg.fn(...).
func isCall(call *ast.CallExpr, pkg, fn string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != fn {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg
}
