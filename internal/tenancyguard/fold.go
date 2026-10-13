package tenancyguard

import (
	"go/ast"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// projectMark stands in the folded text for a piece the scan cannot read but
// whose name says it is the project predicate (`projectClause("$1")`,
// `env.project`, `projectFilter`). It contains no `;`, so it never splits a
// statement.
const projectMark = "\x00project\x00"

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
		return isCall(e, "fmt", "Sprintf") || isCall(e, "strings", "Join")
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
// fmt.Sprintf and strings.Join; any other call is unknown.
func (p *pkgScan) foldCall(call *ast.CallExpr, seen []string) []piece {
	if isCall(call, "fmt", "Sprintf") && len(call.Args) > 0 {
		return p.foldSprintf(call, seen)
	}
	if isCall(call, "strings", "Join") && len(call.Args) == 2 {
		return p.foldJoin(call, seen)
	}
	return []piece{p.unknown(call)}
}

// foldIdent folds a name: a local variable becomes every value assigned to it,
// a package-level string constant or variable its value. A name already being
// folded (`q = q + …`) contributes nothing; anything else is unknown.
func (p *pkgScan) foldIdent(id *ast.Ident, seen []string) []piece {
	if slices.Contains(seen, id.Name) {
		return nil
	}
	p.resolving++
	defer func() { p.resolving-- }()
	if p.isLocal(id.Name) {
		p.used[p.localKey(id.Name)] = true
		var out []piece
		for _, v := range p.scope.values[id.Name] {
			out = append(out, p.fold(v, append(seen, id.Name))...)
		}
		return out
	}
	if v, ok := p.decls[id.Name]; ok && p.isStringExpr(v) {
		p.used[id.Name] = true
		return p.fold(v, append(seen, id.Name))
	}
	return []piece{p.unknown(id)}
}

// unknown stands in for an expression the scan cannot read. Literals nested in
// it are still scanned as appearances of their own — except while a name is
// being resolved, as the walk reaches that value where it is defined. The
// stand-in is projectMark when the expression is named after the project or
// carries a literal that names a project column.
func (p *pkgScan) unknown(e ast.Expr) piece {
	if p.resolving == 0 {
		ast.Inspect(e, p.visit)
	}
	if mentionsProject(e) {
		return piece{text: projectMark}
	}
	return piece{text: unknownMark}
}

// mentionsProject reports whether e names the project: an identifier with
// "project" in its name, or a string literal holding a project column.
func mentionsProject(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if strings.Contains(strings.ToLower(n.Name), "project") {
				found = true
			}
		case *ast.BasicLit:
			for _, col := range projectColumns {
				if strings.Contains(n.Value, col) {
					found = true
				}
			}
		}
		return !found
	})
	return found
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
	return piece{text: pc.text[from:to], lit: pc.lit, raw: pc.raw, off: pc.off + from}
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
		out = append(out, p.fold(el, seen)...)
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
