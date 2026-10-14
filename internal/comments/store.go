package comments

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/audit"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation.
const foreignKeyViolation = "23503"

// Store is the database access layer for comment threads. It owns no connection;
// it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
	// mentions hears about fresh mentions inside the mutation's transaction;
	// nil hears nothing. See WithMentionObserver.
	mentions MentionObserver
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// WithMentionObserver returns a copy of s that reports every fresh mention to
// observer, within the transaction of the comment that carries it. The copy
// shares the pool; s itself is unchanged.
func (s *Store) WithMentionObserver(observer MentionObserver) *Store {
	out := *s
	out.mentions = observer
	return &out
}

// commentColumns is the projection every read and mutation returns, expecting the
// comment row aliased c and the author's user row left-joined as u. The author's
// name is resolved here rather than by the caller so one query yields everything
// a thread needs to render; it collapses to the empty string both for an empty
// display name (fall back to the username) and for a deleted account (no u row).
// The three subject columns are collapsed to the empty string as well: exactly
// one of them is ever set, and Comment reports the unset ones as absent. The last
// column is the comment's mentions as a JSON array (see mentionsColumn).
const commentColumns = `c.uid, COALESCE(c.photo_uid, ''), COALESCE(c.task_uid, ''), COALESCE(c.album_uid, ''),
	COALESCE(c.author_uid, ''),
	COALESCE(NULLIF(u.display_name, ''), u.username, ''),
	s.cover_photo_uid,
	c.body, c.created_at, c.edited_at, ` + mentionsColumn

// mentionsColumn resolves the comment's stored mentions (comment_mentions) to
// the accounts' current names and the people their memberships of the
// comment's project link, as one JSON array in order of first mention, empty —
// never NULL — for a comment without any. It is a correlated subquery rather
// than a join so a comment stays one row however many people it names; an
// account deleted since has no row (the cascade took it), so it is simply
// absent, and the client falls back to the token's name.
const mentionsColumn = `COALESCE((
	SELECT jsonb_agg(jsonb_build_object(
		'user_uid', m.user_uid,
		'name', COALESCE(NULLIF(mu.display_name, ''), mu.username),
		'subject_uid', mm.subject_uid) ORDER BY m.position)
	FROM comment_mentions m JOIN users mu ON mu.uid = m.user_uid
	LEFT JOIN project_members mm ON mm.user_uid = m.user_uid AND mm.project_uid = ` + commentProjectSQL + `
	WHERE m.comment_uid = c.uid), '[]'::jsonb)`

// commentProjectSQL is the project of the comment row aliased c: the project
// of whichever photo, task or album it hangs off (a comment carries no project
// of its own, see parentTables).
const commentProjectSQL = `COALESCE(
		(SELECT pp.project_id FROM photos pp WHERE pp.uid = c.photo_uid),
		(SELECT pt.project_id FROM photo_tasks pt WHERE pt.uid = c.task_uid),
		(SELECT pa.project_id FROM albums pa WHERE pa.uid = c.album_uid))`

// mutationColumns is what a mutating CTE returns for the outer select to join
// against. It is the raw row, not the resolved projection: the author's name is
// added by the join that reads the CTE back.
const mutationColumns = `uid, photo_uid, task_uid, album_uid, author_uid, body, created_at, edited_at`

// authorJoin resolves a comment's author to a user row, and that account to the
// person its membership of the comment's project says it is (who you are is a
// fact about one project's people). All three are LEFT JOINs, and for
// different reasons: the user because author_uid is ON DELETE SET NULL (losing
// the account must not lose the comment), the membership because the author
// may have left the project (or is a maintainer who never joined it), the
// subject because almost no account is linked to one and a linked one usually
// has no cover photo either. The joins are here rather than in a second query
// so one statement still yields everything a thread needs to render.
const authorJoin = ` LEFT JOIN users u ON u.uid = c.author_uid` +
	` LEFT JOIN project_members um ON um.user_uid = c.author_uid AND um.project_uid = ` + commentProjectSQL +
	` LEFT JOIN subjects s ON s.uid = um.subject_uid`

// parentTables maps each subject column to the root table its uid names. A
// comment carries no project of its own: it belongs to the project of the
// photo, task or album it hangs off, and every statement reads that parent's
// project_id.
var parentTables = map[string]string{
	"photo_uid": "photos",
	"task_uid":  "photo_tasks",
	"album_uid": "albums",
}

// inProjectSQL is the predicate confining a comment row, aliased alias, to the
// project bound to placeholder: whichever of its three subject columns is set
// must name a row of that project. It serves the reads and writes addressed by
// the comment's own uid, which do not know the subject kind in advance.
func inProjectSQL(alias, placeholder string) string {
	return "(EXISTS (SELECT 1 FROM photos pp WHERE pp.uid = " + alias + ".photo_uid AND pp.project_id = " +
		placeholder + ") OR EXISTS (SELECT 1 FROM photo_tasks pt WHERE pt.uid = " + alias +
		".task_uid AND pt.project_id = " + placeholder + ") OR EXISTS (SELECT 1 FROM albums pa WHERE pa.uid = " +
		alias + ".album_uid AND pa.project_id = " + placeholder + "))"
}

// requireProject returns ErrNoProject when projectUID is empty and nil
// otherwise. Every Store method calls it before touching the database.
func requireProject(projectUID string) error {
	if projectUID == "" {
		return ErrNoProject
	}
	return nil
}

// withProject defaults entry's ProjectUID to projectUID, so every audited
// comment write records the project it happened in.
func withProject(entry audit.Entry, projectUID string) audit.Entry {
	if entry.ProjectUID == "" {
		entry.ProjectUID = projectUID
	}
	return entry
}

// subjectStatements holds the four statements whose text depends on which
// column carries the subject. They are built once per kind at package
// initialisation rather than formatted per call, so no request ever assembles
// SQL and the column name can only ever come from the closed set below.
type subjectStatements struct {
	list        string
	countsAmong string
	latestAmong string
	create      string
}

// statements maps each subject kind to the statements written against its own
// column. A kind absent from this map cannot be queried at all, which is what
// makes ErrInvalidSubject the only possible outcome of an unknown one.
var statements = map[SubjectKind]subjectStatements{
	SubjectPhoto: buildStatements("photo_uid"),
	SubjectTask:  buildStatements("task_uid"),
	SubjectAlbum: buildStatements("album_uid"),
}

// parentInProjectJoin joins the subject row of column, aliased px, confined to
// the project bound to placeholder: a comment whose subject is another
// project's drops out of the read. The name says "project" on purpose: the
// tenancy guard cannot read a helper's text, and takes a project-named piece in
// a FROM list for the join condition it is.
func parentInProjectJoin(column, placeholder string) string {
	return " JOIN " + parentTables[column] + " px ON px.uid = c." + column + " AND px.project_id = " + placeholder
}

// buildStatements returns the per-kind statements for the subject stored in
// column: the thread listing (oldest first — a conversation reads forwards — with
// the UID as a stable tie-break for comments written in the same instant), the
// bulk count, the bulk "newest comment per subject" (DISTINCT ON over the same
// index the listing reads, newest first — the mirror image of the thread's
// tie-break, so the two never disagree about which of two same-instant comments
// is the last word), and the insert. Soft-deleted rows are filtered out of every
// read, as on every read path. Every read joins the subject confined to the
// project ($2), and the insert writes nothing unless the subject is a row of the
// project ($5) — so a thread never reads or grows across projects.
func buildStatements(column string) subjectStatements {
	return subjectStatements{
		list: `
SELECT ` + commentColumns + `
FROM comments c` + parentInProjectJoin(column, "$2") + authorJoin + `
WHERE c.` + column + ` = $1 AND c.deleted_at IS NULL
ORDER BY c.created_at, c.uid`,
		countsAmong: `
SELECT c.` + column + `, count(*)
FROM comments c` + parentInProjectJoin(column, "$2") + `
WHERE c.` + column + ` = ANY($1) AND c.deleted_at IS NULL
GROUP BY c.` + column,
		latestAmong: `
SELECT DISTINCT ON (c.` + column + `) ` + commentColumns + `
FROM comments c` + parentInProjectJoin(column, "$2") + authorJoin + `
WHERE c.` + column + ` = ANY($1) AND c.deleted_at IS NULL
ORDER BY c.` + column + `, c.created_at DESC, c.uid DESC`,
		create: `
WITH inserted AS (
    INSERT INTO comments (uid, ` + column + `, author_uid, body)
    SELECT $1::text, $2::text, $3::text, $4::text
    WHERE EXISTS (SELECT 1 FROM ` + parentTables[column] + ` px WHERE px.uid = $2::text AND px.project_id = $5)
    RETURNING ` + mutationColumns + `
)
SELECT ` + commentColumns + `
FROM inserted c` + authorJoin,
	}
}

// statementsFor returns the statements for subj, or ErrInvalidSubject when it
// names no known kind or carries no uid.
func statementsFor(subj Subject) (subjectStatements, error) {
	if !subj.Valid() {
		return subjectStatements{}, ErrInvalidSubject
	}
	stmts, ok := statements[subj.Kind]
	if !ok {
		return subjectStatements{}, ErrInvalidSubject
	}
	return stmts, nil
}

// List returns the live comments on subj, a subject of the project, oldest
// first, each carrying its author's resolved name. A subject with no comments —
// or one that does not exist, or belongs to another project — yields an empty
// slice and a nil error: a thread is a view of a photo, a task or an album, not a
// claim that it exists, and the caller has already resolved it.
// An unknown subject kind yields ErrInvalidSubject.
func (s *Store) List(ctx context.Context, projectUID string, subj Subject) ([]Comment, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	stmts, err := statementsFor(subj)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, stmts.list, subj.UID, projectUID)
	if err != nil {
		return nil, fmt.Errorf("comments: listing comments of %s %s: %w", subj.Kind, subj.UID, err)
	}
	defer rows.Close()

	out := make([]Comment, 0)
	for rows.Next() {
		c, scanErr := scanComment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("comments: reading comments of %s %s: %w", subj.Kind, subj.UID, scanErr)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("comments: iterating comments of %s %s: %w", subj.Kind, subj.UID, err)
	}
	return out, nil
}

// CountsAmong returns how many live comments each of uids — subjects of the
// project — has within the thread kind, keyed by subject UID; a subject of
// another project counts nothing. Subjects without a comment are absent from the map
// (a missing key reads as zero), and an empty input yields an empty map without
// querying. An unknown kind yields ErrInvalidSubject.
//
// It is deliberately the bulk shape even though the photo detail asks about a
// single photo: a per-photo count would be one query per item the moment a
// listing wanted the same badge, and this way that N+1 cannot be written by
// accident.
func (s *Store) CountsAmong(
	ctx context.Context, projectUID string, kind SubjectKind, uids []string,
) (map[string]int, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	out := make(map[string]int, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	stmts, err := statementsFor(Subject{Kind: kind, UID: "-"})
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, stmts.countsAmong, uids, projectUID)
	if err != nil {
		return nil, fmt.Errorf("comments: counting comments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			uid   string
			count int
		)
		if err := rows.Scan(&uid, &count); err != nil {
			return nil, fmt.Errorf("comments: scanning comment count: %w", err)
		}
		out[uid] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("comments: iterating comment counts: %w", err)
	}
	return out, nil
}

// getSQL reads one live comment by UID within the project $2. It needs no
// per-kind variant: a comment is addressed by its own UID, whatever it hangs
// off, and inProjectSQL checks whichever subject it has.
var getSQL = `
SELECT ` + commentColumns + `
FROM comments c` + authorJoin + `
WHERE c.uid = $1 AND c.deleted_at IS NULL AND ` + inProjectSQL("c", "$2")

// LatestAmong returns the newest live comment on each of uids — subjects of the
// project — within the thread kind, keyed by subject UID. Subjects without a live comment are absent from
// the map, and an empty input yields an empty map without querying. An unknown
// kind yields ErrInvalidSubject.
//
// It is one grouped query for the whole page for the same reason CountsAmong
// is: the caller is a listing (a task's review ledger, one row per photograph),
// and a per-row lookup would be the N+1 that shape invites. The DISTINCT ON
// walks idx_comments_photo / idx_comments_task / idx_comments_album backwards,
// so the cost is one index probe per subject rather than a sort over its thread.
func (s *Store) LatestAmong(
	ctx context.Context, projectUID string, kind SubjectKind, uids []string,
) (map[string]Comment, error) {
	if err := requireProject(projectUID); err != nil {
		return nil, err
	}
	out := make(map[string]Comment, len(uids))
	if len(uids) == 0 {
		return out, nil
	}
	stmts, err := statementsFor(Subject{Kind: kind, UID: "-"})
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, stmts.latestAmong, uids, projectUID)
	if err != nil {
		return nil, fmt.Errorf("comments: reading latest comments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		c, scanErr := scanComment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("comments: scanning latest comment: %w", scanErr)
		}
		out[c.Subject().UID] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("comments: iterating latest comments: %w", err)
	}
	return out, nil
}

// Get returns the live comment of the project with the given UID, or
// ErrNotFound when it does not exist, has been soft-deleted or hangs off a
// subject of another project. The HTTP layer reads the comment before
// editing or deleting it, both to answer 404 for a comment that is not there and
// to decide authorship from the stored row rather than from the request.
func (s *Store) Get(ctx context.Context, projectUID, uid string) (Comment, error) {
	if err := requireProject(projectUID); err != nil {
		return Comment{}, err
	}
	c, err := scanComment(s.pool.QueryRow(ctx, getSQL, uid, projectUID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("comments: reading comment %s: %w", uid, err)
	}
	return c, nil
}

// Create stores a new comment by authorUID on subj, a subject of the project,
// and writes entry to the audit
// log in the same transaction, so a comment that exists always has a record of
// who wrote it. The body is trimmed and validated (ErrEmptyBody /
// ErrBodyTooLong); a subject that does not exist — or belongs to another project
// — yields ErrSubjectNotFound and writes nothing, and an unknown kind
// ErrInvalidSubject. An empty authorUID stores SQL NULL, which
// only a caller without a principal can produce.
//
// The new comment's UID is stamped into the audit entry's details as
// "comment_uid" — the caller cannot know it in advance, and without it a
// create entry could not be tied to the row it created.
func (s *Store) Create(
	ctx context.Context, projectUID string, subj Subject, authorUID, body string, entry audit.Entry,
) (Comment, error) {
	return s.CreateAlong(ctx, projectUID, subj, authorUID, body, entry, nil)
}

// Along is a further write a caller wants committed together with a comment: it
// runs on the comment's own transaction, after the comment and its audit entry
// are written and before the commit. Its error rolls the whole transaction back
// — the comment included — and reaches the caller unwrapped, so the caller's own
// sentinel errors stay classifiable.
type Along func(ctx context.Context, tx pgx.Tx) error

// CreateAlong is Create with along run on the same transaction, so a comment and
// a write it belongs with (a task's thread answer handing the task on to its
// next state) land together or not at all. A nil along makes it exactly Create.
//
// The comment is written first: a subject that does not exist fails it with
// ErrSubjectNotFound before along is asked to do anything.
func (s *Store) CreateAlong(
	ctx context.Context, projectUID string, subj Subject, authorUID, body string, entry audit.Entry, along Along,
) (Comment, error) {
	if err := requireProject(projectUID); err != nil {
		return Comment{}, err
	}
	stmts, err := statementsFor(subj)
	if err != nil {
		return Comment{}, err
	}
	trimmed, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	uid, err := newCommentUID()
	if err != nil {
		return Comment{}, err
	}
	c, err := s.mutateAudited(ctx, withProject(entry, projectUID), "creating comment", s.mentionsThen(along),
		stmts.create, uid, subj.UID, nullableUID(authorUID), trimmed, projectUID)
	if errors.Is(err, ErrNotFound) {
		// The insert wrote no row: its subject is not one of the project's.
		return Comment{}, ErrSubjectNotFound
	}
	return c, err
}

// updateSQL rewrites a live comment's body and stamps edited_at, reading the row
// back with its author resolved. A soft-deleted comment, or one on a subject of
// another project than $3, matches nothing, so editing one is a not-found rather
// than a resurrection.
var updateSQL = `
WITH updated AS (
    UPDATE comments c
    SET body = $2, edited_at = now()
    WHERE c.uid = $1 AND c.deleted_at IS NULL AND ` + inProjectSQL("c", "$3") + `
    RETURNING ` + mutationColumns + `
)
SELECT ` + commentColumns + `
FROM updated c` + authorJoin

// Update rewrites the body of the live comment uid of the project, stamps its edited_at and
// writes entry to the audit log in the same transaction. The body is validated
// exactly as on create; a missing, already-deleted or foreign comment yields
// ErrNotFound.
// Authorization (only the author may edit) is the caller's decision — the store
// enforces existence, not policy.
func (s *Store) Update(ctx context.Context, projectUID, uid, body string, entry audit.Entry) (Comment, error) {
	if err := requireProject(projectUID); err != nil {
		return Comment{}, err
	}
	trimmed, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	return s.mutateAudited(ctx, withProject(entry, projectUID), "updating comment", s.mentionsThen(nil),
		updateSQL, uid, trimmed, projectUID)
}

// deleteSQL soft-deletes a live comment on a subject of the project $2,
// returning the row it stamped so the caller can tell a real delete from a
// no-op.
var deleteSQL = `
WITH removed AS (
    UPDATE comments c
    SET deleted_at = now()
    WHERE c.uid = $1 AND c.deleted_at IS NULL AND ` + inProjectSQL("c", "$2") + `
    RETURNING ` + mutationColumns + `
)
SELECT ` + commentColumns + `
FROM removed c` + authorJoin

// Delete soft-deletes the live comment uid of the project — the row stays with deleted_at
// stamped and drops out of every read path — and writes entry to the audit log in
// the same transaction, so a removal is always explainable afterwards. A missing
// or already-deleted comment yields ErrNotFound rather than succeeding silently:
// deleting twice is a client bug, and the second attempt has nothing to audit.
// Who may delete (the author, or an admin removing anyone's) is the caller's
// decision.
func (s *Store) Delete(ctx context.Context, projectUID, uid string, entry audit.Entry) error {
	if err := requireProject(projectUID); err != nil {
		return err
	}
	_, err := s.mutateAudited(ctx, withProject(entry, projectUID), "deleting comment", nil, deleteSQL, uid,
		projectUID)
	return err
}

// postStep is work that follows the mutation on its transaction, with the
// comment as the mutation read it back — and may amend it (the mentions a
// create just wrote). Its error is returned as it came.
type postStep func(ctx context.Context, tx pgx.Tx, c *Comment) error

// mutateAudited runs query — a single-row mutation that reads the changed comment
// back — writes entry (stamped with that comment's UID) on the same transaction
// and commits, so the change and its audit record are atomic: if either fails the
// transaction rolls back and neither persists. op names the operation for error
// context. A non-nil post runs on the same transaction before the commit (see
// postStep); its error is returned as it came.
func (s *Store) mutateAudited(
	ctx context.Context, entry audit.Entry, op string, post postStep, query string, args ...any,
) (Comment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Comment{}, fmt.Errorf("comments: begin %s transaction: %w", op, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	c, err := scanComment(tx.QueryRow(ctx, query, args...))
	if err != nil {
		return Comment{}, translateMutation(err, op)
	}
	if err := audit.Write(ctx, tx, entryWithComment(entry, c.UID)); err != nil {
		return Comment{}, fmt.Errorf("comments: %s: writing audit entry: %w", op, err)
	}
	if post != nil {
		if err := post(ctx, tx, &c); err != nil {
			return Comment{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, fmt.Errorf("comments: commit %s transaction: %w", op, err)
	}
	return c, nil
}

// entryWithComment returns entry with the affected comment's UID added to its
// details, leaving the caller's map untouched (it is copied, not mutated). The
// audit target stays the subject — a comment is only ever read in the context of
// the picture or the task it hangs off — so the comment UID lives in the details.
func entryWithComment(entry audit.Entry, commentUID string) audit.Entry {
	details := make(map[string]any, len(entry.Details)+1)
	maps.Copy(details, entry.Details)
	details["comment_uid"] = commentUID
	entry.Details = details
	return entry
}

// rowScanner is the subset of pgx.Row/pgx.Rows scanComment needs, so one scanner
// serves both the single-row reads and the list iteration.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanComment reads one comment row in commentColumns order. Its error carries
// no package prefix: every caller adds the operation that failed, and the cause
// stays wrapped so pgx.ErrNoRows and constraint violations remain classifiable.
func scanComment(row rowScanner) (Comment, error) {
	var c Comment
	if err := row.Scan(&c.UID, &c.PhotoUID, &c.TaskUID, &c.AlbumUID, &c.AuthorUID, &c.AuthorName,
		&c.AuthorPhotoUID, &c.Body, &c.CreatedAt, &c.EditedAt, &c.Mentions); err != nil {
		return Comment{}, fmt.Errorf("scanning comment row: %w", err)
	}
	if c.Mentions == nil {
		c.Mentions = []Mention{}
	}
	return c, nil
}

// translateMutation maps a failed mutation to the package's sentinel errors: no
// row changed means the comment is gone (or never existed), and a foreign-key
// violation on any subject column means the photo, task or album is. Anything
// else is wrapped with op.
func translateMutation(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation &&
		(strings.Contains(pgErr.ConstraintName, "photo_uid") ||
			strings.Contains(pgErr.ConstraintName, "task_uid") ||
			strings.Contains(pgErr.ConstraintName, "album_uid")) {
		return ErrSubjectNotFound
	}
	return fmt.Errorf("comments: %s: %w", op, err)
}

// nullableUID returns nil for an empty UID so the author_uid column stores SQL
// NULL, or the value otherwise. Every write route is behind an authentication
// guard, so a non-empty UID is the norm; a pass-through guard (unit tests) may
// leave it empty.
func nullableUID(uid string) any {
	if uid == "" {
		return nil
	}
	return uid
}
