package review

// Day streaks: the one number on the leaderboard that is not a total.
//
// A total rewards the evening somebody spent clicking through eight hundred
// candidates and says nothing at all the next day. A streak rewards coming back,
// which is the behaviour the game actually needs — the library is cleaned by
// twenty answers a day for a month, not by one heroic session.
//
// It needs no new table. Every decisive answer already writes an audit row
// tagged via = "review" (see answer.go), so "days on which this user answered at
// least one question" is a projection of data that is already durable, already
// indexed for exactly this filter (migration 0037) and already the source of the
// leaderboard's counts. A separate streak table would be a second truth that can
// disagree with the first.
//
// The database reduces those rows to distinct *hours* per user and Go does the
// rest. That split is deliberate. The reduction is what keeps the query cheap —
// a user who plays every day for a year yields at most a few thousand rows
// instead of every answer they ever gave — while the day arithmetic stays in Go,
// where the streak is a pure function of a list of timestamps and a clock, which
// is the only way to test the boundary cases that matter: midnight, a run that
// ended yesterday (still alive), a run that ended the day before (broken).
//
// One caveat comes with the hour reduction: the local day of an hour-truncated
// timestamp is exact only where the zone's offset from UTC is a whole number of
// hours. That is every European zone, and the error in the few places it is not
// (Nepal, parts of Australia) is one answer landing on the neighbouring day.
// Paying for exactness there would mean transferring every answer ever recorded.

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
)

// streakLookback is how far back the streak query reads. A streak longer than a
// year is not a number anybody needs to be exact about, and the bound is what
// keeps the query's cost flat as the audit log grows.
const streakLookback = 400 * 24 * time.Hour

// streakQuery reduces the review-tagged audit rows to the distinct hours each
// user was active in. The 'review' literal, the action set and the actor filter
// match leaderboardQuery exactly, so the two numbers on one row can never
// disagree about what counts as a decision. $1 is the counted action set, $2 the
// lookback bound.
const streakQuery = `
SELECT DISTINCT a.actor_uid, date_trunc('hour', a.created_at) AS active_hour
FROM audit_log a
WHERE a.actor_uid IS NOT NULL
  AND a.details ->> 'via' = 'review'
  AND a.action = ANY($1)
  AND a.created_at >= $2
ORDER BY a.actor_uid, active_hour DESC`

// streaks returns each user's current day streak: how many consecutive days up
// to and including today (or ending yesterday, since a day still in progress
// must not break a run) they recorded at least one review decision on. Users
// with no live streak are absent from the map rather than present with a zero.
func (s *LeaderboardStore) streaks(ctx context.Context) (map[string]int, error) {
	now := s.now()
	rows, err := s.pool.Query(ctx, streakQuery, audit.ReviewDecisionActions(), now.Add(-streakLookback))
	if err != nil {
		return nil, fmt.Errorf("review: querying review streaks: %w", err)
	}
	defer rows.Close()

	active := make(map[string][]time.Time)
	for rows.Next() {
		var actorUID string
		var hour time.Time
		if err := rows.Scan(&actorUID, &hour); err != nil {
			return nil, fmt.Errorf("review: scanning streak row: %w", err)
		}
		active[actorUID] = append(active[actorUID], hour)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("review: iterating streak rows: %w", err)
	}

	streaks := make(map[string]int, len(active))
	for actorUID, hours := range active {
		if days := currentStreak(hours, now); days > 0 {
			streaks[actorUID] = days
		}
	}
	return streaks, nil
}

// currentStreak counts the consecutive days ending today (or yesterday) on which
// at least one of the given instants falls, all measured in now's location.
//
// Yesterday counts as the end of a live run because a day is still in progress:
// at nine in the morning a player who answered every day for a week has not
// broken anything, and telling them their streak is zero until they play again
// would be exactly the wrong feedback. A run that ended the day before yesterday
// is over, and this returns zero.
func currentStreak(active []time.Time, now time.Time) int {
	days := distinctDays(active, now.Location())
	if len(days) == 0 {
		return 0
	}
	today := startOfDay(now)
	// The most recent active day has to be today or yesterday, or the run that
	// ended there is already history.
	expected := days[0]
	if gap := int(today.Sub(expected).Hours() / 24); gap > 1 {
		return 0
	}
	streak := 0
	for _, day := range days {
		if !day.Equal(expected) {
			break
		}
		streak++
		expected = expected.AddDate(0, 0, -1)
	}
	return streak
}

// distinctDays folds instants into the distinct calendar days they fall on in
// loc, most recent first. Sorting here rather than trusting the query's order is
// what makes currentStreak a function of its arguments alone.
func distinctDays(active []time.Time, loc *time.Location) []time.Time {
	seen := make(map[int64]struct{}, len(active))
	days := make([]time.Time, 0, len(active))
	for _, instant := range active {
		day := startOfDay(instant.In(loc))
		if _, ok := seen[day.Unix()]; ok {
			continue
		}
		seen[day.Unix()] = struct{}{}
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].After(days[j]) })
	return days
}

// startOfDay truncates an instant to midnight of its own calendar day in its own
// location. time.Truncate is deliberately not used: it truncates against the
// Unix epoch in UTC, which is midnight somewhere else.
func startOfDay(instant time.Time) time.Time {
	year, month, day := instant.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, instant.Location())
}
