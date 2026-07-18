package review

// The leaderboard turns the review game into a friendly competition: it counts,
// per user, the decisive answers they have recorded — the audit rows tagged
// details.via = "review" (see answer.go). "Yes" decisions are face.assign and
// label.attach, "no" decisions are face.reject and label.reject; skips write no
// row and so never count. Deleted users (a NULL actor_uid) are excluded, and a
// window narrows the tally to all-time, the last seven days, or today.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// LeaderboardWindow selects the time span a leaderboard aggregates over.
type LeaderboardWindow string

// The supported leaderboard windows. The zero value is not valid; callers parse
// a request parameter through ParseWindow, which defaults empty to all-time.
const (
	// WindowAllTime counts every review decision ever recorded.
	WindowAllTime LeaderboardWindow = "all"
	// WindowWeek counts decisions from the last seven days (rolling 7×24h).
	WindowWeek LeaderboardWindow = "7d"
	// WindowToday counts decisions since midnight of the current day.
	WindowToday LeaderboardWindow = "today"
)

// ErrInvalidWindow indicates a window parameter outside the supported set.
var ErrInvalidWindow = errors.New("review: invalid leaderboard window")

// ParseWindow maps a request's window query parameter to a LeaderboardWindow.
// An empty value defaults to all-time; any other unrecognised value returns
// ErrInvalidWindow so the HTTP layer can answer 400.
func ParseWindow(raw string) (LeaderboardWindow, error) {
	switch LeaderboardWindow(raw) {
	case "", WindowAllTime:
		return WindowAllTime, nil
	case WindowWeek:
		return WindowWeek, nil
	case WindowToday:
		return WindowToday, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidWindow, raw)
	}
}

// windowCutoff returns the inclusive lower bound on created_at for window,
// computed from now, or nil for all-time (no bound). WindowWeek is a rolling
// seven days; WindowToday is midnight of now's calendar day in now's location.
func windowCutoff(window LeaderboardWindow, now time.Time) *time.Time {
	switch window {
	case WindowWeek:
		cutoff := now.AddDate(0, 0, -7)
		return &cutoff
	case WindowToday:
		cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return &cutoff
	default:
		return nil
	}
}

// LeaderboardEntry is one user's review-decision tally for a window. Total is
// always YesCount + NoCount (the four counted actions partition into the two
// buckets), so the frontend can rank on it directly.
type LeaderboardEntry struct {
	// UserUID identifies the acting user, so the caller's own row is findable.
	UserUID string `json:"user_uid"`
	// DisplayName is the user's display name, falling back to their username
	// when the display name is blank.
	DisplayName string `json:"display_name"`
	// YesCount is the number of confirmations (face.assign + label.attach).
	YesCount int `json:"yes_count"`
	// NoCount is the number of rejections (face.reject + label.reject).
	NoCount int `json:"no_count"`
	// Total is YesCount + NoCount, the value the board ranks on.
	Total int `json:"total"`
}

// LeaderboardStore aggregates review decisions straight from the audit log. It
// is read-only and safe for concurrent use.
type LeaderboardStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// NewLeaderboardStore returns a LeaderboardStore backed by pool, using the wall
// clock to resolve window cutoffs.
func NewLeaderboardStore(pool *pgxpool.Pool) *LeaderboardStore {
	return &LeaderboardStore{pool: pool, now: time.Now}
}

// leaderboardQuery aggregates the via:review audit rows per acting user. The
// 'review' literal must stay in step with viaReview (answer.go) and the partial
// index in migration 0037 so the planner can use it. $1/$2 are the yes/no action
// sets, $3 the union used to bound the scan; an optional created_at bound is
// appended as $4 for a windowed board.
const leaderboardQuery = `
SELECT
    a.actor_uid,
    COALESCE(NULLIF(u.display_name, ''), u.username) AS display_name,
    COUNT(*) FILTER (WHERE a.action = ANY($1)) AS yes_count,
    COUNT(*) FILTER (WHERE a.action = ANY($2)) AS no_count,
    COUNT(*) AS total
FROM audit_log a
JOIN users u ON u.uid = a.actor_uid
WHERE a.actor_uid IS NOT NULL
  AND a.details ->> 'via' = 'review'
  AND a.action = ANY($3)`

// leaderboardGroupOrder groups per user and orders the board deterministically:
// highest total first, then more confirmations, then display name.
const leaderboardGroupOrder = `
GROUP BY a.actor_uid, u.display_name, u.username
ORDER BY total DESC, yes_count DESC, display_name ASC`

// Leaderboard returns the per-user review-decision tally for window, ordered
// highest total first. Only users with at least one decision in the window
// appear. It returns a wrapped error on any query or scan failure.
func (s *LeaderboardStore) Leaderboard(ctx context.Context, window LeaderboardWindow) ([]LeaderboardEntry, error) {
	yes := []string{audit.ActionFaceAssign, audit.ActionLabelAttach}
	no := []string{audit.ActionFaceReject, audit.ActionLabelReject}
	all := append(append([]string{}, yes...), no...)
	args := []any{yes, no, all}

	query := leaderboardQuery
	if cutoff := windowCutoff(window, s.now()); cutoff != nil {
		args = append(args, *cutoff)
		query += fmt.Sprintf("\n  AND a.created_at >= $%d", len(args))
	}
	query += leaderboardGroupOrder

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("review: querying leaderboard: %w", err)
	}
	defer rows.Close()

	var entries []LeaderboardEntry
	for rows.Next() {
		var e LeaderboardEntry
		if err := rows.Scan(&e.UserUID, &e.DisplayName, &e.YesCount, &e.NoCount, &e.Total); err != nil {
			return nil, fmt.Errorf("review: scanning leaderboard row: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review: iterating leaderboard rows: %w", err)
	}
	return entries, nil
}
