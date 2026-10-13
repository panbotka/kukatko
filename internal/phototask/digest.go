package phototask

import (
	"context"
	"encoding/json"
	"errors"
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

// Digest is what one person would be told today about one project: the open
// tasks of that project whose move is theirs (the same "waiting on me" the
// listing computes for a reader, here computed for everybody at once) and who
// to send it to. A person in two projects has two digests, each with its own
// watermark (project_members.task_digest_at).
type Digest struct {
	// ProjectUID, ProjectSlug and ProjectName name the project the tasks are
	// in: the mail's heading and the base of its links.
	ProjectUID  string
	ProjectSlug string
	ProjectName string
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
	// DigestedAt is when the person's previous digest of this project was
	// scheduled, nil before the first one.
	DigestedAt *time.Time
}

// DigestTarget names one (project, person) pair that has a digest due.
type DigestTarget struct {
	ProjectUID string
	UserUID    string
}

// waitingForEveryoneSQL is the listing's waitingSQL turned inside out: instead
// of asking "which tasks wait on this caller" it lists, for every participant
// of every open task, the tasks whose last activity was not theirs. The
// activity is the same lateral fragment the listing uses (activityJoin), so
// the mail and the app's "Na mně" filter can never disagree about whose move
// it is.
//
// It groups per (project, person): the participant's membership of the task's
// project carries the watermark (task_digest_at), so each project's digest is
// sent and stamped on its own. Accounts that cannot sign in — disabled — and
// memberships nobody approved are left out rather than mailed about a queue
// they cannot open, and so is a participant who is no longer a member, or a
// deactivated project. $2/$3 narrow the run to one project and one person
// ("" = all), which is how the job for one pair reads its own digest.
const waitingForEveryoneSQL = `
WITH waiting AS (
    SELECT t.project_id, p.user_uid, t.uid, t.title, t.state, COALESCE(la.at, t.created_at) AS activity_at
    FROM photo_task_participants p
    JOIN photo_tasks t ON t.uid = p.task_uid AND t.state NOT IN ('done', 'rejected')
    JOIN users u ON u.uid = p.user_uid AND NOT u.disabled
    JOIN project_members pm ON pm.project_uid = t.project_id AND pm.user_uid = p.user_uid
        AND pm.approved_at IS NOT NULL
    JOIN projects pr ON pr.uid = t.project_id AND pr.active` +
	activityJoin + `
    WHERE la.by IS DISTINCT FROM p.user_uid
      AND ($2::text = '' OR t.project_id = $2::text)
      AND ($3::text = '' OR p.user_uid = $3::text)
)
SELECT pr.uid, pr.slug, pr.name,
    u.uid, u.email, COALESCE(NULLIF(u.display_name, ''), u.username, ''), pm.task_digest_at,
    count(*), max(w.activity_at),
    (SELECT COALESCE(json_agg(json_build_object(
            'uid', x.uid, 'title', x.title, 'state', x.state, 'last_activity_at', x.activity_at
        ) ORDER BY x.activity_at DESC, x.uid), '[]'::json)
     FROM (SELECT * FROM waiting x WHERE x.user_uid = u.uid AND x.project_id = pr.uid
           ORDER BY x.activity_at DESC, x.uid LIMIT $1) x)
FROM waiting w
JOIN users u ON u.uid = w.user_uid
JOIN projects pr ON pr.uid = w.project_id
JOIN project_members pm ON pm.project_uid = w.project_id AND pm.user_uid = w.user_uid
GROUP BY pr.uid, pr.slug, pr.name, u.uid, u.email, u.display_name, u.username, pm.task_digest_at
HAVING pm.task_digest_at IS NULL OR max(w.activity_at) > pm.task_digest_at
ORDER BY pr.uid, u.uid`

// Digests returns one Digest per (project, person) that has something new to
// hear: an active, approved member with at least one open task of the project
// waiting on them whose last activity is newer than that membership's previous
// digest stamp (or with no stamp at all). A pair whose waiting tasks have not
// moved since its last digest is absent — the digest is sent only when
// something changed — and so is a pair nothing waits on.
//
// It is one query over every project rather than a listing per user, so a
// daily run never loads every task for every account.
func (s *Store) Digests(ctx context.Context) ([]Digest, error) {
	return s.digests(ctx, "", "")
}

// DigestTargets returns the (project, person) pairs Digests would write to —
// what the daily scheduler enqueues one job for each of.
func (s *Store) DigestTargets(ctx context.Context) ([]DigestTarget, error) {
	digests, err := s.digests(ctx, "", "")
	if err != nil {
		return nil, err
	}
	out := make([]DigestTarget, len(digests))
	for i, d := range digests {
		out[i] = DigestTarget{ProjectUID: d.ProjectUID, UserUID: d.UserUID}
	}
	return out, nil
}

// DigestFor returns userUID's digest of the project projectUID, or nil when
// nothing new waits on them there (see Digests). Both uids are required: an
// empty one is ErrNoProject or an error, never "everybody".
func (s *Store) DigestFor(ctx context.Context, projectUID, userUID string) (*Digest, error) {
	if projectUID == "" {
		return nil, ErrNoProject
	}
	if userUID == "" {
		return nil, errors.New("phototask: a digest needs a user")
	}
	digests, err := s.digests(ctx, projectUID, userUID)
	if err != nil {
		return nil, err
	}
	if len(digests) == 0 {
		return nil, nil //nolint:nilnil // "nothing to send" is a normal answer, not an error.
	}
	return &digests[0], nil
}

// digests runs waitingForEveryoneSQL narrowed to projectUID and userUID ("" =
// all).
func (s *Store) digests(ctx context.Context, projectUID, userUID string) ([]Digest, error) {
	rows, err := s.pool.Query(ctx, waitingForEveryoneSQL, DigestLimit, projectUID, userUID)
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
		if err := rows.Scan(&d.ProjectUID, &d.ProjectSlug, &d.ProjectName,
			&d.UserUID, &d.Email, &d.DisplayName, &d.DigestedAt,
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

// markDigestedSQL stamps when a person's digest of one project was scheduled,
// on their membership — the only place the watermark lives.
const markDigestedSQL = `UPDATE project_members SET task_digest_at = $3 WHERE project_uid = $1 AND user_uid = $2`

// MarkDigested records that a digest of the project projectUID was scheduled
// for the account uid at the given time, so the next run sends again only once
// a waiting task of that project has moved past it. An unknown membership is
// not an error: the account may have left the project between the read and the
// stamp, and there is nothing to remember for it.
func (s *Store) MarkDigested(ctx context.Context, projectUID, uid string, at time.Time) error {
	if projectUID == "" {
		return ErrNoProject
	}
	if _, err := s.pool.Exec(ctx, markDigestedSQL, projectUID, uid, at); err != nil {
		return fmt.Errorf("phototask: stamping task digest of %s in %s: %w", uid, projectUID, err)
	}
	return nil
}
