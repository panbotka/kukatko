//go:build integration

package review_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/review"
)

// These tests run only under `make test-integration` against
// KUKATKO_TEST_DATABASE_URL. They seed users and audit rows directly (explicit
// created_at, so the time windows are exercised) and read the aggregation back.

// seedUser inserts a user with the given uid, username, display name and role.
func seedUser(t *testing.T, pool *pgxpool.Pool, uid, username, displayName string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (uid, username, display_name, password_hash, role)
		 VALUES ($1, $2, $3, 'x', 'viewer')`, uid, username, displayName)
	if err != nil {
		t.Fatalf("seedUser(%s): %v", uid, err)
	}
}

// seedAudit inserts one audit row. actor is nil for a deleted-user (NULL) row;
// via marks the row as review-originated; ageDays sets created_at that many days
// before now (0 = today).
func seedAudit(t *testing.T, pool *pgxpool.Pool, now time.Time, actor *string, action string, via bool, ageDays int) {
	t.Helper()
	details := map[string]any{}
	if via {
		details["via"] = "review"
	}
	raw, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshal details: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO audit_log (actor_uid, action, target_type, details, created_at)
		 VALUES ($1, $2, '', $3, $4)`,
		actor, action, raw, now.AddDate(0, 0, -ageDays))
	if err != nil {
		t.Fatalf("seedAudit(%s %s): %v", action, actionAge(ageDays), err)
	}
}

// actionAge labels an age in days for error messages.
func actionAge(ageDays int) string {
	switch ageDays {
	case 0:
		return "today"
	default:
		return "older"
	}
}

// wantEntry is an expected leaderboard row for assertions.
type wantEntry struct {
	uid, display        string
	yes, no, totalCount int
}

// assertBoard checks the board matches want in order and per-row counts.
func assertBoard(t *testing.T, window review.LeaderboardWindow, got []review.LeaderboardEntry, want []wantEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d entries, want %d: %+v", window, len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.UserUID != w.uid || g.DisplayName != w.display ||
			g.YesCount != w.yes || g.NoCount != w.no || g.Total != w.totalCount {
			t.Errorf("%s: entry %d = %+v, want uid=%s display=%s yes=%d no=%d total=%d",
				window, i, g, w.uid, w.display, w.yes, w.no, w.totalCount)
		}
	}
}

// TestLeaderboard_aggregatesAcrossActionsWindowsAndExclusions is the core
// leaderboard test: it seeds the four review actions across three users and
// three time buckets, plus a non-review row, a NULL-actor row and a via:review
// row with an uncounted action, then asserts the per-window tallies, the yes/no
// split, the ordering with its tiebreaks, and the exclusions.
func TestLeaderboard_aggregatesAcrossActionsWindowsAndExclusions(t *testing.T) {
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)
	pool := db.Pool()
	now := time.Now()

	seedUser(t, pool, "alice", "alice", "Alice")
	seedUser(t, pool, "bob", "bob", "Bob")
	seedUser(t, pool, "erin", "erin", "Erin")
	seedUser(t, pool, "carol", "carol", "") // blank display name → username fallback

	alice, bob, erin, carol := new("alice"), new("bob"), new("erin"), new("carol")

	// alice: 2 yes today, 2 no older (so all-time > 7d/today).
	seedAudit(t, pool, now, alice, "face.assign", true, 0)
	seedAudit(t, pool, now, alice, "label.attach", true, 0)
	seedAudit(t, pool, now, alice, "face.reject", true, 30)
	seedAudit(t, pool, now, alice, "label.reject", true, 30)

	// bob: 3 yes, 1 no, all today; plus one via:review row with an uncounted
	// action, which must not change any tally.
	seedAudit(t, pool, now, bob, "face.assign", true, 0)
	seedAudit(t, pool, now, bob, "face.assign", true, 0)
	seedAudit(t, pool, now, bob, "label.attach", true, 0)
	seedAudit(t, pool, now, bob, "face.reject", true, 0)
	seedAudit(t, pool, now, bob, "subject.update", true, 0) // via:review but not a decision

	// erin: 2 yes, 2 no, all today (ties bob on total, fewer yes than bob).
	seedAudit(t, pool, now, erin, "face.assign", true, 0)
	seedAudit(t, pool, now, erin, "label.attach", true, 0)
	seedAudit(t, pool, now, erin, "face.reject", true, 0)
	seedAudit(t, pool, now, erin, "label.reject", true, 0)

	// carol: 1 yes today, 1 no three days ago; a non-review yes today that must
	// be excluded.
	seedAudit(t, pool, now, carol, "label.attach", true, 0)
	seedAudit(t, pool, now, carol, "face.reject", true, 3)
	seedAudit(t, pool, now, carol, "face.assign", false, 0) // no via → excluded

	// A deleted user's row (NULL actor) must never appear.
	seedAudit(t, pool, now, nil, "face.assign", true, 0)

	store := review.NewLeaderboardStore(pool)

	allTime, err := store.Leaderboard(context.Background(), review.WindowAllTime)
	if err != nil {
		t.Fatalf("Leaderboard(all): %v", err)
	}
	assertBoard(t, review.WindowAllTime, allTime, []wantEntry{
		{"bob", "Bob", 3, 1, 4},
		{"alice", "Alice", 2, 2, 4},
		{"erin", "Erin", 2, 2, 4},
		{"carol", "carol", 1, 1, 2},
	})

	week, err := store.Leaderboard(context.Background(), review.WindowWeek)
	if err != nil {
		t.Fatalf("Leaderboard(7d): %v", err)
	}
	assertBoard(t, review.WindowWeek, week, []wantEntry{
		{"bob", "Bob", 3, 1, 4},
		{"erin", "Erin", 2, 2, 4},
		{"alice", "Alice", 2, 0, 2}, // alice's 2 rejections are older than 7d
		{"carol", "carol", 1, 1, 2},
	})

	today, err := store.Leaderboard(context.Background(), review.WindowToday)
	if err != nil {
		t.Fatalf("Leaderboard(today): %v", err)
	}
	assertBoard(t, review.WindowToday, today, []wantEntry{
		{"bob", "Bob", 3, 1, 4},
		{"erin", "Erin", 2, 2, 4},
		{"alice", "Alice", 2, 0, 2},
		{"carol", "carol", 1, 0, 1}, // carol's rejection was three days ago
	})
}
