package phototask

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DigestLimit is how many tasks one digest mail lists. A person with more than
// this waiting on them gets the newest ones and a count of the rest; the whole
// list is one click away in the app, and a mail that runs to a hundred lines is
// read by nobody.
const DigestLimit = 20

// DigestTask is one task in a person's digest: what a mail line needs and
// nothing more.
type DigestTask struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
	State State  `json:"state"`
	// LastActivityAt is when the task last moved — a state change or a comment
	// by somebody else — and is what the list is ordered by, newest first.
	LastActivityAt time.Time `json:"last_activity_at"`
}

// Digest is what one person would be told today: the open tasks whose move is
// theirs (the same "waiting on me" the listing computes for a reader, here
// computed for everybody at once) and who to send it to.
type Digest struct {
	UserUID     string
	Email       string
	DisplayName string
	// Total is how many tasks wait on the person; Tasks holds the newest
	// DigestLimit of them.
	Total int
	Tasks []DigestTask
	// NewestActivityAt is the last activity across every waiting task, and is
	// what the previous stamp was compared with.
	NewestActivityAt time.Time
	// DigestedAt is when the person's previous digest was scheduled, nil before
	// the first one.
	DigestedAt *time.Time
}

// waitingForEveryoneSQL is the listing's waitingSQL turned inside out: instead
// of asking "which tasks wait on this caller" it lists, for every participant
// of every open task, the tasks whose last activity was not theirs. The
// activity is the same lateral fragment the listing uses (activityJoin), so
// the mail and the app's "Na mně" filter can never disagree about whose move
// it is. Accounts that cannot sign in — disabled, or never approved — are left
// out here rather than mailed about a queue they cannot open.
const waitingForEveryoneSQL = `
WITH waiting AS (
    SELECT p.user_uid, t.uid, t.title, t.state, COALESCE(la.at, t.created_at) AS activity_at
    FROM photo_task_participants p
    JOIN photo_tasks t ON t.uid = p.task_uid AND t.state NOT IN ('done', 'rejected')
    JOIN users u ON u.uid = p.user_uid AND NOT u.disabled AND u.approved_at IS NOT NULL` +
	activityJoin + `
    WHERE la.by IS DISTINCT FROM p.user_uid
)
SELECT u.uid, u.email, COALESCE(NULLIF(u.display_name, ''), u.username, ''), u.task_digest_at,
    count(*), max(w.activity_at),
    (SELECT COALESCE(json_agg(json_build_object(
            'uid', x.uid, 'title', x.title, 'state', x.state, 'last_activity_at', x.activity_at
        ) ORDER BY x.activity_at DESC, x.uid), '[]'::json)
     FROM (SELECT * FROM waiting x WHERE x.user_uid = u.uid
           ORDER BY x.activity_at DESC, x.uid LIMIT $1) x)
FROM waiting w
JOIN users u ON u.uid = w.user_uid
GROUP BY u.uid, u.email, u.display_name, u.username, u.task_digest_at
HAVING u.task_digest_at IS NULL OR max(w.activity_at) > u.task_digest_at
ORDER BY u.uid`

// Digests returns one Digest per person who has something new to hear: an
// active, approved account with at least one open task waiting on them whose
// last activity is newer than the account's previous digest stamp (or with no
// stamp at all). A person whose waiting tasks have not moved since their last
// digest is absent — the digest is sent only when something changed — and so
// is a person nothing waits on.
//
// It is one query over the whole library rather than a listing per user, so a
// daily run never loads every task for every account.
func (s *Store) Digests(ctx context.Context) ([]Digest, error) {
	rows, err := s.pool.Query(ctx, waitingForEveryoneSQL, DigestLimit)
	if err != nil {
		return nil, fmt.Errorf("phototask: listing task digests: %w", err)
	}
	defer rows.Close()

	out := make([]Digest, 0)
	for rows.Next() {
		var (
			d     Digest
			tasks []byte
		)
		if err := rows.Scan(&d.UserUID, &d.Email, &d.DisplayName, &d.DigestedAt,
			&d.Total, &d.NewestActivityAt, &tasks); err != nil {
			return nil, fmt.Errorf("phototask: scanning task digest: %w", err)
		}
		if err := json.Unmarshal(tasks, &d.Tasks); err != nil {
			return nil, fmt.Errorf("phototask: decoding digest tasks: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("phototask: iterating task digests: %w", err)
	}
	return out, nil
}

// markDigestedSQL stamps when a person's digest was scheduled. It does not
// touch updated_at: being mailed is not an edit to the profile.
const markDigestedSQL = `UPDATE users SET task_digest_at = $2 WHERE uid = $1`

// MarkDigested records that a digest was scheduled for the account uid at the
// given time, so the next run sends again only once a waiting task has moved
// past it. An unknown uid is not an error: the account may have been deleted
// between the read and the stamp, and there is nothing to remember for it.
func (s *Store) MarkDigested(ctx context.Context, uid string, at time.Time) error {
	if _, err := s.pool.Exec(ctx, markDigestedSQL, uid, at); err != nil {
		return fmt.Errorf("phototask: stamping task digest of %s: %w", uid, err)
	}
	return nil
}
