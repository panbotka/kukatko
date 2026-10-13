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
}

// funcScope is what the walk knows about one function body's local variables:
// every value assigned to each, and the appearance its current value is being
// accumulated in (`q := "SELECT …"; q += " WHERE …"` is one statement).
type funcScope struct {
	id      int
	values  map[string][]ast.Expr
	current map[string]int
}

// newPkgScan indexes the package-level constants and variables of files.
func newPkgScan(fset *token.FileSet, files []*ast.File) *pkgScan {
	p := &pkgScan{fset: fset, decls: make(map[string]ast.Expr), used: make(map[string]bool)}
	for _, file := range files {
		for _, decl := range file.Decls {
			if gen, ok := decl.(*ast.GenDecl); ok && (gen.Tok == token.CONST || gen.Tok == token.VAR) {
				eachValue(gen.Specs, func(name *ast.Ident, value ast.Expr) { p.decls[name.Name] = value })
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
			if d.Body != nil {
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

// scanBody walks one function body with a fresh scope of its locals. Function
// literals inside it share the scope: a closure sees the same variables.
func (p *pkgScan) scanBody(body *ast.BlockStmt) {
	p.scopes++
	p.scope = &funcScope{id: p.scopes, values: localValues(body), current: make(map[string]int)}
	ast.Inspect(body, p.visit)
	p.scope = nil
}

// localValues collects every value assigned to each local name in body, in
// source order: `:=`, `=`, `+=` and `var x = …`. A multi-value assignment from
// one call gives every name that call.
func localValues(body *ast.BlockStmt) map[string][]ast.Expr {
	values := make(map[string][]ast.Expr)
	add := func(lhs ast.Expr, rhs ast.Expr) {
		if id, ok := lhs.(*ast.Ident); ok && id.Name != "_" {
			values[id.Name] = append(values[id.Name], rhs)
		}
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				add(lhs, assignedValue(n, i))
			}
		case *ast.DeclStmt:
			if gen, ok := n.Decl.(*ast.GenDecl); ok {
				eachValue(gen.Specs, func(name *ast.Ident, value ast.Expr) { add(name, value) })
			}
		}
		return true
	})
	return values
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
			p.accumulate(id.Name, n.Rhs[i], n.Tok == token.ADD_ASSIGN)
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
			p.accumulate(n.Names[i].Name, v, false)
			continue
		}
		ast.Inspect(v, p.visit)
	}
}

// accumulate folds one value assigned to a local. `+=`, and `x = x + …`, extend
// the statement the local is building; any other assignment starts a new one.
func (p *pkgScan) accumulate(name string, rhs ast.Expr, add bool) {
	if bin, ok := rhs.(*ast.BinaryExpr); ok && bin.Op == token.ADD {
		if id, ok := leftmost(bin).(*ast.Ident); ok && id.Name == name {
			add = true
		}
	}
	pieces := p.fold(rhs, []string{name})
	if idx, ok := p.scope.current[name]; ok && add {
		p.appearances[idx].pieces = append(p.appearances[idx].pieces, pieces...)
		return
	}
	p.scope.current[name] = len(p.appearances)
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
