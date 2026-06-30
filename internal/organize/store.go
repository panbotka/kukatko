package organize

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
	uniqueViolation = "23505"
	// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation.
	foreignKeyViolation = "23503"
)

// Store is the database access layer for albums, labels and per-user favorites.
// It owns no connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
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

// isForeignKeyViolation reports whether err is a PostgreSQL foreign-key
// violation and, if so, the name of the violated constraint.
func isForeignKeyViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		return pgErr.ConstraintName, true
	}
	return "", false
}

// translateUserPhotoFK maps a foreign-key violation from a write to a join table
// keyed by (user_uid, photo_uid) — user_favorites or user_ratings — to
// ErrPhotoNotFound or ErrUserNotFound by inspecting the violated constraint, and
// wraps any other error with op for context. The constraint name is matched on
// the referencing column ("photo_uid") rather than the table name, because both
// table-derived constraint names contain "user".
func translateUserPhotoFK(err error, op string) error {
	if name, ok := isForeignKeyViolation(err); ok {
		if strings.Contains(name, "photo_uid") {
			return ErrPhotoNotFound
		}
		return ErrUserNotFound
	}
	return fmt.Errorf("organize: %s: %w", op, err)
}
