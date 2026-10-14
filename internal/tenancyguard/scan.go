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
	// Line is the line of the table name inside the string literal.
	Line int
	// Tables are the root tables the statement reads or writes unscoped.
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
	return fmt.Sprintf("%s: statement over %s names no project: %s", f.Key(), strings.Join(f.Tables, ", "), f.Statement)
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

// rootRef matches a root table named after the SQL keywords that read or write
// it. The keywords are upper case on purpose: lower-case "update photos" is the
// prose of an error message, not a statement.
var rootRef = func() *regexp.Regexp {
	tables := make([]string, 0, len(RootTables))
	for t := range RootTables {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	return regexp.MustCompile(`\b(?:FROM|JOIN|UPDATE|INTO)\s+(` + strings.Join(tables, "|") + `)\b`)
}()

// readRef matches a read-scoped table named after the SQL keywords that read
// it. A DELETE's own FROM is matched too, as the first alternative, so that the
// leftmost match consumes it and judge can skip it: deleting is a write.
var readRef = func() *regexp.Regexp {
	tables := make([]string, 0, len(ReadScopedTables))
	for t := range ReadScopedTables {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	return regexp.MustCompile(`\b(DELETE\s+FROM|FROM|JOIN)\s+(` + strings.Join(tables, "|") + `)\b`)
}()

// scopedRef is one reference a statement makes to a table the guard checks:
// where the table name sits in the statement and the column that scopes it.
type scopedRef struct {
	start, end int
	column     string
}

// scopedRefs returns the references of stmt the guard checks, in order: every
// root-table reference and every read of a read-scoped table.
func scopedRefs(stmt string) []scopedRef {
	var refs []scopedRef
	for _, m := range rootRef.FindAllStringSubmatchIndex(stmt, -1) {
		refs = append(refs, scopedRef{start: m[2], end: m[3], column: RootTables[stmt[m[2]:m[3]]]})
	}
	for _, m := range readRef.FindAllStringSubmatchIndex(stmt, -1) {
		if strings.HasPrefix(stmt[m[2]:m[3]], "DELETE") {
			continue
		}
		refs = append(refs, scopedRef{start: m[4], end: m[5], column: ReadScopedTables[stmt[m[4]:m[5]]]})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].start < refs[j].start })
	return refs
}

// whitespace collapses runs of whitespace in a statement excerpt.
var whitespace = regexp.MustCompile(`\s+`)

// excerptLen caps a finding's statement excerpt.
const excerptLen = 160

// unscopedStmt is one statement an appearance leaves without its project
// column, located at its first unscoped root-table reference.
type unscopedStmt struct {
	pos    token.Position
	tables []string
	stmt   string
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
			rel, err := filepath.Rel(root, u.pos.Filename)
			if err != nil {
				rel = u.pos.Filename
			}
			byLine[u.pos] = len(out)
			out = append(out, Finding{Path: filepath.ToSlash(rel), Line: u.pos.Line, Tables: u.tables, Statement: u.stmt})
		}
	}
	return out
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

// evaluate splits one folded expression into statements and returns those
// whose root-table references are not all scoped: a reference is scoped when
// its statement names the table's project column, or a project-named piece, in
// a position that constrains or writes the row (predicateColumns) — a project
// column only in a select list scopes nothing.
// Each statement is located at its first unscoped reference.
func (p *pkgScan) evaluate(pieces []piece) []unscopedStmt {
	var text strings.Builder
	starts := make([]int, len(pieces))
	for i, pc := range pieces {
		starts[i] = text.Len()
		text.WriteString(pc.text)
	}
	var out []unscopedStmt
	offset := 0
	for stmt := range strings.SplitSeq(text.String(), ";") {
		if u, ok := p.judge(stmt, offset, pieces, starts); ok {
			out = append(out, u)
		}
		offset += len(stmt) + 1
	}
	return out
}

// judge checks one statement starting at offset in the folded text and
// returns it when some root reference in it is unscoped.
func (p *pkgScan) judge(stmt string, offset int, pieces []piece, starts []int) (unscopedStmt, bool) {
	var u unscopedStmt
	scoped := predicateColumns(stmt)
	if scoped[projectMark] {
		return u, false
	}
	for _, ref := range scopedRefs(stmt) {
		table := stmt[ref.start:ref.end]
		if scoped[ref.column] {
			continue
		}
		if u.tables == nil {
			pos, ok := p.position(pieces, starts, offset+ref.start)
			if !ok {
				continue
			}
			u.pos, u.stmt = pos, excerpt(stmt)
		}
		u.tables = mergeTables(u.tables, []string{table})
	}
	return u, u.tables != nil
}

// position maps an offset in the folded text back to the source line of the
// literal that holds it. A raw string literal spans lines, so the newlines in
// front of the offset are counted; an interpreted one sits on one line.
func (p *pkgScan) position(pieces []piece, starts []int, at int) (token.Position, bool) {
	i := sort.Search(len(starts), func(i int) bool { return starts[i] > at }) - 1
	if i < 0 || pieces[i].lit == nil {
		return token.Position{}, false
	}
	pc := pieces[i]
	pos := p.fset.Position(pc.lit.Pos())
	if strings.HasPrefix(pc.lit.Value, "`") {
		pos.Line += strings.Count(pc.raw[:pc.off+at-starts[i]], "\n")
	}
	pos.Column = 0
	return pos, true
}

// excerpt collapses a statement's whitespace and caps its length.
func excerpt(stmt string) string {
	s := strings.TrimSpace(whitespace.ReplaceAllString(stmt, " "))
	if len(s) > excerptLen {
		s = s[:excerptLen] + "…"
	}
	return s
}
