package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/panbotka/kukatko/internal/project"
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
//
// The last column is the account's role in the bootstrap project, read by a
// correlated subquery so every user read — SELECT … FROM users and UPDATE users
// … RETURNING alike, which is why the table is never aliased — carries it without
// a join (transition: until the users.* readers move to the membership). A
// RETURNING clause sees the membership as it was before the statement, so a
// statement that also writes the membership patches the result in Go.
const userColumns = `uid, username, display_name, email, password_hash, role,
	disabled, created_at, updated_at, last_login_at, COALESCE(note, '') AS note,
	subject_uid, approved_at, welcome_seen_at,
	COALESCE((SELECT m.role FROM project_members m
		WHERE m.user_uid = users.uid AND m.project_uid = '` + project.BootstrapUID + `'), '') AS project_role`

// lockUserQuery reads one account and locks its row for the rest of the
// transaction, so the decision that follows — is it blocked? has somebody
// already approved it? has this link been spent? — is made on a row nobody else
// can change underneath it. Two administrators clicking at the same moment
// therefore queue, and the second one sees what the first one wrote.
const lockUserQuery = `SELECT ` + userColumns + ` FROM users WHERE uid = $1 FOR UPDATE`

// lockUser reads the account identified by uid on tx and locks its row (see
// lockUserQuery). A missing account is ErrUserNotFound.
func lockUser(ctx context.Context, tx pgx.Tx, uid string) (User, error) {
	user, err := scanUser(tx.QueryRow(ctx, lockUserQuery, uid))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, err
	}
	return user, nil
}

// scanUser reads one user row in userColumns order from a pgx.Row (a single-row
// QueryRow result or a row during iteration), returning a wrapped error on
// failure.
func scanUser(row pgx.Row) (User, error) {
	var u User
	if err := row.Scan(
		&u.UID, &u.Username, &u.DisplayName, &u.Email, &u.PasswordHash, &u.Role,
		&u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Note, &u.SubjectUID,
		&u.ApprovedAt, &u.WelcomeSeenAt, &u.ProjectRole,
	); err != nil {
		return User{}, fmt.Errorf("auth: scanning user: %w", err)
	}
	u.AccountRole = accountRoleOf(u.Role, u.ProjectRole)
	return u, nil
}

// withProjectRole returns u with its bootstrap project role replaced by role
// and the derived AccountRole recomputed — for a statement that wrote the
// membership in the same statement, whose RETURNING still saw the old one.
func withProjectRole(u User, role project.Role) User {
	u.ProjectRole = role
	u.AccountRole = accountRoleOf(u.Role, role)
	return u
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
//
// The same statement makes the account a member of the project $11 with the
// role $12 — approved exactly when the account is — unless $12 is empty, which
// creates an account with no membership at all.
const insertUserQuery = `WITH new_user AS (
	INSERT INTO users
		(uid, username, display_name, email, password_hash, role, disabled, note, subject_uid,
		 approved_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	RETURNING uid, approved_at
)
INSERT INTO project_members (project_uid, user_uid, role, approved_at)
SELECT $11::text, uid, $12::text, approved_at FROM new_user WHERE $12::text <> ''`

// insertUserArgs returns the arguments of insertUserQuery in its parameter
// order, so the plain and audited insert paths cannot drift apart. The
// membership goes into the bootstrap project (transition: the project the
// account is created in will come from the request).
func insertUserArgs(u User) []any {
	return []any{
		u.UID, u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, u.Disabled, u.Note,
		normalizeSubjectUID(u.SubjectUID), u.ApprovedAt, project.BootstrapUID, string(u.ProjectRole),
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

// GetUserByUIDOrUsername returns the account named by ref, which may be either
// an account UID or a username — the two ways a human refers to an account, and
// a reader filtering by account (the audit trail) has to accept both. The UID is
// tried first, so a UID always names the account it belongs to whatever
// usernames exist. It returns ErrUserNotFound when ref names neither.
func (s *Store) GetUserByUIDOrUsername(ctx context.Context, ref string) (User, error) {
	user, err := s.GetUserByUID(ctx, ref)
	if err == nil || !errors.Is(err, ErrUserNotFound) {
		return user, err
	}
	return s.GetUserByUsername(ctx, ref)
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
//
// $4 is the instance role; the same statement sets the account's role in the
// project $8 to $9, creating the membership (approved exactly when the account
// is) when there is none. RETURNING still sees the old membership — callers
// patch the result with withProjectRole.
const updateUserProfileQuery = `WITH member AS (
	INSERT INTO project_members (project_uid, user_uid, role, approved_at)
	SELECT $8::text, uid, $9::text, approved_at FROM users WHERE uid = $1
	ON CONFLICT (project_uid, user_uid) DO UPDATE SET role = EXCLUDED.role
)
UPDATE users SET display_name = $2, email = $3, role = $4, disabled = $5,
		note = COALESCE($6::text, note), subject_uid = $7, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// updateUserProfileArgs returns the arguments of updateUserProfileQuery in its
// parameter order, so the plain and audited update paths cannot drift apart.
func updateUserProfileArgs(uid string, in UpdateUserInput) []any {
	instance, inProject := in.Role.Split()
	return []any{
		uid, in.DisplayName, in.Email, instance, in.Disabled, in.Note,
		normalizeSubjectUID(in.SubjectUID), project.BootstrapUID, string(inProject),
	}
}

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
	user, err := s.updateUserReturningGuarded(ctx, updateUserProfileQuery, updateUserProfileArgs(uid, in)...)
	if err != nil {
		return User{}, err
	}
	_, inProject := in.Role.Split()
	return withProjectRole(user, inProject), nil
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

// lastProjectSlugQuery reads the slug of the project the account last worked
// in; an account with none remembered (or whose project is gone) yields no row.
const lastProjectSlugQuery = `SELECT p.slug FROM users u JOIN projects p ON p.uid = u.last_project_uid
	WHERE u.uid = $1`

// LastProjectSlug returns the slug of the project the account uid last worked
// in, or "" when none is remembered. It returns a wrapped error on failure.
func (s *Store) LastProjectSlug(ctx context.Context, uid string) (string, error) {
	var slug string
	err := s.pool.QueryRow(ctx, lastProjectSlugQuery, uid).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("auth: reading the last project of %s: %w", uid, err)
	}
	return slug, nil
}
