package phototask

import (
	"context"
	"fmt"
	"strings"
)

// Summary is the queue at a glance for one reader: how many tasks stand in
// each state, how many are open at all, and — relative to the caller — how many
// wait on them and how many have been answered.
//
// ByState always carries every state, zero included, so a client can draw its
// chips without checking for a missing key. Open is the sum of the three live
// states. WaitingOnMe is the count behind the navigation badge: the tasks whose
// move is the caller's (see waitingSQL). Answered is the open tasks somebody
// other than the caller has replied to since the state last moved — the same
// predicate as the Answered filter over the default (open) listing, so the chip
// count and the list it opens agree.
type Summary struct {
	ByState     map[State]int `json:"by_state"`
	Open        int           `json:"open"`
	WaitingOnMe int           `json:"waiting_on_me"`
	Answered    int           `json:"answered"`
}

// summarySQL counts the whole table once, one FILTER per number, over the two
// caller-relative fragments the listing itself uses — the thread and the last
// activity — so the badge, the chips and the list can never disagree. The
// per-state columns come first in the order of States, then waiting, then
// answered. It is built rather than written out so a sixth state could not be
// forgotten here.
var summarySQL = buildSummarySQL()

// buildSummarySQL assembles summarySQL from the state list and the two shared
// predicate fragments.
func buildSummarySQL() string {
	cols := make([]string, 0, len(States)+2)
	for _, state := range States {
		cols = append(cols, "count(*) FILTER (WHERE t.state = '"+string(state)+"')")
	}
	cols = append(cols,
		"count(*) FILTER (WHERE "+waitingSQL+")",
		"count(*) FILTER (WHERE t.state NOT IN ('done', 'rejected') AND "+answeredSQL+")",
	)
	return "SELECT " + strings.Join(cols, ",\n  ") + "\nFROM photo_tasks t" + threadJoin + activityJoin
}

// Summary returns the queue's counts as callerUID sees them, in one query. An
// empty caller reads a queue where nothing waits on anybody and every comment
// is somebody else's — the same convention as List and Get.
func (s *Store) Summary(ctx context.Context, callerUID string) (Summary, error) {
	dest := make([]any, 0, len(States)+2)
	perState := make([]int, len(States))
	for i := range perState {
		dest = append(dest, &perState[i])
	}
	var out Summary
	dest = append(dest, &out.WaitingOnMe, &out.Answered)
	if err := s.pool.QueryRow(ctx, summarySQL, callerUID).Scan(dest...); err != nil {
		return Summary{}, fmt.Errorf("phototask: summarising tasks: %w", err)
	}
	out.ByState = make(map[State]int, len(States))
	for i, state := range States {
		out.ByState[state] = perState[i]
		if !state.Closed() {
			out.Open += perState[i]
		}
	}
	return out, nil
}

// WaitingOnMe is the one number of Summary the rest of the application asks
// for on its own — the digest of what's new draws it as "N tasks wait on you".
// It is the same query; a second, narrower one would be a second place for the
// semantic to drift.
func (s *Store) WaitingOnMe(ctx context.Context, callerUID string) (int, error) {
	summary, err := s.Summary(ctx, callerUID)
	if err != nil {
		return 0, err
	}
	return summary.WaitingOnMe, nil
}
