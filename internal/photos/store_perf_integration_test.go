//go:build integration

package photos_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// explainPlan returns the textual EXPLAIN output for query, run inside a
// rolled-back transaction. It deliberately does NOT disable any plan types: the
// point of the test is that the planner picks the ordered index scan on its own
// merits, so the assertion would be vacuous if the alternatives were switched
// off (with enable_sort = off, for instance, any index that matches the ordering
// wins by default and the plan proves nothing).
func explainPlan(t *testing.T, pool *pgxpool.Pool, query string) string {
	t.Helper()
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, "EXPLAIN "+query)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating plan: %v", err)
	}
	return b.String()
}

// liveTimelineRows is the number of photos seeded for the query-plan test.
//
// The size is load-bearing, not arbitrary. The indexes from migration 0015 pay
// off by letting a grid page stop after LIMIT rows instead of sorting the whole
// live timeline, so the seed must be well above the 100-row page size for that
// early exit to be worth anything. With the ~87 rows this test used to seed, the
// LIMIT 100 never truncated the scan, the index's entire benefit vanished, and
// the planner picked whatever was cheapest by a rounding error — which is how
// this test came to assert a plan it did not actually control. Do not shrink it.
const liveTimelineRows = 5000

// seedTimeline bulk-inserts a realistic live timeline: liveTimelineRows photos
// with descending capture and creation times, a scattering of photos with no
// capture time (the NULLS LAST tail), and a minority of archived photos (which
// the partial indexes must exclude). It inserts in a single statement because
// pushing thousands of rows through Store.Create one by one would dominate the
// package's runtime.
//
// The trailing ANALYZE is as load-bearing as the row count. The integration
// tests share one database and truncate between cases, which resets the row
// counts in pg_class but leaves pg_statistic populated with whatever the
// previous test happened to leave behind. Without ANALYZE the planner therefore
// costs this query from another test's statistics — that is what made this test
// pass locally and fail in CI on identical code. ANALYZE pins the plan to the
// data we actually seeded.
func seedTimeline(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	ctx := t.Context()
	const insert = `
		INSERT INTO photos (
			uid, file_hash, file_path, file_name, file_mime,
			taken_at, taken_at_source, created_at, archived_at
		)
		SELECT
			'perf' || lpad(i::text, 28, '0'),
			'perf-hash-' || lpad(i::text, 54, '0'),
			'p/' || i || '.jpg',
			i || '.jpg',
			'image/jpeg',
			-- Every 33rd photo has an unknown capture time: the NULLS LAST tail.
			CASE WHEN i % 33 = 0 THEN NULL
			     ELSE timestamptz '2024-01-01 12:00:00Z' - (i || ' hours')::interval END,
			CASE WHEN i % 33 = 0 THEN '' ELSE 'exif' END,
			timestamptz '2024-01-01 12:00:00Z' - (i || ' minutes')::interval,
			-- A minority of archived photos keeps the partial predicate honest:
			-- they must stay out of both indexes and out of the live grid.
			CASE WHEN i % 25 = 0 THEN timestamptz '2024-06-01 12:00:00Z' ELSE NULL END
		FROM generate_series(1, $1) i`
	if _, err := pool.Exec(ctx, insert, n); err != nil {
		t.Fatalf("bulk insert %d photos: %v", n, err)
	}
	if _, err := pool.Exec(ctx, "ANALYZE photos"); err != nil {
		t.Fatalf("ANALYZE photos: %v", err)
	}
}

// TestListQueryPlan_usesLiveIndexes verifies the hot grid orderings (the default
// taken_at timeline and the "recently added" created_at ordering, both scoped to
// live photos) are served by the partial composite indexes from migration 0015
// without a Sort node — the optimisation the perf pass added. The queries mirror
// buildListQuery's WHERE/ORDER BY for those two sorts.
func TestListQueryPlan_usesLiveIndexes(t *testing.T) {
	_, db := newStore(t)
	pool := db.Pool()
	seedTimeline(t, pool, liveTimelineRows)

	tests := []struct {
		name      string
		query     string
		wantIndex string
	}{
		{
			name: "default taken_at timeline",
			query: "SELECT uid FROM photos WHERE archived_at IS NULL " +
				"ORDER BY taken_at DESC NULLS LAST, uid DESC LIMIT 100 OFFSET 0",
			wantIndex: "idx_photos_live_taken_at",
		},
		{
			name: "recently added created_at",
			query: "SELECT uid FROM photos WHERE archived_at IS NULL " +
				"ORDER BY created_at DESC NULLS LAST, uid DESC LIMIT 100 OFFSET 0",
			wantIndex: "idx_photos_live_created_at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := explainPlan(t, pool, tt.query)
			if !strings.Contains(plan, tt.wantIndex) {
				t.Errorf("plan does not use %s:\n%s", tt.wantIndex, plan)
			}
			if strings.Contains(plan, "Sort") {
				t.Errorf("plan contains a Sort node (index does not cover the ordering):\n%s", plan)
			}
		})
	}
}
