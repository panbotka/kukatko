package database_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/panbotka/kukatko/internal/audit"
)

// The files that carry migration 0120's literal list of project-scoped actions.
const (
	auditProjectBackfillMigration = "migrations/0120_audit_log_project_backfill.sql"
	auditProjectBackfillDown      = "../../docs/migrations-rollback/0120_down.sql"
)

// actionsAddedAfter0120 are the project-scoped actions introduced after
// migration 0120 shipped. No entry of them can predate the bootstrap project, so
// the frozen migration need not (and, applied, must not) learn them; list a new
// one here instead of editing the migration.
var actionsAddedAfter0120 = map[string]bool{}

// quotedLabel is one single-quoted action label of the list.
var quotedLabel = regexp.MustCompile(`'([^']+)'`)

// backfillActions returns the labels of the `action IN (...)` list of the SQL
// file name, in the order written.
func backfillActions(t *testing.T, name string) []string {
	t.Helper()
	body, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	_, list, found := strings.Cut(string(body), "action IN (")
	if !found {
		t.Fatalf("%s has no `action IN (` list", name)
	}
	list, _, found = strings.Cut(list, ")")
	if !found {
		t.Fatalf("%s: the action list is not closed", name)
	}
	matches := quotedLabel.FindAllStringSubmatch(list, -1)
	actions := make([]string, 0, len(matches))
	for _, m := range matches {
		actions = append(actions, m[1])
	}
	return actions
}

// TestAuditProjectBackfill_listIsProjectScoped keeps the action list of
// migration 0120 and of its down script equal to the audit package's own
// authority: every listed action is project-scoped (an instance action filed
// under a project would leak into its trail), and every project-scoped action
// is listed once unless it is newer than the migration (one left out would
// keep its pre-tenancy history out of the project).
func TestAuditProjectBackfill_listIsProjectScoped(t *testing.T) {
	var want []string
	for _, action := range audit.ProjectScopedActions() {
		if !actionsAddedAfter0120[action] {
			want = append(want, action)
		}
	}
	for _, name := range []string{auditProjectBackfillMigration, auditProjectBackfillDown} {
		got := backfillActions(t, name)
		for _, action := range got {
			if !audit.ProjectScoped(action) {
				t.Errorf("%s lists %q, which is not a project-scoped action", name, action)
			}
		}
		sorted := slices.Sorted(slices.Values(got))
		if !slices.Equal(sorted, want) {
			t.Errorf("%s lists %d actions, want the %d of audit.ProjectScopedActions:\n got %v\nwant %v",
				name, len(got), len(want), sorted, want)
		}
	}
}
