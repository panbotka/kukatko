package tenancyguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// projectImportPath is the package that declares the bootstrap project's
// identifiers.
const projectImportPath = "github.com/panbotka/kukatko/internal/project"

// bootstrapIdents are the identifiers of internal/project that name the
// bootstrap project `v`.
var bootstrapIdents = map[string]bool{"BootstrapUID": true, "BootstrapSlug": true}

// bootstrapUIDLiteral is the bootstrap project's uid, which a string literal
// could spell out instead of naming the constant.
const bootstrapUIDLiteral = "pj0000000000000000veselice"

// BootstrapReference is one place in a non-test Go file that names the
// bootstrap project: project.BootstrapUID, project.BootstrapSlug, or a string
// literal carrying the bootstrap uid.
type BootstrapReference struct {
	// File is the path relative to the scanned root, slash-separated.
	File string
	// Line is the 1-based line of the reference.
	Line int
	// What is the reference as written (`project.BootstrapUID`, or the literal).
	What string
}

// String renders the reference as `file:line what`.
func (r BootstrapReference) String() string {
	return fmt.Sprintf("%s:%d %s", r.File, r.Line, r.What)
}

// BootstrapReferences returns every reference to the bootstrap project in the
// non-test .go files of the directories under root that match one of the
// patterns (filepath.Match syntax, slash-separated, relative to root — e.g.
// "internal/*api", "cmd/kukatko"), sorted by file and line. Only the
// directory itself is scanned, not its subdirectories.
//
// It is the request-path half of the "there is no default project" rule: the
// bootstrap project is a row like any other once the transition is over, so
// no handler and no server wiring may single it out — only the migrations, the
// rekey runbook and the tests know its name.
func BootstrapReferences(root string, patterns ...string) ([]BootstrapReference, error) {
	var refs []BootstrapReference
	for _, pattern := range patterns {
		dirs, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		if err != nil {
			return nil, fmt.Errorf("tenancyguard: pattern %q: %w", pattern, err)
		}
		for _, dir := range dirs {
			found, err := bootstrapReferencesIn(root, dir)
			if err != nil {
				return nil, err
			}
			refs = append(refs, found...)
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].File != refs[j].File {
			return refs[i].File < refs[j].File
		}
		return refs[i].Line < refs[j].Line
	})
	return refs, nil
}

// bootstrapReferencesIn scans the non-test .go files directly inside dir.
func bootstrapReferencesIn(root, dir string) ([]BootstrapReference, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("tenancyguard: reading %s: %w", dir, err)
	}
	var refs []BootstrapReference
	for _, entry := range entries {
		if !isScannedGoFile(entry) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		found, err := bootstrapReferencesInFile(root, path)
		if err != nil {
			return nil, err
		}
		refs = append(refs, found...)
	}
	return refs, nil
}

// isScannedGoFile reports whether entry is a non-test Go source file.
func isScannedGoFile(entry fs.DirEntry) bool {
	name := entry.Name()
	return !entry.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

// bootstrapReferencesInFile parses one file and collects its references.
func bootstrapReferencesInFile(root, path string) ([]BootstrapReference, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("tenancyguard: parsing %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("tenancyguard: %s relative to %s: %w", path, root, err)
	}
	rel = filepath.ToSlash(rel)
	projectName := importName(file, projectImportPath)
	var refs []BootstrapReference
	ast.Inspect(file, func(n ast.Node) bool {
		if what, ok := bootstrapReference(n, projectName); ok {
			refs = append(refs, BootstrapReference{File: rel, Line: fset.Position(n.Pos()).Line, What: what})
		}
		return true
	})
	return refs, nil
}

// bootstrapReference reports whether n names the bootstrap project — a
// selector of one of bootstrapIdents on the package imported as projectName
// ("" when the file does not import it), or a string literal carrying the
// bootstrap uid — and how it is written.
func bootstrapReference(n ast.Node, projectName string) (string, bool) {
	switch node := n.(type) {
	case *ast.SelectorExpr:
		pkg, ok := node.X.(*ast.Ident)
		if ok && projectName != "" && pkg.Name == projectName && bootstrapIdents[node.Sel.Name] {
			return pkg.Name + "." + node.Sel.Name, true
		}
	case *ast.BasicLit:
		if node.Kind == token.STRING && strings.Contains(node.Value, bootstrapUIDLiteral) {
			return node.Value, true
		}
	}
	return "", false
}

// importName returns the name file refers to the package importPath by — its
// alias, or the last path element — or "" when file does not import it (or
// imports it blank or dot).
func importName(file *ast.File, importPath string) string {
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || path != importPath {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name == "_" || spec.Name.Name == "." {
				return ""
			}
			return spec.Name.Name
		}
		return path[strings.LastIndex(path, "/")+1:]
	}
	return ""
}
