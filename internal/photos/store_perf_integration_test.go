//go:build integration

package photos_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/photos"
)

// explainPlan returns the textual EXPLAIN output for query, run with sequential
// and bitmap scans disabled inside a rolled-back transaction. Forcing an ordered
// index scan reveals whether an index can serve the ORDER BY: a bitmap scan does
// not preserve order (so on a tiny table the planner would otherwise bitmap-scan
// and Sort), and if the chosen index does not match the ordering exactly a Sort
// node still appears in the plan.
func explainPlan(t *testing.T, pool *pgxpool.Pool, query string) string {
	t.Helper()
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, stmt := range []string{
		"SET LOCAL enable_seqscan = off",
		"SET LOCAL enable_bitmapscan = off",
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
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

// seedTimeline inserts n live photos with descending capture times, a handful of
// archived photos, and a couple with no capture time, so the planner has a
// representative live timeline to plan against.
func seedTimeline(t *testing.T, store *photos.Store, n int) {
	t.Helper()
	base := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	for i := range n {
		taken := base.Add(time.Duration(i) * time.Hour)
		mustCreate(t, store, photos.Photo{
			FileHash: fmt.Sprintf("perf-live-%04d", i), FilePath: fmt.Sprintf("p/%d.jpg", i),
			FileName: fmt.Sprintf("%d.jpg", i), FileMime: "image/jpeg",
			TakenAt: &taken, TakenAtSource: "exif",
		})
	}
	// A couple of photos with unknown capture time exercise the NULLS LAST tail.
	for i := range 2 {
		mustCreate(t, store, photos.Photo{
			FileHash: fmt.Sprintf("perf-null-%d", i), FilePath: fmt.Sprintf("n/%d.jpg", i),
			FileName: fmt.Sprintf("n%d.jpg", i), FileMime: "image/jpeg",
		})
	}
	// Archived photos must stay out of the partial index and the live timeline.
	for i := range 5 {
		p := mustCreate(t, store, photos.Photo{
			FileHash: fmt.Sprintf("perf-arch-%d", i), FilePath: fmt.Sprintf("a/%d.jpg", i),
			FileName: fmt.Sprintf("a%d.jpg", i), FileMime: "image/jpeg",
		})
		if _, err := store.Archive(t.Context(), p.UID); err != nil {
			t.Fatalf("Archive: %v", err)
		}
	}
}

// TestListQueryPlan_usesLiveIndexes verifies the hot grid orderings (the default
// taken_at timeline and the "recently added" created_at ordering, both scoped to
// live photos) are served by the partial composite indexes from migration 0015
// without a Sort node — the optimisation the perf pass added. The queries mirror
// buildListQuery's WHERE/ORDER BY for those two sorts.
func TestListQueryPlan_usesLiveIndexes(t *testing.T) {
	store, db := newStore(t)
	seedTimeline(t, store, 80)
	pool := db.Pool()

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
