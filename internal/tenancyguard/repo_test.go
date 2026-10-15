package tenancyguard_test

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/tenancyguard"
)

// repoRoot returns the repository root: two levels above this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	return root
}

// TestRepository_rootStatementsNameTheirProject is the guard itself: every SQL
// statement under internal/ and cmd/ that reads or writes a root table must
// mention its project column, or be allow-listed with a reason.
func TestRepository_rootStatementsNameTheirProject(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	start := time.Now()
	findings, err := tenancyguard.Scan(root, "internal", "cmd")
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	t.Logf("scanned internal/ and cmd/ in %s: %d unscoped statements", time.Since(start), len(findings))

	f, err := os.Open("allow.txt")
	if err != nil {
		t.Fatalf("open allow.txt: %v", err)
	}
	defer f.Close()
	entries, err := tenancyguard.ParseAllowList(f)
	if err != nil {
		t.Fatalf("allow.txt: %v", err)
	}
	unallowed, stale := tenancyguard.Judge(findings, entries)
	for _, u := range unallowed {
		t.Errorf("%s\n\tscope it with its project column, or — if it must span every project — add "+
			"`%s <reason>` to internal/tenancyguard/allow.txt", u, u.Key())
	}
	for _, e := range stale {
		t.Errorf("stale allow-list entry (allow.txt line %d): %s no longer holds an unscoped statement; "+
			"remove it or move it to the statement's new line", e.Line, e.Key)
	}
}

// TestRootTables_matchMigrations keeps RootTables honest: the tables that carry
// a project column in the migrations are exactly the root tables, the
// read-scoped tables and the documented exemptions, each with the same column.
func TestRootTables_matchMigrations(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	inSchema, err := tenancyguard.ProjectColumnTables(os.DirFS(root), "internal/database/migrations")
	if err != nil {
		t.Fatalf("ProjectColumnTables: %v", err)
	}
	for table, col := range inSchema {
		if want, ok := tenancyguard.RootTables[table]; ok {
			if col != want {
				t.Errorf("root table %s is scoped by %s in the schema, RootTables says %s", table, col, want)
			}
			continue
		}
		if want, ok := tenancyguard.ReadScopedTables[table]; ok {
			if col != want {
				t.Errorf("read-scoped table %s is scoped by %s in the schema, ReadScopedTables says %s",
					table, col, want)
			}
			continue
		}
		if _, ok := tenancyguard.ExemptTables[table]; !ok {
			t.Errorf("table %s has a %s column but is in none of RootTables, ReadScopedTables and "+
				"ExemptTables (internal/tenancyguard/tables.go)", table, col)
		}
	}
	listed := make([]string, 0,
		len(tenancyguard.RootTables)+len(tenancyguard.ReadScopedTables)+len(tenancyguard.ExemptTables))
	for table := range tenancyguard.RootTables {
		listed = append(listed, table)
	}
	for table := range tenancyguard.ReadScopedTables {
		listed = append(listed, table)
	}
	for table := range tenancyguard.ExemptTables {
		listed = append(listed, table)
	}
	sort.Strings(listed)
	for _, table := range listed {
		if _, ok := inSchema[table]; !ok {
			t.Errorf("%s is listed in tables.go but has no project column in the migrations", table)
		}
	}
}

// TestKeyedChildTables_matchMigrations derives the child tables from the
// schema — every table without a project column that has a foreign key into a
// root table, or into another such child — and requires KeyedChildTables to
// list exactly them, each keyed by exactly its foreign keys into those tables
// (plus, optionally, its own uid). A new child table, or a new parent key of
// an old one, cannot be forgotten.
func TestKeyedChildTables_matchMigrations(t *testing.T) {
	t.Parallel()
	fsys := os.DirFS(repoRoot(t))
	const dir = "internal/database/migrations"
	fks, err := tenancyguard.ForeignKeys(fsys, dir)
	if err != nil {
		t.Fatalf("ForeignKeys: %v", err)
	}
	projectCols, err := tenancyguard.ProjectColumnTables(fsys, dir)
	if err != nil {
		t.Fatalf("ProjectColumnTables: %v", err)
	}
	derived := tenancyguard.ChildTables(fks, projectCols)
	for table, keys := range derived {
		listed, ok := tenancyguard.KeyedChildTables[table]
		if !ok {
			t.Errorf("table %s has no project column but foreign keys %v into the catalogue; add it to "+
				"KeyedChildTables (internal/tenancyguard/tables.go)", table, keys)
			continue
		}
		got := slices.DeleteFunc(slices.Clone(listed), func(k string) bool { return k == "uid" })
		slices.Sort(got)
		if !slices.Equal(got, keys) {
			t.Errorf("KeyedChildTables[%s] = %v, want its foreign keys into the catalogue %v (plus uid)",
				table, listed, keys)
		}
	}
	for table := range tenancyguard.KeyedChildTables {
		if _, ok := derived[table]; !ok {
			t.Errorf("%s is listed in KeyedChildTables but the migrations give it no foreign key into "+
				"the catalogue (or a project column)", table)
		}
	}
}
