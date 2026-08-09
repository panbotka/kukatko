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

// Store is the database access layer for photo comments. It owns no connection;
// it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// commentColumns is the projection every read and mutation returns, expecting the
// comment row aliased c and the author's user row left-joined as u. The author's
// name is resolved here rather than by the caller so one query yields everything
// a thread needs to render; it collapses to the empty string both for an empty
// display name (fall back to the username) and for a deleted account (no u row).
const commentColumns = `c.uid, c.photo_uid, COALESCE(c.author_uid, ''),
	COALESCE(NULLIF(u.display_name, ''), u.username, ''),
	c.body, c.created_at, c.edited_at`

// authorJoin resolves a comment's author to a user row. It is a LEFT JOIN because
// author_uid is ON DELETE SET NULL: losing the account must not lose the comment.
const authorJoin = ` LEFT JOIN users u ON u.uid = c.author_uid`

// listSQL reads one photo's live comments oldest first — a conversation reads
// forwards — with the UID as a stable tie-break for comments written in the same
// instant. Soft-deleted rows are filtered out here, as on every read path.
const listSQL = `
SELECT ` + commentColumns + `
FROM photo_comments c` + authorJoin + `
WHERE c.photo_uid = $1 AND c.deleted_at IS NULL
ORDER BY c.created_at, c.uid`

// List returns the live comments on photoUID, oldest first, each carrying its
// author's resolved name. A photo with no comments — or one that does not exist —
// yields an empty slice and a nil error: a thread is a view of a photo, not a
// claim that it exists, and the caller has already resolved the photo.
func (s *Store) List(ctx context.Context, photoUID string) ([]Comment, error) {
	rows, err := s.pool.Query(ctx, listSQL, photoUID)
	if err != nil {
		return nil, fmt.Errorf("comments: listing comments of photo %s: %w", photoUID, err)
	}
	defer rows.Close()

	out := make([]Comment, 0)
	for rows.Next() {
		c, scanErr := scanComment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("comments: reading comments of photo %s: %w", photoUID, scanErr)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("comments: iterating comments of photo %s: %w", photoUID, err)
	}
	return out, nil
}

// countsAmongSQL counts the live comments of many photos in one aggregate pass,
// so annotating a payload never costs a query per photo.
const countsAmongSQL = `
SELECT photo_uid, count(*)
FROM photo_comments
WHERE photo_uid = ANY($1) AND deleted_at IS NULL
GROUP BY photo_uid`

// CountsAmong returns how many live comments each of photoUIDs has, keyed by
// photo UID. Photos without a comment are absent from the map (a missing key
// reads as zero), and an empty input yields an empty map without querying.
//
// It is deliberately the bulk shape even though the photo detail asks about a
// single photo: a per-photo count would be one query per item the moment a
// listing wanted the same badge, and this way that N+1 cannot be written by
// accident.
func (s *Store) CountsAmong(ctx context.Context, photoUIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(photoUIDs))
	if len(photoUIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, countsAmongSQL, photoUIDs)
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

// getSQL reads one live comment by UID.
const getSQL = `
SELECT ` + commentColumns + `
FROM photo_comments c` + authorJoin + `
WHERE c.uid = $1 AND c.deleted_at IS NULL`

// Get returns the live comment with the given UID, or ErrNotFound when it does
// not exist or has been soft-deleted. The HTTP layer reads the comment before
// editing or deleting it, both to answer 404 for a comment that is not there and
// to decide authorship from the stored row rather than from the request.
func (s *Store) Get(ctx context.Context, uid string) (Comment, error) {
	c, err := scanComment(s.pool.QueryRow(ctx, getSQL, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("comments: reading comment %s: %w", uid, err)
	}
	return c, nil
}

// createSQL inserts a comment and reads it back with its author resolved. The
// insert is wrapped in a CTE so the join to users can run over the new row in the
// same round-trip.
const createSQL = `
WITH inserted AS (
    INSERT INTO photo_comments (uid, photo_uid, author_uid, body)
    VALUES ($1, $2, $3, $4)
    RETURNING uid, photo_uid, author_uid, body, created_at, edited_at
)
SELECT ` + commentColumns + `
FROM inserted c` + authorJoin

// Create stores a new comment by authorUID on photoUID and writes entry to the
// audit log in the same transaction, so a comment that exists always has a record
// of who wrote it. The body is trimmed and validated (ErrEmptyBody /
// ErrBodyTooLong); a photo that does not exist yields ErrPhotoNotFound. An empty
// authorUID stores SQL NULL, which only a caller without a principal can produce.
//
// The new comment's UID is stamped into the audit entry's details as
// "comment_uid" — the caller cannot know it in advance, and without it a
// create entry could not be tied to the row it created.
func (s *Store) Create(ctx context.Context, photoUID, authorUID, body string, entry audit.Entry) (Comment, error) {
	trimmed, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	uid, err := newCommentUID()
	if err != nil {
		return Comment{}, err
	}
	return s.mutateAudited(ctx, entry, "creating comment", createSQL,
		uid, photoUID, nullableUID(authorUID), trimmed)
}

// updateSQL rewrites a live comment's body and stamps edited_at, reading the row
// back with its author resolved. A soft-deleted comment matches nothing, so
// editing one is a not-found rather than a resurrection.
const updateSQL = `
WITH updated AS (
    UPDATE photo_comments
    SET body = $2, edited_at = now()
    WHERE uid = $1 AND deleted_at IS NULL
    RETURNING uid, photo_uid, author_uid, body, created_at, edited_at
)
SELECT ` + commentColumns + `
FROM updated c` + authorJoin

// Update rewrites the body of the live comment uid, stamps its edited_at and
// writes entry to the audit log in the same transaction. The body is validated
// exactly as on create; a missing or already-deleted comment yields ErrNotFound.
// Authorization (only the author may edit) is the caller's decision — the store
// enforces existence, not policy.
func (s *Store) Update(ctx context.Context, uid, body string, entry audit.Entry) (Comment, error) {
	trimmed, err := normalizeBody(body)
	if err != nil {
		return Comment{}, err
	}
	return s.mutateAudited(ctx, entry, "updating comment", updateSQL, uid, trimmed)
}

// deleteSQL soft-deletes a live comment, returning the row it stamped so the
// caller can tell a real delete from a no-op.
const deleteSQL = `
WITH removed AS (
    UPDATE photo_comments
    SET deleted_at = now()
    WHERE uid = $1 AND deleted_at IS NULL
    RETURNING uid, photo_uid, author_uid, body, created_at, edited_at
)
SELECT ` + commentColumns + `
FROM removed c` + authorJoin

// Delete soft-deletes the live comment uid — the row stays with deleted_at
// stamped and drops out of every read path — and writes entry to the audit log in
// the same transaction, so a removal is always explainable afterwards. A missing
// or already-deleted comment yields ErrNotFound rather than succeeding silently:
// deleting twice is a client bug, and the second attempt has nothing to audit.
// Who may delete (the author, or an admin removing anyone's) is the caller's
// decision.
func (s *Store) Delete(ctx context.Context, uid string, entry audit.Entry) error {
	_, err := s.mutateAudited(ctx, entry, "deleting comment", deleteSQL, uid)
	return err
}

// mutateAudited runs query — a single-row mutation that reads the changed comment
// back — writes entry (stamped with that comment's UID) on the same transaction
// and commits, so the change and its audit record are atomic: if either fails the
// transaction rolls back and neither persists. op names the operation for error
// context.
func (s *Store) mutateAudited(
	ctx context.Context, entry audit.Entry, op, query string, args ...any,
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
	if err := tx.Commit(ctx); err != nil {
		return Comment{}, fmt.Errorf("comments: commit %s transaction: %w", op, err)
	}
	return c, nil
}

// entryWithComment returns entry with the affected comment's UID added to its
// details, leaving the caller's map untouched (it is copied, not mutated). The
// audit target stays the photo — a comment is only ever read in the context of
// the picture it hangs off — so the comment UID lives in the details.
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
	if err := row.Scan(&c.UID, &c.PhotoUID, &c.AuthorUID, &c.AuthorName,
		&c.Body, &c.CreatedAt, &c.EditedAt); err != nil {
		return Comment{}, fmt.Errorf("scanning comment row: %w", err)
	}
	return c, nil
}

// translateMutation maps a failed mutation to the package's sentinel errors: no
// row changed means the comment is gone (or never existed), and a foreign-key
// violation on photo_uid means the photo is. Anything else is wrapped with op.
func translateMutation(err error, op string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation &&
		strings.Contains(pgErr.ConstraintName, "photo_uid") {
		return ErrPhotoNotFound
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
