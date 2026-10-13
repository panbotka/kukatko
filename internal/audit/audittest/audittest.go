// Package audittest is the audit trail's test helper: Guard fails a test whose
// mutations wrote an entry about a project's content without naming the
// project. Such an entry is invisible to the project admin's listing
// (GET /p/{project}/audit filters on audit_log.project_id) and indistinguishable
// from an instance action in the maintainer's, so it is a bug in the writer —
// one that no assertion about the mutation itself would notice.
//
// The API packages' integration tests call Guard right after truncating the
// database, so every mutation they drive through a handler is checked.
package audittest

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// Pooler is the one thing Guard needs from a database: its pool. It is
// satisfied by *database.DB.
type Pooler interface {
	// Pool returns the shared connection pool.
	Pool() *pgxpool.Pool
}

// unscopedSQL reads the entries written after a mark that name no project.
const unscopedSQL = `SELECT id, action FROM audit_log WHERE id > $1 AND project_id IS NULL ORDER BY id`

// Guard marks the end of the audit trail now and, when t finishes, fails t for
// every entry written since whose action is project-scoped
// (audit.ProjectScoped) but that names no project. Instance actions (accounts,
// a project's lifecycle, the retention purge) may name none and are ignored.
// Call it after the database is truncated and before the test mutates
// anything; it must run before the pool is closed, which registering it after
// dbtest.New guarantees (cleanups run last-in, first-out).
func Guard(t *testing.T, db Pooler) {
	t.Helper()
	var mark int64
	if err := db.Pool().QueryRow(t.Context(), `SELECT COALESCE(max(id), 0) FROM audit_log`).Scan(&mark); err != nil {
		t.Fatalf("audittest: reading the end of the audit trail: %v", err)
	}
	t.Cleanup(func() {
		// t.Context() is already cancelled while cleanups run.
		unscoped, err := Unscoped(context.Background(), db.Pool(), mark)
		if err != nil {
			t.Errorf("audittest: %v", err)
			return
		}
		for _, entry := range unscoped {
			t.Errorf("audit entry %d (%s) names no project: a project-scoped action must set Entry.ProjectUID",
				entry.ID, entry.Action)
		}
	})
}

// Entry is one offending audit row: its id and action.
type Entry struct {
	ID     int64
	Action string
}

// Unscoped returns the entries after mark whose action is project-scoped but
// that name no project, oldest first.
func Unscoped(ctx context.Context, pool *pgxpool.Pool, mark int64) ([]Entry, error) {
	rows, err := pool.Query(ctx, unscopedSQL, mark)
	if err != nil {
		return nil, fmt.Errorf("reading the unscoped audit entries: %w", err)
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.Action); err != nil {
			return nil, fmt.Errorf("scanning an audit entry: %w", err)
		}
		if audit.ProjectScoped(e.Action) {
			out = append(out, e)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating the audit entries: %w", err)
	}
	return out, nil
}
