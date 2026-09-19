package phototask

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Paging bounds for a listing. A library holds tens of open tasks, not
// thousands, so the default page already shows everything in practice; the
// maximum exists so one request cannot ask for the whole table.
const (
	// DefaultLimit is the page size used when a caller asks for none.
	DefaultLimit = 50
	// MaxLimit is the largest page a caller may ask for.
	MaxLimit = 200
)

// Store is the database access layer for tasks. It owns no connection; it
// borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// callerParam is the placeholder every read binds the reader's uid to. It is
// always the first parameter: three of the projection's columns depend on who is
// asking (HasNewAnswer, WaitingOnMe, and through them two filters), so the caller
// is part of the query itself rather than a post-processing step — which is what
// keeps a caller-relative filter honest across paging. The cast pins the type
// so the same parameter can be compared against a VARCHAR column in several
// places without pgx having to deduce it twice.
const callerParam = "$1::text"

// answeredSQL is "somebody else has replied since the state last moved": the
// newest live comment by anyone but the caller is later than state_at. It is
// one fragment used as both a column and a filter, so the badge a client draws
// and the list it asks for can never disagree.
const answeredSQL = `(th.last_reply_at IS NOT NULL AND th.last_reply_at > t.state_at)`

// waitingSQL is "the move is the caller's": the task is open, the caller is on
// it, and the last activity was not theirs. An unknown last actor (a deleted
// account) reads as somebody else — a task nobody can be shown to have answered
// is still waiting. Like answeredSQL it serves the column and the filter alike.
const waitingSQL = `(t.state NOT IN ('done', 'rejected')
	AND EXISTS (SELECT 1 FROM photo_task_participants me
		WHERE me.task_uid = t.uid AND me.user_uid = ` + callerParam + `)
	AND la.by IS DISTINCT FROM ` + callerParam + `)`

// taskColumns is the projection every read returns, expecting the task row
// aliased t, its two author accounts left-joined as cu and xu, the thread
// summary as th, the last activity as la with its account lu, and the
// participants as pa. The counts and the two caller-relative flags are resolved
// here rather than by the caller so one query yields everything a listing
// renders — and so the thread's newest reply is available to the filter and the
// ordering, not just to the payload.
const taskColumns = `t.uid, t.title, t.body, t.state, t.resolution, t.source_query,
	COALESCE(t.created_by, ''), COALESCE(NULLIF(cu.display_name, ''), cu.username, ''),
	t.created_at, t.updated_at, t.state_at, COALESCE(t.state_by, ''),
	t.closed_at, COALESCE(t.closed_by, ''), COALESCE(NULLIF(xu.display_name, ''), xu.username, ''),
	(SELECT count(*) FROM photo_task_photos tp WHERE tp.task_uid = t.uid),
	COALESCE((SELECT tp.photo_uid FROM photo_task_photos tp WHERE tp.task_uid = t.uid
		ORDER BY tp.added_at, tp.photo_uid LIMIT 1), ''),
	th.comment_count, th.last_comment_at, ` + answeredSQL + `,
	COALESCE(la.at, t.created_at), COALESCE(la.by, ''),
	COALESCE(NULLIF(lu.display_name, ''), lu.username, ''), ` + waitingSQL + `,
	pa.participants, t.options`

// taskJoins resolves both author accounts, summarises the thread, finds the
// last activity and aggregates the participants. The users are LEFT JOINs
// because created_by, closed_by and state_by are ON DELETE SET NULL: losing an
// account must not lose the task. The thread is a LATERAL so its values can be
// filtered and ordered on, which is the whole reason the count is not fetched
// separately afterwards; last_reply_at is the same maximum restricted to
// comments by somebody other than the caller — one's own words are not a reply.
//
// The last activity is the newest of three dated acts: the task's creation, the
// last state change (an unrecorded mover read as the creator — the rows from
// before migration 0082, and the truth for a task nobody has advanced) and the
// newest live comment. Membership and participant changes are deliberately not
// among them: they are bookkeeping around the conversation, not a move in it.
// The rank breaks a tie at the same instant in favour of the comment, then the
// state change, so a reply posted in the same second as a move still reads as
// the reply.
const taskJoins = `
FROM photo_tasks t
LEFT JOIN users cu ON cu.uid = t.created_by
LEFT JOIN users xu ON xu.uid = t.closed_by
LEFT JOIN LATERAL (
    SELECT count(*) AS comment_count, max(c.created_at) AS last_comment_at,
        max(c.created_at) FILTER (WHERE c.author_uid IS DISTINCT FROM ` + callerParam + `) AS last_reply_at
    FROM comments c
    WHERE c.task_uid = t.uid AND c.deleted_at IS NULL
) th ON TRUE
LEFT JOIN LATERAL (
    SELECT a.at, a.by
    FROM (
        SELECT t.created_at AS at, t.created_by AS by, 2 AS rank
        UNION ALL
        SELECT t.state_at, COALESCE(t.state_by, t.created_by), 1
        UNION ALL
        SELECT c.created_at, c.author_uid, 0
        FROM comments c
        WHERE c.task_uid = t.uid AND c.deleted_at IS NULL
    ) a
    ORDER BY a.at DESC, a.rank
    LIMIT 1
) la ON TRUE
LEFT JOIN users lu ON lu.uid = la.by
LEFT JOIN LATERAL (
    SELECT COALESCE(json_agg(json_build_object(
        'user_uid', p.user_uid,
        'name', COALESCE(NULLIF(pu.display_name, ''), pu.username, ''),
        'joined_at', p.joined_at,
        'added_by', COALESCE(p.added_by, ''),
        'added_by_name', COALESCE(NULLIF(au.display_name, ''), au.username, '')
    ) ORDER BY p.joined_at, p.user_uid), '[]'::json) AS participants
    FROM photo_task_participants p
    JOIN users pu ON pu.uid = p.user_uid
    LEFT JOIN users au ON au.uid = p.added_by
    WHERE p.task_uid = t.uid
) pa ON TRUE`

// taskOrder puts the open tasks first and the most recently touched of them at
// the top, where "touched" counts a reply as well as an edit — a task somebody
// answered an hour ago matters more than one edited last week. The UID breaks
// ties so paging is stable.
const taskOrder = `
ORDER BY (t.state IN ('done', 'rejected')), GREATEST(t.updated_at, th.last_comment_at) DESC, t.uid`

// Filter narrows a listing. The zero value lists every task, newest activity
// first.
type Filter struct {
	// States restricts the listing to the given states.
	States []State
	// Open is the shorthand for "the three states a live task can be in". It
	// applies only when States is empty, so an explicit choice always wins.
	Open bool
	// CallerUID is who is reading. HasNewAnswer and WaitingOnMe are relative to
	// it, and so are the Answered and Waiting filters; empty means "nobody in
	// particular", for which every comment is somebody else's and no task waits.
	CallerUID string
	// Answered restricts the listing to tasks somebody other than the caller has
	// replied to since the state last moved — the work that has been answered
	// and is waiting to be written into the library. It is the one an agent polls.
	Answered bool
	// Waiting restricts the listing to tasks whose move is the caller's: open,
	// the caller on them, and the last activity somebody else's. It is evaluated
	// in SQL like every other filter, because a caller-relative predicate applied
	// after paging would silently return short pages.
	Waiting bool
	// Search matches a substring of the question or its context, case-insensitively.
	Search string
	// PhotoUID restricts the listing to tasks that photograph is part of.
	PhotoUID string
	// ParticipantUID restricts the listing to tasks that person is on — the
	// "what am I involved in?" view. It combines with every other filter, so
	// "mine, still open" is one query.
	ParticipantUID string
	// Limit and Offset page the result; Limit is clamped to MaxLimit and defaults
	// to DefaultLimit.
	Limit  int
	Offset int
}

// effectiveStates returns the states the filter selects: the explicit list, or
// the open ones when Open was asked for, or none (meaning every state).
func (f Filter) effectiveStates() []State {
	if len(f.States) > 0 {
		return f.States
	}
	if f.Open {
		return OpenStates
	}
	return nil
}

// Page returns the limit and offset the listing will actually use, clamped to
// the bounds above. It is exported because the HTTP layer echoes them back to
// the client, which must be told what it got rather than what it asked for.
func (f Filter) Page() (limit, offset int) {
	limit = f.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	offset = max(f.Offset, 0)
	return limit, offset
}

// conditions compiles the filter into WHERE clauses and their arguments. The
// clauses are ANDed by the caller; an empty filter yields none. The arguments
// start with the caller, which the projection binds as callerParam whether or
// not any clause needs it, so the clauses' own placeholders start at $2.
func (f Filter) conditions() ([]string, []any) {
	var where []string
	args := []any{f.CallerUID}
	bind := func(value any) string {
		args = append(args, value)
		return "$" + strconv.Itoa(len(args))
	}
	if states := f.effectiveStates(); len(states) > 0 {
		where = append(where, "t.state = ANY("+bind(stateStrings(states))+")")
	}
	if f.Answered {
		where = append(where, answeredSQL)
	}
	if f.Waiting {
		where = append(where, waitingSQL)
	}
	if f.Search != "" {
		// Case- *and* accent-insensitive, exactly like every other text search in
		// the library (albums, labels, people): somebody typing "dum" is looking
		// for "dům", and a queue you have to spell with the right diacritics to
		// search is a queue nobody searches.
		pattern := "immutable_unaccent(" + bind("%"+likeEscape(f.Search)+"%") + ")"
		where = append(where, "(immutable_unaccent(t.title) ILIKE "+pattern+
			" OR immutable_unaccent(t.body) ILIKE "+pattern+")")
	}
	if f.PhotoUID != "" {
		where = append(where, "EXISTS (SELECT 1 FROM photo_task_photos tp"+
			" WHERE tp.task_uid = t.uid AND tp.photo_uid = "+bind(f.PhotoUID)+")")
	}
	if f.ParticipantUID != "" {
		// EXISTS rather than a filter over the aggregated participants above:
		// the aggregate is for rendering, and making the listing depend on it
		// would turn a lookup on the (user_uid, joined_at) index into a scan.
		where = append(where, "EXISTS (SELECT 1 FROM photo_task_participants pp"+
			" WHERE pp.task_uid = t.uid AND pp.user_uid = "+bind(f.ParticipantUID)+")")
	}
	return where, args
}

// stateStrings converts states to the []string an ANY() parameter needs.
func stateStrings(states []State) []string {
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, string(s))
	}
	return out
}

// likeEscape neutralises the LIKE wildcards in a user's search term, so a
// question mentioning "50 %" searches for that text rather than matching
// everything.
func likeEscape(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// whereSQL joins clauses into a WHERE fragment, or an empty string when there
// are none.
func whereSQL(clauses []string) string {
	if len(clauses) == 0 {
		return ""
	}
	return "\nWHERE " + strings.Join(clauses, "\n  AND ")
}

// Get returns the task with the given UID, or ErrNotFound. callerUID is who is
// reading: HasNewAnswer and WaitingOnMe are computed for them, and an empty
// caller reads a task nobody is waiting on whose every comment is a reply.
func (s *Store) Get(ctx context.Context, uid, callerUID string) (Task, error) {
	query := "SELECT " + taskColumns + taskJoins + "\nWHERE t.uid = $2"
	t, err := scanTask(s.pool.QueryRow(ctx, query, callerUID, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("phototask: reading task %s: %w", uid, err)
	}
	return t, nil
}

// List returns one page of tasks matching f, together with the total number of
// matches before paging — so a client can say "1-25 of 63" without a second
// request. Open tasks come first, most recently touched at the top.
func (s *Store) List(ctx context.Context, f Filter) ([]Task, int, error) {
	clauses, args := f.conditions()
	where := whereSQL(clauses)

	var total int
	countQuery := "SELECT count(*)" + taskJoins + where
	if err := s.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("phototask: counting tasks: %w", err)
	}

	limit, offset := f.Page()
	listQuery := "SELECT " + taskColumns + taskJoins + where + taskOrder +
		fmt.Sprintf("\nLIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	rows, err := s.pool.Query(ctx, listQuery, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("phototask: listing tasks: %w", err)
	}
	defer rows.Close()

	out := make([]Task, 0)
	for rows.Next() {
		t, scanErr := scanTask(rows)
		if scanErr != nil {
			return nil, 0, fmt.Errorf("phototask: reading tasks: %w", scanErr)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("phototask: iterating tasks: %w", err)
	}
	return out, total, nil
}

// Ref is the compact form of a task, for annotating something that is part of
// one: enough to draw a chip and link to it, and nothing more.
type Ref struct {
	UID   string `json:"uid"`
	Title string `json:"title"`
	State State  `json:"state"`
}

// openForPhotosSQL lists the open tasks each of the given photographs is part
// of. Closed tasks are left out: a person looking at a picture wants the
// question still waiting on somebody, not the archive of settled ones.
const openForPhotosSQL = `
SELECT tp.photo_uid, t.uid, t.title, t.state
FROM photo_task_photos tp
JOIN photo_tasks t ON t.uid = tp.task_uid
WHERE tp.photo_uid = ANY($1) AND t.state NOT IN ('done', 'rejected')
ORDER BY tp.photo_uid, t.updated_at DESC, t.uid`

// OpenForPhotos returns the open tasks each of photoUIDs belongs to, keyed by
// photo UID. Photos in no open task are absent from the map, and an empty input
// yields an empty map without querying.
//
// It is the bulk shape for the same reason the comment count is: the photo
// detail asks about one picture, but a listing would ask about a page of them,
// and a per-photo query would be an N+1 waiting to be written.
func (s *Store) OpenForPhotos(ctx context.Context, photoUIDs []string) (map[string][]Ref, error) {
	out := make(map[string][]Ref, len(photoUIDs))
	if len(photoUIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, openForPhotosSQL, photoUIDs)
	if err != nil {
		return nil, fmt.Errorf("phototask: listing open tasks of photos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			photoUID string
			ref      Ref
		)
		if err := rows.Scan(&photoUID, &ref.UID, &ref.Title, &ref.State); err != nil {
			return nil, fmt.Errorf("phototask: scanning open task of photo: %w", err)
		}
		out[photoUID] = append(out[photoUID], ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("phototask: iterating open tasks of photos: %w", err)
	}
	return out, nil
}

// rowScanner is the subset of pgx.Row/pgx.Rows scanTask needs, so one scanner
// serves both the single-row reads and the list iteration.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTask reads one task row in taskColumns order. The two caller-relative
// flags arrive already computed by the query — the same fragments the filters
// use — so no client, and no second code path, has to derive them. Its error
// carries no package prefix: every caller adds the operation that failed, and
// the cause stays wrapped so pgx.ErrNoRows remains classifiable.
func scanTask(row rowScanner) (Task, error) {
	var (
		t            Task
		participants []byte
	)
	err := row.Scan(&t.UID, &t.Title, &t.Body, &t.State, &t.Resolution, &t.Query,
		&t.CreatedByUID, &t.CreatedByName, &t.CreatedAt, &t.UpdatedAt, &t.StateAt, &t.StateByUID,
		&t.ClosedAt, &t.ClosedByUID, &t.ClosedByName,
		&t.PhotoCount, &t.CoverPhotoUID, &t.CommentCount, &t.LastCommentAt, &t.HasNewAnswer,
		&t.LastActivityAt, &t.LastActivityByUID, &t.LastActivityByName, &t.WaitingOnMe,
		&participants, &t.Options)
	if err != nil {
		return Task{}, fmt.Errorf("scanning task row: %w", err)
	}
	// The column is NOT NULL, so this only guards a scanner that left the
	// destination alone: a client is promised an array, never null.
	if t.Options == nil {
		t.Options = []string{}
	}
	// The aggregate is COALESCE'd to an empty array by the query, so a task with
	// nobody on it decodes to an empty slice. An absent value is treated the same
	// way rather than failing: participation is an annotation on a task, and it
	// must never be able to make the task itself unreadable.
	t.Participants = make([]Participant, 0)
	if len(participants) > 0 {
		if err := json.Unmarshal(participants, &t.Participants); err != nil {
			return Task{}, fmt.Errorf("decoding task participants: %w", err)
		}
	}
	return t, nil
}
