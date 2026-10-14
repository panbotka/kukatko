package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/panbotka/kukatko/internal/audit"
)

// foreignKeyViolation is the PostgreSQL SQLSTATE of a foreign-key violation.
const foreignKeyViolation = "23503"

// memberColumns is the column list every membership read scans, in scanMember's
// order; it reads project_members as m joined with users as u.
const memberColumns = `m.project_uid, m.user_uid, m.role, m.approved_at, m.subject_uid,
	m.last_seen_at, m.visit_reference_at, m.task_digest_at, m.created_at,
	u.username, u.display_name, u.last_login_at,
	(m.subject_uid IS NOT NULL OR EXISTS (SELECT 1 FROM user_pictures up WHERE up.user_uid = u.uid))`

// memberFrom joins a membership with its account.
const memberFrom = ` FROM project_members m JOIN users u ON u.uid = m.user_uid`

// scanMember reads one membership row, in the order of memberColumns.
func scanMember(row pgx.Row) (*Member, error) {
	var m Member
	if err := row.Scan(&m.ProjectUID, &m.UserUID, &m.Role, &m.ApprovedAt, &m.SubjectUID,
		&m.LastSeenAt, &m.VisitReferenceAt, &m.TaskDigestAt, &m.CreatedAt,
		&m.Username, &m.DisplayName, &m.LastLoginAt, &m.HasPicture); err != nil {
		return nil, fmt.Errorf("scanning project member: %w", err)
	}
	return &m, nil
}

// membersSQL lists one project's members by display name; an account without
// one sorts by its username, and the uid keeps equal names in a stable order.
const membersSQL = `SELECT ` + memberColumns + memberFrom + `
WHERE m.project_uid = $1
ORDER BY COALESCE(NULLIF(u.display_name, ''), u.username), u.username, m.user_uid`

// Members returns every member of the project projectUID ordered by display
// name (the username standing in for an empty one). It returns ErrNotFound for
// an unknown project; a project without members yields an empty slice.
func (s *Store) Members(ctx context.Context, projectUID string) ([]Member, error) {
	if _, err := s.Get(ctx, projectUID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, membersSQL, projectUID)
	if err != nil {
		return nil, fmt.Errorf("project: listing members of %s: %w", projectUID, err)
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		m, err := scanMember(rows)
		if err != nil {
			return nil, fmt.Errorf("project: %w", err)
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: iterating members of %s: %w", projectUID, err)
	}
	return out, nil
}

// membershipSQL reads one membership.
const membershipSQL = `SELECT ` + memberColumns + memberFrom + `
WHERE m.project_uid = $1 AND m.user_uid = $2`

// Membership returns the membership of the account userUID in the project
// projectUID, or ErrNotMember when there is none (an unknown project or account
// included: neither has a membership).
func (s *Store) Membership(ctx context.Context, projectUID, userUID string) (*Member, error) {
	m, err := scanMember(s.pool.QueryRow(ctx, membershipSQL, projectUID, userUID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotMember
	}
	if err != nil {
		return nil, fmt.Errorf("project: reading membership of %s in %s: %w", userUID, projectUID, err)
	}
	return m, nil
}

// membershipsOfSQL lists the projects one account belongs to, by project name.
const membershipsOfSQL = `
SELECT p.uid, p.slug, p.name, p.active, m.role, m.approved_at IS NOT NULL, m.subject_uid
FROM project_members m JOIN projects p ON p.uid = m.project_uid
WHERE m.user_uid = $1
ORDER BY p.name, p.uid`

// MembershipsOf returns every project the account userUID belongs to, ordered by
// project name, with its role there and whether it is approved — inactive
// projects included, so the caller decides how to show them — and the person it
// says it is there. An account without
// memberships (or an unknown one) yields an empty slice.
func (s *Store) MembershipsOf(ctx context.Context, userUID string) ([]MembershipSummary, error) {
	rows, err := s.pool.Query(ctx, membershipsOfSQL, userUID)
	if err != nil {
		return nil, fmt.Errorf("project: listing memberships of %s: %w", userUID, err)
	}
	defer rows.Close()

	out := []MembershipSummary{}
	for rows.Next() {
		var ms MembershipSummary
		err := rows.Scan(&ms.ProjectUID, &ms.Slug, &ms.Name, &ms.Active, &ms.Role, &ms.Approved, &ms.SubjectUID)
		if err != nil {
			return nil, fmt.Errorf("project: scanning membership of %s: %w", userUID, err)
		}
		out = append(out, ms)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project: iterating memberships of %s: %w", userUID, err)
	}
	return out, nil
}

// addMemberSQL inserts a membership unless one exists; RETURNING yields no row
// for an existing one, which leaves it untouched.
const addMemberSQL = `
INSERT INTO project_members (project_uid, user_uid, role, approved_at)
VALUES ($1, $2, $3, CASE WHEN $4::boolean THEN now() END)
ON CONFLICT (project_uid, user_uid) DO NOTHING
RETURNING user_uid`

// AddMember makes the account userUID a member of the project projectUID with
// role, approved at once when approved is true and waiting for approval
// otherwise, and records ActionProjectMemberAdd in the same transaction. An
// existing membership is left exactly as it is and ErrAlreadyMember returned (no
// audit entry). It returns ErrInvalidRole for a bad role, ErrNotFound for an
// unknown project and ErrUserNotFound for an unknown account.
func (s *Store) AddMember(
	ctx context.Context, meta audit.Meta, projectUID, userUID string, role Role, approved bool,
) (*Member, error) {
	if !role.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	entry := projectEntry(meta, projectUID, audit.ActionProjectMemberAdd, MemberTargetType, userUID, map[string]any{
		"project_uid": projectUID, "role": string(role), "approved": approved,
	})
	var added *Member
	err := s.inTx(ctx, entry, func(tx pgx.Tx) error {
		if err := lockProject(ctx, tx, projectUID); err != nil {
			return err
		}
		var uid string
		err := tx.QueryRow(ctx, addMemberSQL, projectUID, userUID, string(role), approved).Scan(&uid)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyMember
		}
		if err != nil {
			return fmt.Errorf("inserting membership: %w", err)
		}
		added, err = lockMember(ctx, tx, projectUID, userUID)
		return err
	})
	if err != nil {
		return nil, wrapMemberErr("adding", projectUID, userUID, err)
	}
	return added, nil
}

// SetRole changes the role of the member userUID in the project projectUID and
// records ActionProjectMemberRole (new and previous role) in the same
// transaction. Setting the role the member already has changes nothing and
// records nothing. It returns ErrInvalidRole for a bad role, ErrNotFound for an
// unknown project, ErrNotMember when the account is not a member and
// ErrLastAdmin when the member is the project's only admin and role is not admin.
func (s *Store) SetRole(ctx context.Context, meta audit.Meta, projectUID, userUID string, role Role) (*Member, error) {
	if !role.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}
	entry := projectEntry(meta, projectUID, audit.ActionProjectMemberRole, MemberTargetType, userUID, map[string]any{
		"project_uid": projectUID, "role": string(role),
	})
	return s.mutateMember(ctx, entry, projectUID, userUID, func(tx pgx.Tx, current *Member) (bool, error) {
		if current.Role == role {
			return false, nil
		}
		if current.Role.IsAdmin() && !role.IsAdmin() {
			if err := ensureAnotherAdmin(ctx, tx, projectUID); err != nil {
				return false, err
			}
		}
		entry.Details["previous_role"] = string(current.Role)
		return true, execMember(ctx, tx,
			`UPDATE project_members SET role = $3 WHERE project_uid = $1 AND user_uid = $2`,
			projectUID, userUID, string(role))
	})
}

// Approve marks the member userUID of the project projectUID approved now and
// records ActionProjectMemberApprove in the same transaction. Approving an
// already approved member keeps the original approval time and records nothing.
// It returns ErrNotFound for an unknown project and ErrNotMember when the
// account is not a member. It is ApproveWith without a hook.
func (s *Store) Approve(ctx context.Context, meta audit.Meta, projectUID, userUID string) (*Member, error) {
	return s.ApproveWith(ctx, meta, projectUID, userUID, nil)
}

// ApproveHook runs inside an approval's transaction, after the membership is
// stamped and before the audit entry is written, with the member as it was
// before the approval. It is how the "you may come in" mail is scheduled if
// and only if the approval commits; an error rolls the approval back.
type ApproveHook func(ctx context.Context, tx pgx.Tx, member *Member) error

// ApproveWith approves the member userUID of the project projectUID like
// Approve and, when the approval really changes something, runs hook (nil: no
// hook) on the same transaction. Approving an already approved member runs no
// hook. The account's own approval is derived from its memberships (sign-in
// lets an account in once any membership is approved), so nothing else is
// stamped.
func (s *Store) ApproveWith(
	ctx context.Context, meta audit.Meta, projectUID, userUID string, hook ApproveHook,
) (*Member, error) {
	entry := projectEntry(meta, projectUID, audit.ActionProjectMemberApprove, MemberTargetType, userUID, map[string]any{
		"project_uid": projectUID,
	})
	return s.mutateMember(ctx, entry, projectUID, userUID, func(tx pgx.Tx, current *Member) (bool, error) {
		if current.ApprovedAt != nil {
			return false, nil
		}
		entry.Details["role"] = string(current.Role)
		if err := execMember(ctx, tx,
			`UPDATE project_members SET approved_at = now() WHERE project_uid = $1 AND user_uid = $2`,
			projectUID, userUID); err != nil {
			return false, err
		}
		if hook == nil {
			return true, nil
		}
		return true, hook(ctx, tx, current)
	})
}

// SetSubject sets the person the member userUID says it is in the project
// projectUID ("this is me") to subjectUID, or clears it when subjectUID is nil,
// and records ActionProjectMemberSubject (previous and new subject) in the same
// transaction. Setting the subject the member already has records nothing. It
// returns ErrNotFound for an unknown project, ErrNotMember when the account is
// not a member and ErrSubjectNotFound — writing nothing — unless subjectUID
// names a person of projectUID: another project's subject is nobody here, the
// same answer as a uid that names nothing (the rule internal/auth's
// checkSubject applies on the self-service path).
func (s *Store) SetSubject(
	ctx context.Context, meta audit.Meta, projectUID, userUID string, subjectUID *string,
) (*Member, error) {
	entry := projectEntry(meta, projectUID, audit.ActionProjectMemberSubject, MemberTargetType, userUID, map[string]any{
		"project_uid": projectUID, "subject_uid": subjectUID,
	})
	return s.mutateMember(ctx, entry, projectUID, userUID, func(tx pgx.Tx, current *Member) (bool, error) {
		if sameSubject(current.SubjectUID, subjectUID) {
			return false, nil
		}
		if err := requireSubjectInProject(ctx, tx, projectUID, subjectUID); err != nil {
			return false, err
		}
		entry.Details["role"] = string(current.Role)
		entry.Details["previous_subject_uid"] = current.SubjectUID
		return true, execMember(ctx, tx,
			`UPDATE project_members SET subject_uid = $3 WHERE project_uid = $1 AND user_uid = $2`,
			projectUID, userUID, subjectUID)
	})
}

// subjectInProjectSQL reports whether the subject $1 is a person of the project
// $2 — the query internal/auth's checkSubject runs.
const subjectInProjectSQL = `SELECT EXISTS (SELECT 1 FROM subjects WHERE uid = $1 AND project_id = $2)`

// requireSubjectInProject returns ErrSubjectNotFound unless subjectUID (nil: no
// link, always fine) names a person of projectUID, run on the transaction of the
// write it guards.
func requireSubjectInProject(ctx context.Context, tx pgx.Tx, projectUID string, subjectUID *string) error {
	if subjectUID == nil {
		return nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, subjectInProjectSQL, *subjectUID, projectUID).Scan(&ok); err != nil {
		return fmt.Errorf("checking subject %s: %w", *subjectUID, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s in project %s", ErrSubjectNotFound, *subjectUID, projectUID)
	}
	return nil
}

// RemoveMember removes the account userUID from the project projectUID and
// records ActionProjectMemberRemove (with the role it held) in the same
// transaction. It returns ErrNotFound for an unknown project, ErrNotMember when
// the account is not a member and ErrLastAdmin when it is the project's only
// admin. Who may remove whom (themselves included) is the caller's rule; the
// store only keeps the project governable.
func (s *Store) RemoveMember(ctx context.Context, meta audit.Meta, projectUID, userUID string) error {
	entry := projectEntry(meta, projectUID, audit.ActionProjectMemberRemove, MemberTargetType, userUID, map[string]any{
		"project_uid": projectUID,
	})
	_, err := s.mutateMember(ctx, entry, projectUID, userUID, func(tx pgx.Tx, current *Member) (bool, error) {
		if current.Role.IsAdmin() {
			if err := ensureAnotherAdmin(ctx, tx, projectUID); err != nil {
				return false, err
			}
		}
		entry.Details["role"] = string(current.Role)
		return true, execMember(ctx, tx,
			`DELETE FROM project_members WHERE project_uid = $1 AND user_uid = $2`, projectUID, userUID)
	})
	return err
}

// TouchLastProject remembers projectUID as the project the account userUID last
// worked in, so a sign-in can land it there. It is bookkeeping, not a decision,
// and is deliberately not audited; it does not check membership. It returns
// ErrUserNotFound for an unknown account and ErrNotFound for an unknown project.
func (s *Store) TouchLastProject(ctx context.Context, userUID, projectUID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET last_project_uid = $2 WHERE uid = $1`, userUID, projectUID)
	if fkConstraint(err) != "" {
		return fmt.Errorf("%w: %s", ErrNotFound, projectUID)
	}
	if err != nil {
		return fmt.Errorf("project: remembering %s as the last project of %s: %w", projectUID, userUID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrUserNotFound, userUID)
	}
	return nil
}

// mutateMember runs one membership change in a transaction that holds the
// project row's lock — so concurrent changes to one project's members (two
// admins demoting each other) are serialised and the last-admin guard cannot be
// raced — and reads the locked membership first. change returns whether it
// changed anything: when it did, entry is written in the same transaction and
// the member is read back; when it did not, nothing is written and the member is
// returned as it was. The result is nil after a removal.
func (s *Store) mutateMember(
	ctx context.Context, entry audit.Entry, projectUID, userUID string,
	change func(pgx.Tx, *Member) (bool, error),
) (*Member, error) {
	var result *Member
	err := s.inTx(ctx, entry, func(tx pgx.Tx) error {
		if err := lockProject(ctx, tx, projectUID); err != nil {
			return err
		}
		current, err := lockMember(ctx, tx, projectUID, userUID)
		if err != nil {
			return err
		}
		changed, err := change(tx, current)
		if err != nil {
			return err
		}
		if !changed {
			result = current
			return errNoChange
		}
		result, err = lockMember(ctx, tx, projectUID, userUID)
		if errors.Is(err, ErrNotMember) {
			result = nil // removed
			return nil
		}
		return err
	})
	if errors.Is(err, errNoChange) {
		return result, nil
	}
	if err != nil {
		return nil, wrapMemberErr("changing", projectUID, userUID, err)
	}
	return result, nil
}

// execMember runs one membership write inside tx, wrapping its failure.
func execMember(ctx context.Context, tx pgx.Tx, query string, args ...any) error {
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("writing membership: %w", err)
	}
	return nil
}

// lockProject locks the project row uid for the rest of the transaction, the
// mutex every membership change of that project takes. It returns ErrNotFound
// for an unknown project.
func lockProject(ctx context.Context, tx pgx.Tx, uid string) error {
	var locked string
	err := tx.QueryRow(ctx, `SELECT uid FROM projects WHERE uid = $1 FOR UPDATE`, uid).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, uid)
	}
	if err != nil {
		return fmt.Errorf("locking project %s: %w", uid, err)
	}
	return nil
}

// lockMemberSQL reads one membership and locks it for the rest of the transaction.
const lockMemberSQL = membershipSQL + ` FOR UPDATE OF m`

// lockMember reads and locks one membership inside tx, mapping "no row" to
// ErrNotMember.
func lockMember(ctx context.Context, tx pgx.Tx, projectUID, userUID string) (*Member, error) {
	m, err := scanMember(tx.QueryRow(ctx, lockMemberSQL, projectUID, userUID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotMember
	}
	return m, err
}

// ensureAnotherAdmin returns ErrLastAdmin unless the project has more than one
// admin member, so the caller may demote or remove one of them. The caller
// holds the project lock, so the count cannot change underneath it.
func ensureAnotherAdmin(ctx context.Context, tx pgx.Tx, projectUID string) error {
	var admins int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM project_members WHERE project_uid = $1 AND role = $2`,
		projectUID, string(RoleAdmin)).Scan(&admins); err != nil {
		return fmt.Errorf("counting the admins of %s: %w", projectUID, err)
	}
	if admins <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// sameSubject reports whether two optional subject uids name the same subject
// (both nil counts as the same).
func sameSubject(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// fkConstraint returns the name of the foreign-key constraint err violated, or
// "" when err is not a foreign-key violation.
func fkConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
		return pgErr.ConstraintName
	}
	return ""
}

// wrapMemberErr passes the package's sentinel errors through unchanged, turns a
// violated membership foreign key into the sentinel of what it points at, and
// wraps anything else with the operation, the project and the account.
func wrapMemberErr(op, projectUID, userUID string, err error) error {
	for _, sentinel := range []error{ErrNotFound, ErrNotMember, ErrAlreadyMember, ErrLastAdmin} {
		if errors.Is(err, sentinel) {
			return err
		}
	}
	switch constraint := fkConstraint(err); {
	case strings.Contains(constraint, "user_uid"):
		return fmt.Errorf("%w: %s", ErrUserNotFound, userUID)
	case strings.Contains(constraint, "subject_uid"):
		return fmt.Errorf("%w (member %s of %s)", ErrSubjectNotFound, userUID, projectUID)
	case strings.Contains(constraint, "project_uid"):
		return fmt.Errorf("%w: %s", ErrNotFound, projectUID)
	}
	return fmt.Errorf("project: %s member %s of %s: %w", op, userUID, projectUID, err)
}
