package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// Store is the database access layer for subjects and markers. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
	// tags hears every tagging and untagging inside its transaction; nil hears
	// nothing. See WithTagObserver.
	tags TagObserver
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Tagging is one person put on (or taken off) one photo by a marker change.
type Tagging struct {
	// PhotoUID is the photo the marker is on.
	PhotoUID string
	// SubjectUID is the subject the marker was assigned to (or taken from).
	SubjectUID string
	// ActorUID is the account that made the change, or empty for a system
	// caller — a cluster assignment, a plain (unaudited) store call.
	ActorUID string
}

// TagObserver hears that a marker put a subject on a photo, or stopped doing
// so, inside the transaction that made the change — so whatever it records
// commits with the assignment or not at all. It is how the "you were tagged"
// notification (internal/tagnotifyjob) learns about every way a person gets
// tagged without each caller having to remember it: manual tagging, the review
// game, cluster assignment and candidate acceptance all end in this store.
//
// An error it returns fails the marker change. An observer that must never
// cost an assignment (a notification is worth less than the tag) contains its
// own failures, for instance under a savepoint.
type TagObserver interface {
	// Tagged hears that t.SubjectUID is now marked on t.PhotoUID.
	Tagged(ctx context.Context, tx pgx.Tx, t Tagging) error
	// Untagged hears that a marker of t.SubjectUID on t.PhotoUID was
	// unassigned or deleted. The subject may still be on the photo through
	// another marker; the observer checks.
	Untagged(ctx context.Context, tx pgx.Tx, t Tagging) error
}

// WithTagObserver returns a copy of s that reports every tagging and untagging
// to observer, within the transaction of the change. The copy shares the pool;
// s itself is unchanged.
func (s *Store) WithTagObserver(observer TagObserver) *Store {
	out := *s
	out.tags = observer
	return &out
}

// tagged reports t to the observer, if there is one.
func (s *Store) tagged(ctx context.Context, tx pgx.Tx, t Tagging) error {
	if s.tags == nil {
		return nil
	}
	if err := s.tags.Tagged(ctx, tx, t); err != nil {
		return fmt.Errorf("people: reporting the tagging of %s on %s: %w", t.SubjectUID, t.PhotoUID, err)
	}
	return nil
}

// untagged reports t to the observer, if there is one.
func (s *Store) untagged(ctx context.Context, tx pgx.Tx, t Tagging) error {
	if s.tags == nil {
		return nil
	}
	if err := s.tags.Untagged(ctx, tx, t); err != nil {
		return fmt.Errorf("people: reporting the untagging of %s on %s: %w", t.SubjectUID, t.PhotoUID, err)
	}
	return nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation and, if so, the name of the violated constraint.
func isUniqueViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return pgErr.ConstraintName, true
	}
	return "", false
}
