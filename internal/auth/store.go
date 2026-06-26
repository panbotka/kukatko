package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// uniqueViolation is the PostgreSQL SQLSTATE for a unique-constraint violation.
const uniqueViolation = "23505"

// Store is the database access layer for users and sessions. It owns no
// connection; it borrows the shared pgx pool supplied at construction.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by pool. The pool stays owned by the caller.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// userColumns is the canonical, ordered column list for user reads, matched by
// scanUser.
const userColumns = `uid, username, display_name, email, password_hash, role,
	disabled, created_at, updated_at, last_login_at`

// scanUser reads one user row in userColumns order from a pgx.Row (a single-row
// QueryRow result or a row during iteration), returning a wrapped error on
// failure.
func scanUser(row pgx.Row) (User, error) {
	var u User
	if err := row.Scan(
		&u.UID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &u.Role,
		&u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	); err != nil {
		return User{}, fmt.Errorf("auth: scanning user: %w", err)
	}
	return u, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

// CreateUser inserts u (its CreatedAt/UpdatedAt are assigned by the database
// defaults and not read back). It returns ErrUsernameTaken if the username
// already exists, or a wrapped error otherwise.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	const q = `INSERT INTO users (uid, username, display_name, email, password_hash, role, disabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := s.pool.Exec(ctx, q,
		u.UID, u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, u.Disabled)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUsernameTaken
		}
		return fmt.Errorf("auth: inserting user: %w", err)
	}
	return nil
}

// GetUserByUsername returns the user with the given username, or ErrUserNotFound.
func (s *Store) GetUserByUsername(ctx context.Context, username string) (User, error) {
	return s.getUser(ctx, "username", username)
}

// GetUserByUID returns the user with the given UID, or ErrUserNotFound.
func (s *Store) GetUserByUID(ctx context.Context, uid string) (User, error) {
	return s.getUser(ctx, "uid", uid)
}

// getUser fetches a single user filtered by an equality on the trusted column
// name col (an internal constant, never user input), translating pgx.ErrNoRows
// into ErrUserNotFound.
func (s *Store) getUser(ctx context.Context, col, val string) (User, error) {
	q := "SELECT " + userColumns + " FROM users WHERE " + col + " = $1"
	user, err := scanUser(s.pool.QueryRow(ctx, q, val))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return user, nil
}

// ListUsers returns all users ordered by username. The slice is empty (not nil)
// when there are no users.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	q := "SELECT " + userColumns + " FROM users ORDER BY username"
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("auth: querying users: %w", err)
	}
	defer rows.Close()

	users := make([]User, 0)
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterating users: %w", err)
	}
	return users, nil
}

// UpdateUserProfile updates the mutable profile fields of the user identified by
// uid and returns the refreshed user. It returns ErrUserNotFound if no such user
// exists. updated_at is bumped to now() by the statement.
func (s *Store) UpdateUserProfile(
	ctx context.Context, uid, displayName, email string, role Role, disabled bool,
) (User, error) {
	q := `UPDATE users SET display_name = $2, email = $3, role = $4, disabled = $5, updated_at = now()
		WHERE uid = $1 RETURNING ` + userColumns
	user, err := scanUser(s.pool.QueryRow(ctx, q, uid, displayName, email, role, disabled))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return user, nil
}

// SetUserDisabled flips the disabled flag for the user identified by uid, bumps
// updated_at, and returns the refreshed user. It returns ErrUserNotFound if no
// such user exists.
func (s *Store) SetUserDisabled(ctx context.Context, uid string, disabled bool) (User, error) {
	q := `UPDATE users SET disabled = $2, updated_at = now() WHERE uid = $1 RETURNING ` + userColumns
	user, err := scanUser(s.pool.QueryRow(ctx, q, uid, disabled))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return user, nil
}

// SetPasswordHash replaces the password hash for the user identified by uid and
// bumps updated_at. It returns ErrUserNotFound if no row was affected.
func (s *Store) SetPasswordHash(ctx context.Context, uid, hash string) error {
	const q = `UPDATE users SET password_hash = $2, updated_at = now() WHERE uid = $1`
	tag, err := s.pool.Exec(ctx, q, uid, hash)
	if err != nil {
		return fmt.Errorf("auth: updating password hash: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetLastLogin records a successful login time for the user identified by uid.
// A missing user is not treated as an error here (the caller has just
// authenticated the user), but query failures are returned wrapped.
func (s *Store) SetLastLogin(ctx context.Context, uid string, at time.Time) error {
	const q = `UPDATE users SET last_login_at = $2 WHERE uid = $1`
	if _, err := s.pool.Exec(ctx, q, uid, at); err != nil {
		return fmt.Errorf("auth: updating last_login_at: %w", err)
	}
	return nil
}

// CountUsers returns the total number of user rows, used to decide whether the
// bootstrap admin should be created.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: counting users: %w", err)
	}
	return n, nil
}
