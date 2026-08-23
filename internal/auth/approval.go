package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/jobs"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
)

// ApprovalConfig bundles what NewApproval needs.
type ApprovalConfig struct {
	// Service is the auth domain service whose store, clock and maintainer
	// boundary the approval reuses (required).
	Service *Service
	// Mail schedules the message telling the person their account is active.
	// Optional: an instance that wires none approves accounts and sends nothing,
	// exactly like one whose mail is switched off.
	Mail MailScheduler
	// SignInURL is the address the mail points at, normally this instance's
	// public URL plus /login. An empty one leaves the link line blank, which is
	// what an instance without a configured public URL can honestly say; with
	// mail enabled the configuration requires mail.base_url, so it is set
	// wherever a message is really sent.
	SignInURL string
}

// Approval is the administrator's half of self-service registration: letting in
// an account that Registration created and left waiting.
//
// It is a separate type for the same reason Registration is one — approving
// sends mail, and Service deliberately knows nothing about mail — and it is
// built from the same pieces, so the two halves of one flow schedule their
// messages through the same interface, on the transaction of the change that
// caused them.
type Approval struct {
	svc       *Service
	mail      MailScheduler
	signInURL string
}

// NewApproval returns an Approval from cfg, defaulting Mail to a scheduler that
// sends nothing.
func NewApproval(cfg ApprovalConfig) *Approval {
	mail := cfg.Mail
	if mail == nil {
		mail = noMail{}
	}
	return &Approval{svc: cfg.Service, mail: mail, signInURL: cfg.SignInURL}
}

// noMail is the scheduler an Approval built without one uses: it schedules
// nothing and reports success, so an instance that wires no mail still approves
// accounts. It is a value rather than a nil check at every call site, so the
// mailing path has exactly one shape.
type noMail struct{}

// Enqueue discards the message and reports success.
func (noMail) Enqueue(context.Context, jobs.Execer, mailjob.Mail) error { return nil }

// Approve lets the account identified by uid in: it stamps the approval time,
// records the decision in the audit trail and schedules the mail telling the
// person they can sign in — all on one transaction, so an approval that fails at
// any point neither lets anybody in nor promises them anything.
//
// It deliberately does not touch the role. The account was created on the lowest
// one and raising it is a separate, deliberate act through the user-update
// endpoint: "this person may come in" and "this person may edit the library" are
// two decisions, and an approval that silently made both would be the wrong
// default in the direction that matters.
//
// actor is the role of the account performing the approval; the maintainer
// boundary applies exactly as it does to every other user-management action, so
// a non-maintainer approving a maintainer account gets ErrMaintainerRequired. An
// account that is already approved is returned unchanged with no mail and no
// audit entry (see Store.ApproveUserAudited); a blocked one is refused with
// ErrUserDisabled, and a missing one with ErrUserNotFound.
func (ap *Approval) Approve(ctx context.Context, uid string, actor Role, entry audit.Entry) (User, error) {
	if err := ap.svc.guardMaintainerBoundary(ctx, actor, uid, ""); err != nil {
		return User{}, err
	}
	return ap.svc.store.ApproveUserAudited(ctx, uid, ap.svc.now(), entry,
		func(ctx context.Context, tx pgx.Tx, user User) error {
			return ap.scheduleMail(ctx, tx, user)
		})
}

// scheduleMail enqueues the "your account is active" message on tx, so it is
// scheduled if and only if the approval commits.
//
// A message that will not schedule fails the approval, unlike the
// administrators' notice a registration sends: this one is the whole point of
// the action — somebody is waiting to be told they may come in — and an
// administrator who sees the refusal can simply click again, which is harmless
// precisely because approving twice is not an error.
func (ap *Approval) scheduleMail(ctx context.Context, tx pgx.Tx, user User) error {
	m := mailjob.AccountApproved(user.Email, mailer.AccountApprovedData{
		DisplayName: user.DisplayName,
		SignInURL:   ap.signInURL,
	})
	if err := ap.mail.Enqueue(ctx, tx, m); err != nil {
		return fmt.Errorf("auth: scheduling the approval notice: %w", err)
	}
	return nil
}
