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
//
// It is the account record and nothing project-shaped: an account's role and
// linked person in a project live on that membership (internal/project). The
// one derived column is the account's approval (accountApprovedSQL), read off
// the memberships by a correlated subquery so every user read — SELECT … FROM
// users and UPDATE users … RETURNING alike, which is why the table is never
// aliased — carries it without a join.
const userColumns = `uid, username, display_name, email, password_hash, role,
	disabled, created_at, updated_at, last_login_at, COALESCE(note, '') AS note,
	` + accountApprovedSQL + ` AS approved_at, welcome_seen_at`

// accountApprovedSQL is when the account (the users row in scope, never
// aliased) was let in: the earliest approval of any of its memberships. While
// it holds memberships and none is approved it is NULL — "registered,
// waiting", which sign-in refuses. A maintainer enters every project whatever
// its memberships say, and an account in no project at all is waiting for
// nobody (it may sign in, see nothing and accept an invitation), so both count
// as approved since they were created.
const accountApprovedSQL = `COALESCE(
		(SELECT min(m.approved_at) FROM project_members m WHERE m.user_uid = users.uid),
		CASE WHEN users.role = '` + string(RoleMaintainer) + `'
			OR NOT EXISTS (SELECT 1 FROM project_members m WHERE m.user_uid = users.uid)
		THEN users.created_at END)`

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
		&u.Disabled, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt, &u.Note,
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
// violation (SQLSTATE 23503). The only foreign key an account write can trip is
// the membership's subject_uid, so it always means the named person does not
// exist (checkSubject refuses that up front; this covers a subject deleted in
// between).
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation
}

// querier is the one method checkSubject needs, satisfied by the pool and by a
// transaction alike.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// execQuerier is a querier that also executes statements — the pool or a
// transaction, whichever an account insert runs on.
type execQuerier interface {
	querier
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// subjectInProjectQuery reports whether the subject $1 is a person of the
// project $2.
const subjectInProjectQuery = `SELECT EXISTS (SELECT 1 FROM subjects WHERE uid = $1 AND project_id = $2)`

// checkSubject returns ErrSubjectNotFound unless subjectUID (nil or empty: no
// link, always fine) names a person of the project projectUID. A person is a
// row of one project, so another project's subject is nobody there — the same
// answer as a uid that names nothing at all.
func checkSubject(ctx context.Context, q querier, projectUID string, subjectUID *string) error {
	uid := normalizeSubjectUID(subjectUID)
	if uid == nil {
		return nil
	}
	var ok bool
	if err := q.QueryRow(ctx, subjectInProjectQuery, *uid, projectUID).Scan(&ok); err != nil {
		return fmt.Errorf("auth: checking subject %s: %w", *uid, err)
	}
	if !ok {
		return ErrSubjectNotFound
	}
	return nil
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

// insertUserQuery inserts one account and makes it a member of the project $11
// with the role $12 — unless $12 is empty, which creates an account with no
// membership at all. The membership is approved at $10 (from the model, never
// a schema default: a row that says nothing about approval is an account still
// waiting for one, which is what self-service registration inserts) and links
// the person $9, which checkSubject has confirmed is one of that project's.
// welcome_seen_at is deliberately absent — a brand new account has never seen
// the welcome, and the column's NULL says so.
const insertUserQuery = `WITH new_user AS (
	INSERT INTO users (uid, username, display_name, email, password_hash, role, disabled, note)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING uid
)
INSERT INTO project_members (project_uid, user_uid, role, approved_at, subject_uid)
SELECT $11::text, uid, $12::text, $10, $9 FROM new_user WHERE $12::text <> ''`

// insertUserArgs returns the arguments of insertUserQuery in its parameter
// order, so the plain and audited insert paths cannot drift apart. The
// membership, if any, is u.join.
func insertUserArgs(u User) []any {
	return []any{
		u.UID, u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, u.Disabled, u.Note,
		normalizeSubjectUID(u.join.subject), u.ApprovedAt, u.join.project, string(u.join.role),
	}
}

// insertUser runs insertUserQuery for u on q, refusing a linked person who is
// not one of the joined project's first. An account created without a
// membership (u.join.role empty) links nobody. It returns ErrUsernameTaken for a
// duplicate username, ErrSubjectNotFound for a person who is nobody there, or a
// wrapped error.
func insertUser(ctx context.Context, q execQuerier, u User) error {
	if u.join.role != "" {
		if err := checkSubject(ctx, q, u.join.project, u.join.subject); err != nil {
			return err
		}
	}
	_, err := q.Exec(ctx, insertUserQuery, insertUserArgs(u)...)
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

// CreateUser inserts u (its CreatedAt/UpdatedAt are assigned by the database
// defaults and not read back). It returns ErrUsernameTaken if the username
// already exists, ErrSubjectNotFound if the membership's linked person is no
// person of the joined project, or a wrapped error otherwise.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	return insertUser(ctx, s.pool, u)
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
	// for an administrator (no approved membership, see accountApprovedSQL),
	// false only the ones already let in, and nil — the usual case — everybody.
	Pending *bool
}

// listUsersQuery lists the accounts a UserFilter selects, ordered by username.
// The approval filter is expressed as a comparison against the parameter rather
// than as two statements, so a NULL parameter (no filter) selects every row and
// the listing has exactly one query.
const listUsersQuery = `SELECT ` + userColumns + ` FROM users
	WHERE $1::boolean IS NULL OR (` + accountApprovedSQL + ` IS NULL) = $1
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

// updateUserProfileQuery patches the mutable profile fields of one user — the
// account record only; it never touches a membership — and returns the
// refreshed row. Every field is optional: a NULL argument keeps what the row
// holds, read under the lock the UPDATE takes, so an edit that names only the
// display name cannot write back a stale disabled flag or instance role over a
// change another administrator committed meanwhile. A non-NULL argument
// (including "" for the note) overwrites. COALESCE keeps that branch in SQL,
// so every combination is one statement. $4 is the instance role.
const updateUserProfileQuery = `UPDATE users SET display_name = COALESCE($2::text, display_name),
		email = COALESCE($3::text, email), role = COALESCE($4::text, role),
		disabled = COALESCE($5::boolean, disabled), note = COALESCE($6::text, note), updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// profileUpdate returns the guarded mutation that applies the patch in to uid's
// profile.
func profileUpdate(ctx context.Context, uid string, in UpdateUserInput) func(pgx.Tx) (User, error) {
	var role *string
	if in.InstanceRole != nil {
		role = new(string(*in.InstanceRole))
	}
	return func(tx pgx.Tx) (User, error) {
		return scanUpdatedUser(ctx, tx, updateUserProfileQuery,
			uid, in.DisplayName, in.Email, role, in.Disabled, in.Note)
	}
}

// setMemberSubjectQuery points the account $1's membership of the project $3
// at the person $2, or clears the link when $2 is NULL, and reports the
// membership it wrote — none when the account is not a member there.
const setMemberSubjectQuery = `UPDATE project_members SET subject_uid = $2
	WHERE user_uid = $1 AND project_uid = $3 RETURNING subject_uid`

// setUserDisabledQuery flips the disabled flag of one user and returns the
// refreshed row.
const setUserDisabledQuery = `UPDATE users SET disabled = $2, updated_at = now()
	WHERE uid = $1 RETURNING ` + userColumns

// UpdateUserProfile applies the patch in to the user identified by uid (see
// updateUserProfileQuery) and returns the refreshed user; an empty patch writes
// nothing and returns the account as it is. It returns ErrUserNotFound if no such user
// exists, or ErrLastMaintainer when the change would leave the
// instance without a single enabled maintainer (see withMaintainerGuard).
// updated_at is bumped to now() by the statement.
func (s *Store) UpdateUserProfile(ctx context.Context, uid string, in UpdateUserInput) (User, error) {
	if in.empty() {
		return s.GetUserByUID(ctx, uid)
	}
	return s.updateUserGuarded(ctx, profileUpdate(ctx, uid, in))
}

// SetUserSubject points the account identified by uid at the subject named by
// subjectUID in the project projectUID, or clears the link when subjectUID is
// nil (or empty). An empty projectUID is ErrNoProject: there is no default
// project. The link lives on the membership: who you
// are is a fact about one project's people. It is the self-service write
// behind the account page and returns the account with the link of that
// project — nil when the account is no member there (a maintainer
// visiting), where nothing is written — ErrUserNotFound if no such account
// exists, or ErrSubjectNotFound when the UID names no person of the project.
//
// It is deliberately outside the maintainer guard: the link carries no
// permission and cannot strand the instance. It leaves updated_at alone on
// purpose: the user-administration screens read that column as "an
// administrator edited this profile", and somebody saying who they are on
// their own account page is not that.
func (s *Store) SetUserSubject(ctx context.Context, projectUID, uid string, subjectUID *string) (LinkedUser, error) {
	if projectUID == "" {
		return LinkedUser{}, ErrNoProject
	}
	subjectUID = normalizeSubjectUID(subjectUID)
	user, err := s.GetUserByUID(ctx, uid)
	if err != nil {
		return LinkedUser{}, err
	}
	if err := checkSubject(ctx, s.pool, projectUID, subjectUID); err != nil {
		return LinkedUser{}, err
	}
	var written *string
	err = s.pool.QueryRow(ctx, setMemberSubjectQuery, uid, subjectUID, projectUID).Scan(&written)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		written = nil
	case isForeignKeyViolation(err):
		return LinkedUser{}, ErrSubjectNotFound
	case err != nil:
		return LinkedUser{}, fmt.Errorf("auth: linking %s to a person: %w", uid, err)
	}
	return LinkedUser{User: user, SubjectUID: written}, nil
}

// SetUserDisabled flips the disabled flag for the user identified by uid, bumps
// updated_at, and returns the refreshed user. It returns ErrUserNotFound if no
// such user exists, or ErrLastMaintainer when disabling would leave the instance
// without a single enabled maintainer (see withMaintainerGuard).
func (s *Store) SetUserDisabled(ctx context.Context, uid string, disabled bool) (User, error) {
	return s.updateUserGuarded(ctx, func(tx pgx.Tx) (User, error) {
		return scanUpdatedUser(ctx, tx, setUserDisabledQuery, uid, disabled)
	})
}

// markWelcomeSeenQuery records that the account has seen the first-run welcome
// and returns the refreshed row.
//
// COALESCE is what makes it idempotent: the stamp is written only while the
// column is still NULL, so a second call — a second tab, a retried request, a
// reload of the page that sends it — leaves the first time in place and can
// never move it backwards.
//
// Like SetUserSubject it leaves updated_at alone. The user-administration
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
// in, as long as the account can still enter it — the rule enterProject
// applies: a maintainer enters every project, anybody else only an active one
// it holds an approved membership of. An account with none remembered, whose
// project is gone, or that has since been removed, left waiting or locked out
// by a deactivation yields no row, so it is never pointed at a project that
// would answer 404 (and keeps no record of a project it no longer belongs to).
const lastProjectSlugQuery = `SELECT p.slug FROM users u JOIN projects p ON p.uid = u.last_project_uid
	WHERE u.uid = $1 AND (u.role = '` + string(RoleMaintainer) + `' OR (p.active AND EXISTS (
		SELECT 1 FROM project_members m
		WHERE m.project_uid = p.uid AND m.user_uid = u.uid AND m.approved_at IS NOT NULL)))`

// LastProjectSlug returns the slug of the project the account uid last worked
// in, or "" when none is remembered or the account can no longer enter it (see
// lastProjectSlugQuery). It returns a wrapped error on failure.
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
