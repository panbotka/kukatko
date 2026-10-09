package auth

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
)

// RenameConfig bundles what NewRename needs.
type RenameConfig struct {
	// Service is the auth domain service whose store and maintainer boundary the
	// rename reuses (required).
	Service *Service
	// Mail schedules the message telling the person their new username.
	// Optional: an instance that wires none renames accounts and sends nothing,
	// exactly like one whose mail is switched off.
	Mail MailScheduler
	// SignInURL is the address the mail points at, normally this instance's
	// public URL plus /login. An empty one leaves the link out of the message.
	SignInURL string
}

// Rename is the administrator's change of an account's username — for the
// person who registered as "xXx_jan_xXx" and is better known as "jan.novak".
//
// It is a separate type rather than a Service method for the reason Approval is
// one: renaming sends mail, and Service deliberately knows nothing about mail.
// The message matters more than it looks — the username is what the person signs
// in with, and without being told they would type the old one, be refused and
// not know why.
type Rename struct {
	svc       *Service
	mail      MailScheduler
	signInURL string
}

// NewRename returns a Rename from cfg, defaulting Mail to a scheduler that sends
// nothing.
func NewRename(cfg RenameConfig) *Rename {
	mail := cfg.Mail
	if mail == nil {
		mail = noMail{}
	}
	return &Rename{svc: cfg.Service, mail: mail, signInURL: cfg.SignInURL}
}

// Rename gives the account identified by uid the new name username, normalized
// and validated exactly as account creation does it (trimmed, lower-cased, not
// empty, at most MaxUsernameLen runes). The new name, the audit entry (entry,
// stamped with the old and new names) and the mail telling the person are all
// written on one transaction, so a rename that fails at any point neither
// changes the name nor promises a change.
//
// Nothing else about the account moves. Its sessions, API tokens and passkeys
// are keyed on the account's uid, so the person stays signed in everywhere and
// every credential keeps working; only the password sign-in now wants the new
// name. The placeholder address of an account that has no real one
// (<name>-<uid>@kukatko.invalid) is deliberately left as it is: it only has to be
// unique and undeliverable, and the uid already makes it unique.
//
// actor is the role of the account performing the rename; the maintainer
// boundary applies exactly as it does to every other user-management action, so
// a non-maintainer renaming a maintainer account gets ErrMaintainerRequired.
// Renaming an account to the name it already has is a no-op that returns the
// account unchanged, with no mail and no audit entry. It returns
// ErrUsernameRequired or ErrUsernameTooLong for a name it will not store,
// ErrUsernameTaken for a name another account holds (in any letter case), and
// ErrUserNotFound when the account does not exist.
func (rn *Rename) Rename(ctx context.Context, uid, username string, actor Role, entry audit.Entry) (User, error) {
	if err := rn.svc.guardMaintainerBoundary(ctx, actor, uid, ""); err != nil {
		return User{}, err
	}
	normalized := normalizeUsername(username)
	if err := validateAccountUsername(normalized); err != nil {
		return User{}, err
	}
	return rn.svc.store.RenameUserAudited(ctx, uid, normalized, entry,
		func(ctx context.Context, tx pgx.Tx, user User) error {
			return rn.scheduleMail(ctx, tx, user)
		})
}

// scheduleMail enqueues the "your username is now X" message on tx, so it is
// scheduled if and only if the rename commits. The scheduler itself drops it
// silently when mail is off or the account's address is a .invalid placeholder,
// and the rename still succeeds; a message that is refused for any other reason
// fails the rename, because an administrator who sees the refusal can simply try
// again, while a rename the person was never told about locks them out.
func (rn *Rename) scheduleMail(ctx context.Context, tx pgx.Tx, user User) error {
	m := mailjob.UsernameChanged(user.Email, mailer.UsernameChangedData{
		DisplayName: user.DisplayName,
		Username:    user.Username,
		SignInURL:   rn.signInURL,
	})
	if err := rn.mail.Enqueue(ctx, tx, m); err != nil {
		return fmt.Errorf("auth: scheduling the rename notice: %w", err)
	}
	return nil
}
