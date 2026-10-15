package invite

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// lockInviteSQL reads the invite a code hash names and locks both it and its
// project for the rest of the transaction: the invite so two accepts cannot
// both take its last use, the project because it is the mutex every membership
// change of that project takes (internal/project), so an accept and a removal
// or role change of the same member never interleave.
var lockInviteSQL = selectInviteSQL + " WHERE i.code_hash = $1 FOR UPDATE OF i, p"

// lockMembershipSQL reads and locks the account's membership of the project.
const lockMembershipSQL = `SELECT role, approved_at FROM project_members
WHERE project_uid = $1 AND user_uid = $2 FOR UPDATE`

// grantMembershipSQL creates the membership approved with role $3, or sets an
// existing one's role to $3 and approves it when it still waited. The caller
// has already decided $3 is never lower than the role held.
const grantMembershipSQL = `INSERT INTO project_members (project_uid, user_uid, role, approved_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (project_uid, user_uid) DO UPDATE
SET role = EXCLUDED.role, approved_at = COALESCE(project_members.approved_at, EXCLUDED.approved_at)`

// countUseSQL counts one use of an invite.
const countUseSQL = `UPDATE project_invites SET uses = uses + 1 WHERE uid = $1`

// membership is the part of a project_members row accepting decides on.
type membership struct {
	role       project.Role
	approvedAt *time.Time
}

// Accept lets the account userUID — signed in, already approved — accept the
// invite code names, in one transaction: it adds an approved membership with
// the invite's role, raises a lower role it already holds (never lowers a
// higher one) and approves a membership that was still waiting, counts one use
// and records ActionProjectInviteAccept, the actor taken from meta. When the
// account already holds an approved membership at or above the invite's role,
// nothing is written and no use is counted, so accepting twice is idempotent —
// even once the invite reached its cap.
//
// It returns ErrInvalid for a malformed, unknown, expired, revoked or used-up
// code and for an invite of a deactivated project.
func (s *Store) Accept(ctx context.Context, code, userUID string, meta audit.Meta) (Acceptance, error) {
	if !uploadlink.ValidCode(code) {
		return Acceptance{}, ErrInvalid
	}
	var out Acceptance
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		inv, err := s.lockOpen(ctx, tx, code)
		if err != nil {
			return err
		}
		out, err = accept(ctx, tx, inv, userUID, meta)
		return err
	})
	if err != nil {
		return Acceptance{}, err
	}
	return out, nil
}

// accept is Accept's work on tx once the invite inv is locked and known open.
func accept(ctx context.Context, tx pgx.Tx, inv Invite, userUID string, meta audit.Meta) (Acceptance, error) {
	out := Acceptance{ProjectUID: inv.ProjectUID, ProjectSlug: inv.ProjectSlug}
	current, member, err := lockMembership(ctx, tx, inv.ProjectUID, userUID)
	if err != nil {
		return Acceptance{}, err
	}
	if member && current.approvedAt != nil && rank(current.role) >= rank(inv.Role) {
		out.Role = current.role
		return out, nil
	}
	if inv.usedUp() {
		return Acceptance{}, ErrInvalid
	}
	out.Role, out.Changed = inv.Role, true
	var previous project.Role
	if member {
		previous = current.role
		out.Role = higher(current.role, inv.Role)
	}
	if _, err := tx.Exec(ctx, grantMembershipSQL, inv.ProjectUID, userUID, string(out.Role)); err != nil {
		return Acceptance{}, fmt.Errorf("invite: granting the membership: %w", err)
	}
	if err := countUse(ctx, tx, inv, userUID, out.Role, previous, meta); err != nil {
		return Acceptance{}, err
	}
	return out, nil
}

// RedeemTx counts one use of the invite code names for the account userUID,
// which a registration through the invite has just created on tx together with
// its approved membership of the invite's project (in RegistrationRole of the
// invite's role), and records ActionProjectInviteAccept on tx, the actor taken
// from meta. It locks the invite first, so two registrations racing for its
// last use cannot both win. It returns the invite, or ErrInvalid when it is no
// longer usable — which must roll the account back.
func (s *Store) RedeemTx(ctx context.Context, tx pgx.Tx, code, userUID string, meta audit.Meta) (Invite, error) {
	if !uploadlink.ValidCode(code) {
		return Invite{}, ErrInvalid
	}
	inv, err := s.lockOpen(ctx, tx, code)
	if err != nil {
		return Invite{}, err
	}
	if inv.usedUp() {
		return Invite{}, ErrInvalid
	}
	if err := countUse(ctx, tx, inv, userUID, RegistrationRole(inv.Role), "", meta); err != nil {
		return Invite{}, err
	}
	return inv, nil
}

// lockOpen locks the invite code names and its project on tx and returns it, or
// ErrInvalid when there is none or it is revoked, expired or of a deactivated
// project. The use cap is left to the caller.
func (s *Store) lockOpen(ctx context.Context, tx pgx.Tx, code string) (Invite, error) {
	inv, err := scanInvite(tx.QueryRow(ctx, lockInviteSQL, uploadlink.HashSecret(code)), ErrInvalid)
	if err != nil {
		return Invite{}, err
	}
	if !inv.openAt(s.now()) {
		return Invite{}, ErrInvalid
	}
	return inv, nil
}

// lockMembership reads and locks the account's membership of the project on
// tx; the boolean is false when it has none.
func lockMembership(ctx context.Context, tx pgx.Tx, projectUID, userUID string) (membership, bool, error) {
	var m membership
	err := tx.QueryRow(ctx, lockMembershipSQL, projectUID, userUID).Scan(&m.role, &m.approvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return membership{}, false, nil
	}
	if err != nil {
		return membership{}, false, fmt.Errorf("invite: locking the membership: %w", err)
	}
	return m, true, nil
}

// countUse counts one use of inv on tx and records ActionProjectInviteAccept for
// the account userUID, which now holds role (previous before, "" when it was
// not a member).
func countUse(
	ctx context.Context, tx pgx.Tx, inv Invite, userUID string, role, previous project.Role, meta audit.Meta,
) error {
	if _, err := tx.Exec(ctx, countUseSQL, inv.UID); err != nil {
		return fmt.Errorf("invite: counting the use: %w", err)
	}
	if meta.ActorUID == "" {
		meta.ActorUID = userUID
	}
	entry := meta.Entry(audit.ActionProjectInviteAccept, TargetType, inv.UID, map[string]any{
		"project_uid": inv.ProjectUID, "user_uid": userUID, "role": string(role),
		"previous_role": string(previous), "invite_role": string(inv.Role),
	})
	entry.ProjectUID = inv.ProjectUID
	if err := audit.Write(ctx, tx, entry); err != nil {
		return fmt.Errorf("invite: auditing the accept: %w", err)
	}
	return nil
}
