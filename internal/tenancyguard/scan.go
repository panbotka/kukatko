package tenancyguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Finding is one reference to a root table, or one read of a read-scoped
// table, from a statement that names no project: the place a forgotten
// `WHERE project_id = …` would leak rows.
type Finding struct {
	// Path is the slash-separated file path, relative to the scanned root.
	Path string
	// Line is the statement's use site: the line of the first unscoped table
	// name when it is written in the expression itself, or the line naming
	// the package-level constant (or variable) that holds it.
	Line int
	// Origin is the `path:line` of that table name in its string literal; it
	// differs from Key when the name sits in a constant used here.
	Origin string
	// Tables are the tables the statement reads or writes unscoped.
	Tables []string
	// Statement is a whitespace-collapsed excerpt of the folded statement.
	Statement string
}

// Key returns the finding's allow-list key, `path:line`.
func (f Finding) Key() string {
	return fmt.Sprintf("%s:%d", f.Path, f.Line)
}

// String renders the finding for a test failure.
func (f Finding) String() string {
	via := ""
	if f.Origin != "" && f.Origin != f.Key() {
		via = " (table named at " + f.Origin + ")"
	}
	return fmt.Sprintf("%s: statement over %s names no project%s: %s",
		f.Key(), strings.Join(f.Tables, ", "), via, f.Statement)
}

// Scan parses every non-test .go file below the given directories of root and
// returns the root-table references and read-scoped-table reads whose
// statement names no project, sorted by path and line. Packages are scanned concurrently. It returns an error if a
// directory cannot be walked or a file cannot be parsed.
func Scan(root string, dirs ...string) ([]Finding, error) {
	packages, err := goPackages(root, dirs)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	results := make([][]Finding, len(packages))
	errs := make([]error, len(packages))
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, files := range packages {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i], errs[i] = scanPackage(fset, root, files)
		})
	}
	wg.Wait()
	var findings []Finding
	for i := range packages {
		if errs[i] != nil {
			return nil, errs[i]
		}
		findings = append(findings, results[i]...)
	}
	sort.Slice(findings, func(a, b int) bool {
		if findings[a].Path != findings[b].Path {
			return findings[a].Path < findings[b].Path
		}
		return findings[a].Line < findings[b].Line
	})
	return findings, nil
}

// goPackages lists the non-test .go files below dirs of root, grouped by
// directory (one directory is one package for the scan).
func goPackages(root string, dirs []string) ([][]string, error) {
	byDir := make(map[string][]string)
	for _, dir := range dirs {
		if err := filepath.WalkDir(filepath.Join(root, dir), collectGoFile(byDir)); err != nil {
			return nil, fmt.Errorf("walk %s: %w", dir, err)
		}
	}
	keys := make([]string, 0, len(byDir))
	for k := range byDir {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	packages := make([][]string, 0, len(keys))
	for _, k := range keys {
		packages = append(packages, byDir[k])
	}
	return packages, nil
}

// collectGoFile returns the WalkDir callback that files every non-test .go
// file under its directory in byDir, skipping testdata and node_modules.
func collectGoFile(byDir map[string][]string) fs.WalkDirFunc {
	return func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && (d.Name() == "testdata" || d.Name() == "node_modules"):
			return filepath.SkipDir
		case !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go"):
			byDir[filepath.Dir(p)] = append(byDir[filepath.Dir(p)], p)
		}
		return nil
	}
}

// scanPackage parses one package's files and returns its findings.
func scanPackage(fset *token.FileSet, root string, files []string) ([]Finding, error) {
	parsed := make([]*ast.File, 0, len(files))
	for _, f := range files {
		file, err := parser.ParseFile(fset, f, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		parsed = append(parsed, file)
	}
	return ScanFiles(fset, root, parsed), nil
}

// ScanFiles returns the findings of one package given as its parsed files. File
// paths in the findings are made relative to root. Package-level string
// constants and variables are resolved across all the files given.
func ScanFiles(fset *token.FileSet, root string, files []*ast.File) []Finding {
	p := newPkgScan(fset, files)
	for _, file := range files {
		p.scanFile(file)
	}
	return p.findings(root)
}

// whitespace collapses runs of whitespace in a statement excerpt.
var whitespace = regexp.MustCompile(`\s+`)

// excerptLen caps a finding's statement excerpt.
const excerptLen = 160

// unscopedStmt is one statement an appearance leaves without its project
// column, anchored at the use site of its first unscoped table reference
// (pos) and located at that name in its literal (origin).
type unscopedStmt struct {
	pos, origin token.Position
	tables      []string
	stmt        string
}

// findings evaluates every appearance and returns the statements that some
// counted appearance leaves unscoped, one per source line. An appearance owned
// by a constant, variable or local that another expression folds in is not
// counted: that use is, with whatever it concatenates around the value.
func (p *pkgScan) findings(root string) []Finding {
	byLine := make(map[token.Position]int)
	var out []Finding
	for _, a := range p.appearances {
		if a.owner != "" && p.used[a.owner] {
			continue
		}
		for _, u := range p.evaluate(a.pieces) {
			if i, seen := byLine[u.pos]; seen {
				out[i].Tables = mergeTables(out[i].Tables, u.tables)
				continue
			}
			byLine[u.pos] = len(out)
			out = append(out, Finding{Path: relPath(root, u.pos.Filename), Line: u.pos.Line,
				Origin:    fmt.Sprintf("%s:%d", relPath(root, u.origin.Filename), u.origin.Line),
				Tables:    u.tables,
				Statement: u.stmt})
		}
	}
	return out
}

// relPath returns file relative to root, slash-separated.
func relPath(root, file string) string {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		rel = file
	}
	return filepath.ToSlash(rel)
}

// mergeTables returns the sorted union of two table lists.
func mergeTables(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, t := range b {
		if !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}

// evaluate blanks the SQL comments of one folded expression, splits it into
// statements and returns those with an item no predicate ties to one project
// (unscopedRefs), each anchored at its first unscoped item. An expression
// with conditionally appended pieces is judged twice — whole, and without
// them — since the statement may run either way.
func (p *pkgScan) evaluate(pieces []piece) []unscopedStmt {
	out := p.evaluateText(pieces)
	if slices.ContainsFunc(pieces, func(pc piece) bool { return pc.cond }) {
		bare := slices.DeleteFunc(slices.Clone(pieces), func(pc piece) bool { return pc.cond })
		out = append(out, p.evaluateText(bare)...)
	}
	return out
}

// evaluateText judges the statements of one folded text.
func (p *pkgScan) evaluateText(pieces []piece) []unscopedStmt {
	var text strings.Builder
	starts := make([]int, len(pieces))
	for i, pc := range pieces {
		starts[i] = text.Len()
		text.WriteString(pc.text)
	}
	var out []unscopedStmt
	stmts, offsets := splitStatements(blankComments(text.String()))
	for i, stmt := range stmts {
		if u, ok := p.judge(stmt, offsets[i], pieces, starts); ok {
			out = append(out, u)
		}
	}
	return out
}

// judge checks one statement starting at offset in the folded text and
// returns it when some item in it is unscoped (unscopedRefs), located at the
// first such item.
func (p *pkgScan) judge(stmt string, offset int, pieces []piece, starts []int) (unscopedStmt, bool) {
	var u unscopedStmt
	for _, ref := range unscopedRefs(stmt) {
		if u.tables == nil {
			pos, origin, ok := p.position(pieces, starts, offset+ref.start)
			if !ok {
				continue
			}
			u.pos, u.origin, u.stmt = pos, origin, excerpt(stmt)
		}
		u.tables = mergeTables(u.tables, []string{ref.table})
	}
	return u, u.tables != nil
}

// position maps an offset in the folded text back to the source: the anchor
// (the use of the package-level name the literal was folded in through, or
// the literal itself) and the origin (the line of the offset in the literal).
// A raw string literal spans lines, so the newlines in front of the offset
// are counted; an interpreted one sits on one line.
func (p *pkgScan) position(pieces []piece, starts []int, at int) (anchor, origin token.Position, ok bool) {
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > at }) - 1
	if i < 0 || pieces[i].lit == nil {
		return token.Position{}, token.Position{}, false
	}
	pc := pieces[i]
	origin = p.fset.Position(pc.lit.Pos())
	if strings.HasPrefix(pc.lit.Value, "`") {
		origin.Line += strings.Count(pc.raw[:pc.off+at-starts[i]], "\n")
	}
	origin.Column = 0
	anchor = origin
	if pc.use.IsValid() {
		anchor = p.fset.Position(pc.use)
		anchor.Column = 0
	}
	return anchor, origin, true
}

// excerpt collapses a statement's whitespace and caps its length.
func excerpt(stmt string) string {
	s := strings.TrimSpace(whitespace.ReplaceAllString(stmt, " "))
	if len(s) > excerptLen {
		s = s[:excerptLen] + "…"
	}
	return s
}
