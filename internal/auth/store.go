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

// foreignKeyViolation is the PostgreSQL SQLSTATE for a foreign-key violation.
const foreignKeyViolation = "23503"

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
// scanUser. It is valid in both a SELECT list and a RETURNING clause. note is
// nullable in the schema, so it is coalesced to the empty string here and the Go
// model can keep it a plain string.
const userColumns = `uid, username, display_name, email, password_hash, role,
	disabled, created_at, updated_at, last_login_at, COALESCE(note, '') AS note,
	subject_uid, approved_at, welcome_seen_at`

// scanUser reads one user row in userColumns order from a pgx.Row (a single-row
// QueryRow result or a row during iteration), returning a wrapped error on
// failure.
func scanUser(row pgx.Row) (User, error) {
	var u User
	if err := row.Scan(
		&u.UID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &u.Role,
		&u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Note, &u.SubjectUID,
		&u.ApprovedAt, &u.WelcomeSeenAt,
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

// isForeignKeyViolation reports whether err is a PostgreSQL foreign-key
// violation (SQLSTATE 23503). The only foreign key a user row carries is
// subject_uid, so it always means the named person does not exist.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation
}

// normalizeSubjectUID maps an empty-string link to nil, so a client that clears
// the field by sending "" means the same thing as one that sends null: no link.
// A stored empty string would be a UID that matches no subject, which the
// foreign key rejects anyway — turning it into "no link" is the only reading
// that is not an error message about a field the user just emptied.
func normalizeSubjectUID(uid *string) *string {
	if uid != nil && *uid == "" {
		return nil
	}
	return uid
}

// insertUserQuery inserts one account. approved_at is written from the model
// rather than defaulted in the schema: a row that says nothing about approval is
// an account still waiting for one, and that has to stay the answer once
// self-service registration inserts rows of its own. welcome_seen_at is
// deliberately absent — a brand new account has never seen the welcome, and the
// column's NULL says so.
const insertUserQuery = `INSERT INTO users
		(uid, username, display_name, email, password_hash, role, disabled, note, subject_uid,
		 approved_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

// insertUserArgs returns the arguments of insertUserQuery in its parameter
// order, so the plain and audited insert paths cannot drift apart.
func insertUserArgs(u User) []any {
	return []any{
		u.UID, u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, u.Disabled, u.Note,
		normalizeSubjectUID(u.SubjectUID), u.ApprovedAt,
	}
}

// CreateUser inserts u (its CreatedAt/UpdatedAt are assigned by the database
// defaults and not read back). It returns ErrUsernameTaken if the username
// already exists, ErrSubjectNotFound if u.SubjectUID names no subject, or a
// wrapped error otherwise.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	_, err := s.pool.Exec(ctx, insertUserQuery, insertUserArgs(u)...)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUsernameTaken
		}
		if isForeignKeyViolation(err) {
			return ErrSubjectNotFound
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

// UserFilter narrows a user listing. Its zero value selects every account, which
// is what the administration page asks for by default.
type UserFilter struct {
	// Pending selects by approval state: true lists only the accounts waiting
	// for an administrator (approved_at IS NULL), false only the ones already
	// let in, and nil — the usual case — everybody.
	Pending *bool
}

// listUsersQuery lists the accounts a UserFilter selects, ordered by username.
// The approval filter is expressed as a comparison against the parameter rather
// than as two statements, so a NULL parameter (no filter) selects every row and
// the listing has exactly one query.
const listUsersQuery = `SELECT ` + userColumns + ` FROM users
	WHERE $1::boolean IS NULL OR (approved_at IS NULL) = $1
	ORDER BY username`

// ListUsers returns the users matching filter, ordered by username. The slice is
// empty (not nil) when nothing matches.
func (s *Store) ListUsers(ctx context.Context, filter UserFilter) ([]User, error) {
	rows, err := s.pool.Query(ctx, listUsersQuery, filter.Pending)
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

// updateUserProfileQuery replaces the mutable profile fields of one user and
// returns the refreshed row. The note is a partial update: a nil argument leaves
// the stored note untouched, while a non-nil one (including a pointer to "")
// overwrites it. COALESCE keeps that branch in SQL, so a nil note needs no
// separate statement.
const updateUserProfileQuery = `UPDATE users SET display_name = $2, email = $3, role = $4, disabled = $5,
		note = COALESCE($6::text, note), subject_uid = $7, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// setUserSubjectQuery points one account at a person of the library, or clears
// the link when the argument is NULL, and returns the refreshed row.
//
// It leaves updated_at alone on purpose. The user-administration screens read
// that column as "an administrator edited this profile", and somebody saying who
// they are on their own account page is not that.
const setUserSubjectQuery = `UPDATE users SET subject_uid = $2
	WHERE uid = $1 RETURNING ` + userColumns

// setUserDisabledQuery flips the disabled flag of one user and returns the
// refreshed row.
const setUserDisabledQuery = `UPDATE users SET disabled = $2, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// UpdateUserProfile updates the mutable profile fields of the user identified by
// uid and returns the refreshed user. It returns ErrUserNotFound if no such user
// exists, or ErrLastMaintainer when the change would leave the instance without a
// single enabled maintainer (see withMaintainerGuard). updated_at is bumped to
// now() by the statement.
func (s *Store) UpdateUserProfile(ctx context.Context, uid string, in UpdateUserInput) (User, error) {
	return s.updateUserReturningGuarded(ctx, updateUserProfileQuery,
		uid, in.DisplayName, in.Email, in.Role, in.Disabled, in.Note,
		normalizeSubjectUID(in.SubjectUID))
}

// SetUserSubject points the account identified by uid at the subject named by
// subjectUID, or clears the link when subjectUID is nil (or empty). It is the
// self-service write behind the account page and returns the refreshed user,
// ErrUserNotFound if no such account exists, or ErrSubjectNotFound when the UID
// names no person in the library.
//
// It is deliberately outside the maintainer guard: the link carries no
// permission and cannot strand the instance.
func (s *Store) SetUserSubject(ctx context.Context, uid string, subjectUID *string) (User, error) {
	user, err := scanUser(s.pool.QueryRow(ctx, setUserSubjectQuery, uid, normalizeSubjectUID(subjectUID)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		if isForeignKeyViolation(err) {
			return User{}, ErrSubjectNotFound
		}
		return User{}, err
	}
	return user, nil
}

// SetUserDisabled flips the disabled flag for the user identified by uid, bumps
// updated_at, and returns the refreshed user. It returns ErrUserNotFound if no
// such user exists, or ErrLastMaintainer when disabling would leave the instance
// without a single enabled maintainer (see withMaintainerGuard).
func (s *Store) SetUserDisabled(ctx context.Context, uid string, disabled bool) (User, error) {
	return s.updateUserReturningGuarded(ctx, setUserDisabledQuery, uid, disabled)
}

// markWelcomeSeenQuery records that the account has seen the first-run welcome
// and returns the refreshed row.
//
// COALESCE is what makes it idempotent: the stamp is written only while the
// column is still NULL, so a second call — a second tab, a retried request, a
// reload of the page that sends it — leaves the first time in place and can
// never move it backwards.
//
// Like setUserSubjectQuery it leaves updated_at alone. The user-administration
// screens read that column as "an administrator edited this profile", and
// somebody closing their own welcome is not that.
const markWelcomeSeenQuery = `UPDATE users SET welcome_seen_at = COALESCE(welcome_seen_at, $2)
	WHERE uid = $1 RETURNING ` + userColumns

// MarkWelcomeSeen stamps at into welcome_seen_at for the account identified by
// uid unless it already holds a time, and returns the refreshed user. It returns
// ErrUserNotFound when no such account exists.
func (s *Store) MarkWelcomeSeen(ctx context.Context, uid string, at time.Time) (User, error) {
	user, err := scanUser(s.pool.QueryRow(ctx, markWelcomeSeenQuery, uid, at))
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
