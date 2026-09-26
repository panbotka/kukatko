package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation.
const foreignKeyViolation = "23503"

// Store is the database access layer for notifications, their photo sets and
// the per-account preferences. It owns no connection; it borrows the shared pgx
// pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// row is the column scanner shared by the single-row queries.
type row interface {
	Scan(dest ...any) error
}

// selectColumns is the column list every notification read shares, qualified by
// the n alias. The photo count reads the frozen set as it stands now, so a
// photograph the cascade removed no longer counts.
const selectColumns = `n.uid, n.user_uid, n.kind, n.title, n.body, n.link, n.created_at, n.read_at,
	(SELECT count(*) FROM notification_photos np WHERE np.notification_uid = n.uid)`

// scanNotification reads one notification row, mapping pgx.ErrNoRows to
// ErrNotFound so callers can branch on the sentinel.
func scanNotification(r row) (Notification, error) {
	var (
		n    Notification
		kind string
	)
	err := r.Scan(&n.UID, &n.UserUID, &kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &n.ReadAt, &n.PhotoCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrNotFound
	}
	if err != nil {
		return Notification{}, fmt.Errorf("notification: scanning row: %w", err)
	}
	n.Kind = Kind(kind)
	return n, nil
}

// insertSQL records one notification; its photo set follows in insertPhotosSQL.
const insertSQL = `
INSERT INTO notifications (uid, user_uid, kind, title, body, link)
VALUES ($1, $2, $3, $4, $5, $6)`

// insertPhotosSQL writes the frozen set in one statement. WITH ORDINALITY turns
// the array order into the stored position (zero-based), so the set reads back
// in exactly the order it was given.
const insertPhotosSQL = `
INSERT INTO notification_photos (notification_uid, photo_uid, position)
SELECT $1, u.photo_uid, u.ord - 1
FROM unnest($2::text[]) WITH ORDINALITY AS u (photo_uid, ord)`

// getSQL reads one notification scoped to its owner.
const getSQL = `SELECT ` + selectColumns + `
FROM notifications n
WHERE n.uid = $1 AND n.user_uid = $2`

// Create records n together with its photo set in one transaction: either the
// notification and every photograph of its set are written, or nothing is. The
// fields are validated first (ErrInvalid, ErrUnknownKind, ErrTooManyPhotos); a
// photograph that does not exist yields ErrPhotoNotFound and an account that
// does not exist ErrUserNotFound, and in either case nothing is written. A
// photograph given twice keeps its first position.
//
// Create sends nothing and does not consult the account's preferences — the
// caller asks Wants first.
func (s *Store) Create(ctx context.Context, n New) (Notification, error) {
	var out Notification
	err := s.inTx(ctx, "creating notification", func(tx pgx.Tx) error {
		var err error
		out, err = s.CreateTx(ctx, tx, n)
		return err
	})
	if err != nil {
		return Notification{}, err
	}
	return out, nil
}

// CreateTx is Create on the caller's open transaction tx instead of one of its
// own, so a notification caused by another mutation commits with that mutation
// or not at all. It validates, maps a missing photograph or account and sends
// nothing exactly as Create does. A failed insert leaves tx aborted, as any
// failed statement does; a caller that must survive the failure runs CreateTx
// under a savepoint (tx.Begin).
func (s *Store) CreateTx(ctx context.Context, tx pgx.Tx, n New) (Notification, error) {
	photos, err := n.validate()
	if err != nil {
		return Notification{}, err
	}
	uid, err := newNotificationUID()
	if err != nil {
		return Notification{}, err
	}
	if _, err := tx.Exec(ctx, insertSQL, uid, n.UserUID, string(n.Kind), n.Title, n.Body, n.Link); err != nil {
		return Notification{}, mapForeignKey(err)
	}
	if len(photos) > 0 {
		if _, err := tx.Exec(ctx, insertPhotosSQL, uid, photos); err != nil {
			return Notification{}, mapForeignKey(err)
		}
	}
	return scanNotification(tx.QueryRow(ctx, getSQL, uid, n.UserUID))
}

// Get returns the notification uid if it belongs to ownerUID. A notification
// that does not exist and one that belongs to somebody else are the same
// ErrNotFound, so a uid cannot be probed for existence.
func (s *Store) Get(ctx context.Context, ownerUID, uid string) (Notification, error) {
	return scanNotification(s.pool.QueryRow(ctx, getSQL, uid, ownerUID))
}

// markReadSQL stamps read_at once. COALESCE keeps the first reading, so marking
// an already-read notification again changes nothing.
const markReadSQL = `
UPDATE notifications n
SET read_at = COALESCE(n.read_at, now())
WHERE n.uid = $1 AND n.user_uid = $2
RETURNING ` + selectColumns

// MarkRead marks the notification uid of ownerUID read and returns it. It is
// idempotent: a notification already read keeps the moment it was first read.
// A missing or foreign uid is ErrNotFound.
func (s *Store) MarkRead(ctx context.Context, ownerUID, uid string) (Notification, error) {
	return scanNotification(s.pool.QueryRow(ctx, markReadSQL, uid, ownerUID))
}

// photosSQL reads the visible part of a frozen set, in its stored order. The
// three flags lift the archived, hidden and private filters respectively; the
// defaults (all false) drop every one of them.
const photosSQL = `
SELECT np.photo_uid
FROM notification_photos np
JOIN photos p ON p.uid = np.photo_uid
WHERE np.notification_uid = $1
  AND ($2 OR p.archived_at IS NULL)
  AND ($3 OR NOT p.hidden_from_library)
  AND ($4 OR NOT p.private)
ORDER BY np.position`

// Photos returns the photographs of ownerUID's notification uid that vis lets
// through, in the order the set was frozen in. It is the only way to read a set:
// a photograph archived, hidden or made private after the notification was sent
// is still in the set, and whether it may be shown depends on who is asking, so
// the caller states that in vis instead of receiving a raw list to render
// blindly. A deleted photograph is gone from the set already (the cascade).
//
// A notification whose whole set is gone — or that never had one — yields an
// empty, non-nil slice and no error. A missing or foreign uid is ErrNotFound.
func (s *Store) Photos(ctx context.Context, ownerUID, uid string, vis Visibility) ([]string, error) {
	if _, err := s.Get(ctx, ownerUID, uid); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, photosSQL, uid, vis.IncludeArchived, vis.IncludeHidden, vis.IncludePrivate)
	if err != nil {
		return nil, fmt.Errorf("notification: reading photos of %s: %w", uid, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("notification: reading photos of %s: %w", uid, err)
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// purgeSQL deletes the notifications past their retention: read ones recorded
// before $1, unread ones recorded before $2. The photo sets go with them (the
// cascade).
const purgeSQL = `
DELETE FROM notifications
WHERE (read_at IS NOT NULL AND created_at < $1)
   OR (read_at IS NULL AND created_at < $2)`

// Purge deletes, as of now, every read notification recorded more than r.Read
// ago and every unread one recorded more than r.Unread ago, and returns how many
// it deleted. Both ages count from when the notification was recorded, not from
// when it was read. A retention with a non-positive threshold, or an unread
// threshold shorter than the read one, is ErrInvalidRetention and deletes
// nothing.
func (s *Store) Purge(ctx context.Context, now time.Time, r Retention) (int64, error) {
	if err := r.validate(); err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, purgeSQL, now.Add(-r.Read), now.Add(-r.Unread))
	if err != nil {
		return 0, fmt.Errorf("notification: purging: %w", err)
	}
	return tag.RowsAffected(), nil
}

// inTx runs fn in a transaction, committing when it returns nil and rolling back
// otherwise. what names the operation in the wrapped errors.
func (s *Store) inTx(ctx context.Context, what string, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("notification: begin %s: %w", what, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("notification: commit %s: %w", what, err)
	}
	return nil
}

// mapForeignKey turns a foreign-key violation into the sentinel naming what was
// missing — a photograph or an account — and wraps anything else.
func mapForeignKey(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != foreignKeyViolation {
		return fmt.Errorf("notification: writing: %w", err)
	}
	switch pgErr.ConstraintName {
	case "notification_photos_photo_uid_fkey":
		return fmt.Errorf("%w: %s", ErrPhotoNotFound, pgErr.Detail)
	case "notifications_user_uid_fkey", "notification_prefs_user_uid_fkey":
		return fmt.Errorf("%w: %s", ErrUserNotFound, pgErr.Detail)
	default:
		return fmt.Errorf("notification: writing: %w", err)
	}
}
