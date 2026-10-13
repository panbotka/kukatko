package inviteapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/invite"
)

// Redeemer resolves and redeems invites for a registration. It is satisfied by
// *invite.Store.
type Redeemer interface {
	// Resolve returns the live invite a code names, or invite.ErrInvalid.
	Resolve(ctx context.Context, code string) (invite.Invite, error)
	// RedeemTx counts one use of the invite on the account's transaction, or
	// answers invite.ErrInvalid.
	RedeemTx(ctx context.Context, tx pgx.Tx, code, userUID string, meta audit.Meta) (invite.Invite, error)
}

// RegistrationGate implements auth.InviteGate: a usable invite stands in for
// the shared registration secret and for the approval.
type RegistrationGate struct {
	invites Redeemer
}

// NewRegistrationGate returns a gate resolving invites through invites.
func NewRegistrationGate(invites Redeemer) *RegistrationGate {
	return &RegistrationGate{invites: invites}
}

// AdmitInvite admits a registration naming code when code names a usable
// invite, returning auth.ErrRegistrationInvite otherwise — one answer for every
// reason. The grant names the invite's project and its role capped by
// invite.RegistrationRole, and redeems the invite on the account's transaction,
// failing with auth.ErrRegistrationInvite when it stopped being usable since.
func (g *RegistrationGate) AdmitInvite(ctx context.Context, code string) (auth.InviteGrant, error) {
	inv, err := g.invites.Resolve(ctx, code)
	if errors.Is(err, invite.ErrInvalid) {
		return auth.InviteGrant{}, auth.ErrRegistrationInvite
	}
	if err != nil {
		return auth.InviteGrant{}, fmt.Errorf("inviteapi: resolving the invite: %w", err)
	}
	return auth.InviteGrant{
		InviteUID:  inv.UID,
		ProjectUID: inv.ProjectUID,
		Role:       invite.RegistrationRole(inv.Role),
		Redeem: func(ctx context.Context, tx pgx.Tx, userUID string, meta audit.Meta) error {
			_, err := g.invites.RedeemTx(ctx, tx, code, userUID, meta)
			if errors.Is(err, invite.ErrInvalid) {
				return auth.ErrRegistrationInvite
			}
			if err != nil {
				return fmt.Errorf("inviteapi: redeeming the invite: %w", err)
			}
			return nil
		},
	}, nil
}
