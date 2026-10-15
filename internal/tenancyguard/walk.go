package tenancyguard

import (
	"fmt"
	"go/ast"
	"go/token"
)

// appearance is one statement candidate found in the package, folded.
type appearance struct {
	pieces []piece
	// owner names the constant, variable or local the expression is the value
	// of, or is empty. An owned appearance whose owner some other expression
	// folds in is judged there, with what that use site adds around it.
	owner string
}

// pkgScan folds the string expressions of one package.
type pkgScan struct {
	fset *token.FileSet
	// decls are the package-level constant and variable values by name.
	decls map[string]ast.Expr
	// used records the owners some folded expression resolved.
	used        map[string]bool
	appearances []appearance
	// scope is the function body being walked, nil at package level.
	scope *funcScope
	// resolving counts the names being folded in place of their use.
	resolving int
	// scopes counts the function bodies walked, to name their locals uniquely.
	scopes int
	// helpers are the package's SQL helpers by name (isHelper).
	helpers map[string]*ast.FuncDecl
	// args are the folded arguments of the helper call being inlined, by
	// parameter name; nil outside one.
	args map[string][]piece
}

// funcScope is what the walk knows about one function body's local variables:
// every value assigned to each, and the appearance its current value is being
// accumulated in (`q := "SELECT …"; q += " WHERE …"` is one statement), with
// the branch depth that statement started at.
type funcScope struct {
	id      int
	values  map[string][]localValue
	current map[string]int
	started map[string]int
	// depth is the branch depth (depthOf) of every assignment and declaration.
	depth map[ast.Node]int
}

// localValue is one value assigned to a local; cond when it is assigned under
// a branch or loop the local's first value is not under.
type localValue struct {
	expr ast.Expr
	cond bool
}

// newPkgScan indexes the package-level constants and variables of files.
func newPkgScan(fset *token.FileSet, files []*ast.File) *pkgScan {
	p := &pkgScan{fset: fset, decls: make(map[string]ast.Expr), used: make(map[string]bool),
		helpers: make(map[string]*ast.FuncDecl)}
	for _, file := range files {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.GenDecl:
				if d.Tok == token.CONST || d.Tok == token.VAR {
					eachValue(d.Specs, func(name *ast.Ident, value ast.Expr) { p.decls[name.Name] = value })
				}
			case *ast.FuncDecl:
				if isHelper(d) {
					p.helpers[d.Name.Name] = d
				}
			}
		}
	}
	return p
}

// eachValue calls fn for every name of the value specs that is given a value.
func eachValue(specs []ast.Spec, fn func(name *ast.Ident, value ast.Expr)) {
	for _, spec := range specs {
		if vs, ok := spec.(*ast.ValueSpec); ok {
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					fn(name, vs.Values[i])
				}
			}
		}
	}
}

// scanFile records every statement candidate of file.
func (p *pkgScan) scanFile(file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.IMPORT {
				p.scanGenDecl(d)
			}
		case *ast.FuncDecl:
			if p.helpers[d.Name.Name] == d {
				p.scanHelper(d)
			} else if d.Body != nil {
				p.scanBody(d.Body)
			}
		}
	}
}

// scanGenDecl records the values of a package-level declaration under their
// names; type declarations are inspected for nested literals only.
func (p *pkgScan) scanGenDecl(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			ast.Inspect(spec, p.visit)
			continue
		}
		for i, v := range vs.Values {
			if i < len(vs.Names) && p.isStringExpr(v) {
				p.record(v, vs.Names[i].Name)
				continue
			}
			ast.Inspect(v, p.visit)
		}
	}
}

// scanHelper records a SQL helper's returned expression as an appearance owned
// by the helper, so it is judged at each call that inlines it — and on its own
// only when nothing calls it.
func (p *pkgScan) scanHelper(fn *ast.FuncDecl) {
	p.record(helperReturn(fn), "func "+fn.Name.Name)
}

// scanBody walks one function body with a fresh scope of its locals. Function
// literals inside it share the scope: a closure sees the same variables.
func (p *pkgScan) scanBody(body *ast.BlockStmt) {
	p.scopes++
	depth := depthOf(body)
	p.scope = &funcScope{id: p.scopes, values: localValues(body, depth), current: make(map[string]int),
		started: make(map[string]int), depth: depth}
	ast.Inspect(body, p.visit)
	p.scope = nil
}

// depthOf returns the branch depth of every assignment and declaration in
// body: the number of blocks, case clauses and select clauses around it.
// A statement deeper than another one's start may not run with it.
func depthOf(body *ast.BlockStmt) map[ast.Node]int {
	depth := make(map[ast.Node]int)
	var stack []ast.Node
	level := 0
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if opensBranch(top) {
				level--
			}
			return true
		}
		stack = append(stack, n)
		if opensBranch(n) {
			level++
		}
		switch n.(type) {
		case *ast.AssignStmt, *ast.ValueSpec:
			depth[n] = level
		}
		return true
	})
	return depth
}

// opensBranch reports whether n is a block or clause a statement may be
// skipped in.
func opensBranch(n ast.Node) bool {
	switch n.(type) {
	case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
		return true
	}
	return false
}

// localValues collects every value assigned to each local name in body, in
// source order: `:=`, `=`, `+=` and `var x = …`. A multi-value assignment from
// one call gives every name that call. A value assigned deeper (depth) than
// the name's first value is conditional.
func localValues(body *ast.BlockStmt, depth map[ast.Node]int) map[string][]localValue {
	lv := localCollector{depth: depth, values: make(map[string][]localValue), first: make(map[string]int)}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				lv.add(lhs, assignedValue(n, i), n)
			}
		case *ast.DeclStmt:
			if gen, ok := n.Decl.(*ast.GenDecl); ok {
				lv.declare(gen)
			}
		}
		return true
	})
	return lv.values
}

// localCollector gathers the values of a body's locals (localValues): every
// value per name, and the depth of each name's first one.
type localCollector struct {
	depth  map[ast.Node]int
	values map[string][]localValue
	first  map[string]int
}

// add records rhs as a value of lhs, assigned by the statement at.
func (lc *localCollector) add(lhs, rhs ast.Expr, at ast.Node) {
	id, ok := lhs.(*ast.Ident)
	if !ok || id.Name == "_" {
		return
	}
	d, seen := lc.first[id.Name]
	if !seen {
		lc.first[id.Name], d = lc.depth[at], lc.depth[at]
	}
	lc.values[id.Name] = append(lc.values[id.Name], localValue{expr: rhs, cond: lc.depth[at] > d})
}

// declare records the values of a `var` declaration inside the body.
func (lc *localCollector) declare(gen *ast.GenDecl) {
	for _, spec := range gen.Specs {
		eachValue([]ast.Spec{spec}, func(name *ast.Ident, value ast.Expr) { lc.add(name, value, spec) })
	}
}

// assignedValue returns the expression the i-th left-hand side of n receives:
// its own right-hand side, or the one call of a multi-value assignment.
func assignedValue(n *ast.AssignStmt, i int) ast.Expr {
	if len(n.Rhs) == len(n.Lhs) {
		return n.Rhs[i]
	}
	return n.Rhs[0]
}

// localKey names a local variable of the current scope as an owner.
func (p *pkgScan) localKey(name string) string {
	return fmt.Sprintf("#%d.%s", p.scope.id, name)
}

// isLocal reports whether name is a local variable of the current scope.
func (p *pkgScan) isLocal(name string) bool {
	if p.scope == nil {
		return false
	}
	_, ok := p.scope.values[name]
	return ok
}

// visit is the ast.Inspect callback: it records a maximal string expression
// and stops there, accumulates assignments to locals, and skips the
// identifiers that only name things (selectors, struct keys) so they are not
// mistaken for uses.
func (p *pkgScan) visit(n ast.Node) bool {
	if p.visitStructure(n) {
		return false
	}
	switch n := n.(type) {
	case *ast.Ident:
		if v, ok := p.decls[n.Name]; ok && !p.isLocal(n.Name) && p.isStringExpr(v) {
			p.record(n, "")
		}
		return false
	case ast.Expr:
		if p.isStringExpr(n) {
			p.record(n, "")
			return false
		}
	}
	return true
}

// visitStructure handles the nodes whose parts play different roles —
// assignments and declarations (accumulated), selectors and struct keys (whose
// names are not uses) — and reports whether it did.
func (p *pkgScan) visitStructure(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.AssignStmt:
		p.assign(n)
	case *ast.ValueSpec:
		p.declareLocals(n)
	case *ast.SelectorExpr:
		ast.Inspect(n.X, p.visit)
	case *ast.KeyValueExpr:
		if _, ok := n.Key.(*ast.Ident); !ok {
			ast.Inspect(n.Key, p.visit)
		}
		ast.Inspect(n.Value, p.visit)
	default:
		return false
	}
	return true
}

// assign accumulates a one-to-one assignment to locals into their current
// statement; any other assignment is inspected for nested literals.
func (p *pkgScan) assign(n *ast.AssignStmt) {
	if !accumulates(n) {
		for _, e := range append(append([]ast.Expr(nil), n.Lhs...), n.Rhs...) {
			ast.Inspect(e, p.visit)
		}
		return
	}
	for i, lhs := range n.Lhs {
		if id, ok := lhs.(*ast.Ident); ok && p.isLocal(id.Name) {
			p.accumulate(id.Name, n.Rhs[i], n.Tok == token.ADD_ASSIGN, n)
			continue
		}
		ast.Inspect(lhs, p.visit)
		ast.Inspect(n.Rhs[i], p.visit)
	}
}

// accumulates reports whether n assigns one value to each name with `:=`, `=`
// or `+=` — the forms that build a statement in a local.
func accumulates(n *ast.AssignStmt) bool {
	return len(n.Rhs) == len(n.Lhs) &&
		(n.Tok == token.DEFINE || n.Tok == token.ASSIGN || n.Tok == token.ADD_ASSIGN)
}

// declareLocals accumulates `var x = …` inside a function like `x := …`.
func (p *pkgScan) declareLocals(n *ast.ValueSpec) {
	for i, v := range n.Values {
		if i < len(n.Names) && p.isLocal(n.Names[i].Name) {
			p.accumulate(n.Names[i].Name, v, false, n)
			continue
		}
		ast.Inspect(v, p.visit)
	}
}

// accumulate folds one value assigned to a local by the statement at. `+=`,
// and `x = x + …`, extend the statement the local is building — conditionally
// when at is deeper than where that statement started; any other assignment
// starts a new one.
func (p *pkgScan) accumulate(name string, rhs ast.Expr, add bool, at ast.Node) {
	if bin, ok := rhs.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		if id, ok := leftmost(bin).(*ast.Ident); ok && id.Name == name {
			add = true
		}
	}
	pieces := p.fold(rhs, []string{name})
	if idx, ok := p.scope.current[name]; ok && add {
		if p.scope.depth[at] > p.scope.started[name] {
			pieces = conditional(pieces)
		}
		p.appearances[idx].pieces = append(p.appearances[idx].pieces, pieces...)
		return
	}
	p.scope.current[name] = len(p.appearances)
	p.scope.started[name] = p.scope.depth[at]
	p.appearances = append(p.appearances, appearance{pieces: pieces, owner: p.localKey(name)})
}

// leftmost returns the first operand of a chain of additions.
func leftmost(e ast.Expr) ast.Expr {
	for {
		bin, ok := e.(*ast.BinaryExpr)
		if !ok || bin.Op != token.ADD {
			return e
		}
		e = bin.X
	}
}

// record folds e and records it as one appearance owned by owner.
func (p *pkgScan) record(e ast.Expr, owner string) {
	p.appearances = append(p.appearances, appearance{pieces: p.fold(e, nil), owner: owner})
}
