package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestKnownActionsCoverEveryConstant is the drift guard between the Action*
// constants and the knownActions set that decides whether a filter value is a
// real action or a typo. It parses this package's own source rather than
// restating the list: a new auditable operation adds a constant, and the moment
// it does, this test fails unless the set learned about it too. Without that,
// a brand new action would be filterable everywhere except on the audit page,
// where it would answer "nothing happened".
func TestKnownActionsCoverEveryConstant(t *testing.T) {
	t.Parallel()

	declared := actionConstants(t)
	if len(declared) < 50 {
		t.Fatalf("found %d Action* constants, want the whole block — the parse went wrong", len(declared))
	}
	for name, value := range declared {
		if !KnownAction(value) {
			t.Errorf("%s = %q is not in knownActions; add it there too", name, value)
		}
	}
	if len(knownActions) != len(declared) {
		t.Errorf("knownActions has %d entries, the constants declare %d — one side is stale",
			len(knownActions), len(declared))
	}
}

// activityLabelsPattern extracts the own-activity page's ACTION_LABEL_KEYS
// literal from web/src/lib/activityView.ts, and activityLabelKeyPattern each
// quoted action of it.
var (
	activityLabelsPattern   = regexp.MustCompile(`(?s)const ACTION_LABEL_KEYS: Record<string, ParseKeys> = \{(.*?)\n\}`)
	activityLabelKeyPattern = regexp.MustCompile(`(?m)^\s*'([a-z_.]+)':`)
)

// TestActivityPageNamesEveryAction is the drift guard between knownActions and
// the words the own-activity page (/account/activity) reads each action in. A
// missing one is not an error anywhere at runtime — the page falls back to the
// raw label — so an action shipped without its words reads as `task.create`
// for everybody, which only this test notices. A label for an action nobody
// records any more is flagged too.
func TestActivityPageNamesEveryAction(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "web", "src", "lib", "activityView.ts")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	literal := activityLabelsPattern.FindSubmatch(source)
	if literal == nil {
		t.Fatalf("%s no longer declares ACTION_LABEL_KEYS as an object literal", path)
	}
	labelled := map[string]bool{}
	for _, match := range activityLabelKeyPattern.FindAllSubmatch(literal[1], -1) {
		labelled[string(match[1])] = true
	}
	for action := range knownActions {
		if !labelled[action] {
			t.Errorf("%s: ACTION_LABEL_KEYS has no words for %q", path, action)
		}
	}
	for action := range labelled {
		if !KnownAction(action) {
			t.Errorf("%s: ACTION_LABEL_KEYS names %q, which is no audit action", path, action)
		}
	}
}

// TestKnownAction verifies the membership test itself: a real label is accepted,
// a misspelling and the empty string are not.
func TestKnownAction(t *testing.T) {
	t.Parallel()

	for _, action := range []string{ActionPhotoUpdate, ActionFaceAssign, ActionLibraryReset} {
		if !KnownAction(action) {
			t.Errorf("KnownAction(%q) = false, want true", action)
		}
	}
	for _, action := range []string{"", "photo.updat", "photos.update", "PHOTO.UPDATE", "nonsense"} {
		if KnownAction(action) {
			t.Errorf("KnownAction(%q) = true, want false", action)
		}
	}
}

// actionConstants parses audit.go and returns every Action*-named constant with
// a string-literal value, keyed by constant name.
func actionConstants(t *testing.T) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "audit.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing audit.go: %v", err)
	}
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != len(value.Values) {
				continue
			}
			for i, name := range value.Names {
				if !strings.HasPrefix(name.Name, "Action") {
					continue
				}
				literal, ok := value.Values[i].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquoting %s: %v", name.Name, err)
				}
				out[name.Name] = unquoted
			}
		}
	}
	return out
}
