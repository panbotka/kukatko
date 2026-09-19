package phototask

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/audit"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation.
const foreignKeyViolation = "23503"

// insertTaskSQL opens a task. Every column the rules decided is written
// explicitly rather than left to a default, so the row can never disagree with
// what newFields validated.
const insertTaskSQL = `
INSERT INTO photo_tasks (uid, title, body, state, resolution, source_query,
	created_by, state_at, state_by, closed_at, closed_by, options)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

// insertMembersSQL adds photographs to a task, ignoring the ones already there
// so adding the same batch twice is not an error.
const insertMembersSQL = `
INSERT INTO photo_task_photos (task_uid, photo_uid)
SELECT $1, u FROM unnest($2::text[]) AS u
ON CONFLICT DO NOTHING`

// Create opens a task over photoUIDs and writes entry to the audit log in the
// same transaction, so a task that exists always has a record of who opened it
// and over which photographs. The actor joins the task's participants in that
// same transaction — acting on a question is what puts somebody on it. The text fields are trimmed and validated
// (ErrEmptyTitle, ErrTooLong); a task opened in a closed state must carry a
// resolution (ErrClosedNeedsResolution) and an unknown state is ErrInvalidState.
// A photograph that does not exist yields ErrPhotoNotFound and nothing is
// written.
//
// The new task's UID becomes the audit entry's target when the caller left it
// empty — it cannot be known in advance.
func (s *Store) Create(ctx context.Context, t Task, photoUIDs []string, entry audit.Entry) (Task, error) {
	if len(photoUIDs) > MaxPhotos {
		return Task{}, fmt.Errorf("%w: %d over the limit of %d", ErrTooManyPhotos, len(photoUIDs), MaxPhotos)
	}
	f, err := newFields(t, entry.ActorUID, time.Now().UTC())
	if err != nil {
		return Task{}, err
	}
	uid, err := newTaskUID()
	if err != nil {
		return Task{}, err
	}
	err = s.inTx(ctx, "creating task", func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, insertTaskSQL, uid, f.Title, f.Body, string(f.State),
			f.Resolution, f.Query, nullableUID(entry.ActorUID), f.StateAt, nullableUID(f.StateBy),
			f.ClosedAt, nullableUID(f.ClosedBy), f.Options); err != nil {
			return fmt.Errorf("inserting task row: %w", err)
		}
		if err := addMembers(ctx, tx, uid, photoUIDs); err != nil {
			return err
		}
		// Opening a question is the strongest statement of being on it.
		if err := joinParticipant(ctx, tx, uid, entry.ActorUID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, withTarget(entry, uid, "photo_uids", photoUIDs))
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, uid, entry.ActorUID)
}

// updateTaskSQL rewrites every field an edit may touch. state_at, state_by,
// closed_at and closed_by are written too because they are consequences of the
// state, computed by applyUpdate rather than by SQL.
const updateTaskSQL = `
UPDATE photo_tasks
SET title = $2, body = $3, state = $4, resolution = $5, source_query = $6,
	state_at = $7, state_by = $8, closed_at = $9, closed_by = $10, options = $11, updated_at = now()
WHERE uid = $1`

// currentSQL reads the columns an edit folds onto, locking the row for the rest
// of the transaction so two concurrent edits cannot both decide from the same
// stale state. It is the bare row: the joins and counts of the full projection
// cannot be locked, and an edit does not need them.
const currentSQL = `
SELECT title, body, state, resolution, source_query, state_at, COALESCE(state_by, ''),
	closed_at, COALESCE(closed_by, ''), options
FROM photo_tasks WHERE uid = $1 FOR UPDATE`

// Update folds upd onto the stored task, writes entry — stamped with the
// field-level diff — in the same transaction, and returns the task as it now
// stands. A missing task yields ErrNotFound; the validation errors are the same
// ones Create raises, and closing without a resolution is refused
// (ErrClosedNeedsResolution).
//
// Changing the state moves state_at and names the actor in state_by, which is
// what resets "has anybody replied since" and makes the move theirs; an edit that
// leaves the state alone deliberately does neither.
func (s *Store) Update(ctx context.Context, uid string, upd Update, entry audit.Entry) (Task, error) {
	err := s.inTx(ctx, "updating task", func(tx pgx.Tx) error {
		cur, err := scanCurrent(tx.QueryRow(ctx, currentSQL, uid))
		if err != nil {
			return err
		}
		next, changes, err := applyUpdate(cur, upd, entry.ActorUID, time.Now().UTC())
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, updateTaskSQL, uid, next.Title, next.Body, string(next.State),
			next.Resolution, next.Query, next.StateAt, nullableUID(next.StateBy), next.ClosedAt,
			nullableUID(next.ClosedBy), next.Options); err != nil {
			return fmt.Errorf("updating task row: %w", err)
		}
		if err := joinParticipant(ctx, tx, uid, entry.ActorUID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, withChanges(entry, uid, changes))
	})
	if err != nil {
		return Task{}, err
	}
	return s.Get(ctx, uid, entry.ActorUID)
}

// Delete removes a task outright — with its membership and its thread, which
// cascade — and writes entry in the same transaction. A missing task yields
// ErrNotFound rather than succeeding silently.
//
// Deleting is the exception, not the way work ends. A finished task is closed:
// its frozen list of photographs is the record of what a batch of edits touched,
// and that record is the reason the task outlives the question.
func (s *Store) Delete(ctx context.Context, uid string, entry audit.Entry) error {
	return s.inTx(ctx, "deleting task", func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "DELETE FROM photo_tasks WHERE uid = $1", uid)
		if err != nil {
			return fmt.Errorf("deleting task row: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit.Write(ctx, tx, withTarget(entry, uid, "", nil))
	})
}

// AddPhotos adds photoUIDs to the task and writes entry in the same
// transaction, returning how many were actually new. Photographs already in the
// task are ignored, so replaying a batch is harmless. A missing task yields
// ErrNotFound, a missing photograph ErrPhotoNotFound, and a membership that
// would grow past MaxPhotos ErrTooManyPhotos — in every case nothing is written.
func (s *Store) AddPhotos(ctx context.Context, uid string, photoUIDs []string, entry audit.Entry) (int, error) {
	var added int
	err := s.inTx(ctx, "adding photos to task", func(tx pgx.Tx) error {
		if err := lockTask(ctx, tx, uid); err != nil {
			return err
		}
		if err := checkRoom(ctx, tx, uid, len(photoUIDs)); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, insertMembersSQL, uid, photoUIDs)
		if err != nil {
			return fmt.Errorf("inserting task membership: %w", err)
		}
		added = int(tag.RowsAffected())
		if err := touch(ctx, tx, uid); err != nil {
			return err
		}
		if err := joinParticipant(ctx, tx, uid, entry.ActorUID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, withTarget(entry, uid, "photo_uids", photoUIDs))
	})
	return added, err
}

// RemovePhotos drops photoUIDs from the task and writes entry in the same
// transaction, returning how many were actually removed. Photographs that were
// not part of it are ignored. A missing task yields ErrNotFound.
func (s *Store) RemovePhotos(
	ctx context.Context, uid string, photoUIDs []string, entry audit.Entry,
) (int, error) {
	var removed int
	err := s.inTx(ctx, "removing photos from task", func(tx pgx.Tx) error {
		if err := lockTask(ctx, tx, uid); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			"DELETE FROM photo_task_photos WHERE task_uid = $1 AND photo_uid = ANY($2)",
			uid, photoUIDs)
		if err != nil {
			return fmt.Errorf("deleting task membership: %w", err)
		}
		removed = int(tag.RowsAffected())
		if err := touch(ctx, tx, uid); err != nil {
			return err
		}
		if err := joinParticipant(ctx, tx, uid, entry.ActorUID); err != nil {
			return err
		}
		return audit.Write(ctx, tx, withTarget(entry, uid, "photo_uids", photoUIDs))
	})
	return removed, err
}

// addMembers inserts the task's initial membership, skipping the query entirely
// for a task opened over nothing.
func addMembers(ctx context.Context, tx pgx.Tx, uid string, photoUIDs []string) error {
	if len(photoUIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, insertMembersSQL, uid, photoUIDs); err != nil {
		return fmt.Errorf("inserting task membership: %w", err)
	}
	return nil
}

// lockTask takes the row lock and reports ErrNotFound for a task that is not
// there, so a membership change on a missing task fails before it writes.
func lockTask(ctx context.Context, tx pgx.Tx, uid string) error {
	var found string
	err := tx.QueryRow(ctx, "SELECT uid FROM photo_tasks WHERE uid = $1 FOR UPDATE", uid).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("locking task row: %w", err)
	}
	return nil
}

// checkRoom refuses a membership that would grow past MaxPhotos. The count is an
// upper bound — some of the incoming UIDs may already be members — so it can
// refuse a batch that would in fact have fitted; at these sizes that is the right
// trade for one cheap query.
func checkRoom(ctx context.Context, tx pgx.Tx, uid string, incoming int) error {
	var current int
	if err := tx.QueryRow(ctx,
		"SELECT count(*) FROM photo_task_photos WHERE task_uid = $1", uid).Scan(&current); err != nil {
		return fmt.Errorf("counting task membership: %w", err)
	}
	if current+incoming > MaxPhotos {
		return fmt.Errorf("%w: %d + %d over the limit of %d",
			ErrTooManyPhotos, current, incoming, MaxPhotos)
	}
	return nil
}

// touch stamps the task as changed, so a membership edit counts as activity in
// the listing's ordering exactly as an edit to its text does.
func touch(ctx context.Context, tx pgx.Tx, uid string) error {
	if _, err := tx.Exec(ctx, "UPDATE photo_tasks SET updated_at = now() WHERE uid = $1", uid); err != nil {
		return fmt.Errorf("touching task row: %w", err)
	}
	return nil
}

// inTx runs mutate on a transaction and commits, so the change and its audit
// record are atomic: if either fails the transaction rolls back and neither
// persists. op names the operation for error context, and the package's sentinel
// errors pass through untouched.
func (s *Store) inTx(ctx context.Context, op string, mutate func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("phototask: begin %s transaction: %w", op, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := mutate(tx); err != nil {
		return translate(err, op)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("phototask: commit %s transaction: %w", op, err)
	}
	return nil
}

// scanCurrent reads the columns an edit folds onto into the Task fields
// applyUpdate consults.
func scanCurrent(row rowScanner) (Task, error) {
	var t Task
	err := row.Scan(&t.Title, &t.Body, &t.State, &t.Resolution, &t.Query,
		&t.StateAt, &t.StateByUID, &t.ClosedAt, &t.ClosedByUID, &t.Options)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, fmt.Errorf("scanning task row: %w", err)
	}
	if t.Options == nil {
		t.Options = []string{}
	}
	return t, nil
}

// withTarget returns entry with the task as its target and, when key is given,
// one detail added — leaving the caller's map untouched (it is copied, not
// mutated). An entry that already names a target keeps it.
func withTarget(entry audit.Entry, uid, key string, value any) audit.Entry {
	if entry.TargetType == "" {
		entry.TargetType = "photo_tasks"
	}
	if entry.TargetUID == "" {
		entry.TargetUID = uid
	}
	if key == "" {
		return entry
	}
	details := make(map[string]any, len(entry.Details)+1)
	maps.Copy(details, entry.Details)
	details[key] = value
	entry.Details = details
	return entry
}

// withChanges returns entry targeted at the task and carrying the field-level
// diff of the edit, so the trail records what a value was before it changed.
func withChanges(entry audit.Entry, uid string, changes *audit.ChangeSet) audit.Entry {
	entry = withTarget(entry, uid, "", nil)
	details := make(map[string]any, len(entry.Details)+1)
	maps.Copy(details, entry.Details)
	changes.StampInto(details)
	entry.Details = details
	return entry
}

// translate maps a failed mutation to the package's sentinel errors: a
// foreign-key violation on photo_uid means one of the photographs is gone, one on
// task_uid means the task is. The package's own sentinels pass through, and
// anything else is wrapped with op.
func translate(err error, op string) error {
	if isSentinel(err) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		switch {
		case strings.Contains(pgErr.ConstraintName, "photo_uid"):
			return ErrPhotoNotFound
		case strings.Contains(pgErr.ConstraintName, "user_uid"):
			return ErrUserNotFound
		case strings.Contains(pgErr.ConstraintName, "task_uid"):
			return ErrNotFound
		}
	}
	return fmt.Errorf("phototask: %s: %w", op, err)
}

// isSentinel reports whether err is one of the package's own errors, which carry
// their meaning already and must not be wrapped into an opaque failure.
func isSentinel(err error) bool {
	for _, sentinel := range []error{
		ErrNotFound, ErrPhotoNotFound, ErrEmptyTitle, ErrTooLong,
		ErrInvalidState, ErrClosedNeedsResolution, ErrTooManyPhotos,
		ErrUserNotFound, ErrTooManyOptions, ErrEmptyOption, ErrDuplicateOption,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// nullableUID returns nil for an empty UID so the column stores SQL NULL, or the
// value otherwise. Every write route is behind an authentication guard, so a
// non-empty actor is the norm; a pass-through guard (unit tests) may leave it
// empty.
func nullableUID(uid string) any {
	if uid == "" {
		return nil
	}
	return uid
}
