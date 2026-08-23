package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/mailer"
	"github.com/panbotka/kukatko/internal/mailjob"
)

// PasswordResetTTL is how long a reset link stays usable. Seven days is
// deliberately generous: the link is handed out by an administrator to somebody
// who is, by definition, currently locked out, and who may read the mail only
// after a weekend away. The window costs nothing an attacker could use — the
// token is 256 bits, single-use, and issuing a new one kills the old — while a
// link that dies overnight simply produces a second support request.
const PasswordResetTTL = 7 * 24 * time.Hour

// passwordResetPath is the frontend route where somebody chooses a new password,
// used as the link base when the caller configured none. The token is appended
// as the last path segment, so the site-relative form is still a usable link an
// administrator can paste after their own host.
const passwordResetPath = "/password-reset"

// ErrPasswordResetInvalid is the single answer to every unusable link: unknown,
// already used, expired, or belonging to an account that has since been blocked.
// It is deliberately unspecific — the endpoints behind it need no authentication,
// so telling a caller *which* of those a token is would let them probe the table
// they cannot read.
var ErrPasswordResetInvalid = errors.New("auth: the password-reset link is not valid")

// PasswordResetToken is one outstanding reset: the account it belongs to, the
// hash of the link that was handed out, and the two times that decide whether it
// still works. The token itself is never stored and exists only in the mail and
// in the answer to the administrator who issued it.
type PasswordResetToken struct {
	// ID identifies the row; it is recorded in both audit entries, so issuing
	// and using one link can be tied together in the trail.
	ID string
	// UserUID is the account whose password the link sets.
	UserUID string
	// TokenHash is the hex-encoded SHA-256 of the token, the lookup key.
	TokenHash string
	// CreatedAt is when the link was issued.
	CreatedAt time.Time
	// ExpiresAt is the instant the link stops working (usable strictly before).
	ExpiresAt time.Time
	// UsedAt is when the link was consumed, or nil while it is still unused.
	UsedAt *time.Time
	// IssuedByUID is the account that started the reset, or nil once that
	// account has been deleted.
	IssuedByUID *string
}

// Usable reports whether the link may still set a password as of now: it has not
// been consumed and its expiry has not been reached. The boundary instant counts
// as expired, exactly as an API token's does.
func (t PasswordResetToken) Usable(now time.Time) bool {
	return t.UsedAt == nil && t.ExpiresAt.After(now)
}

// newPasswordResetID returns a fresh id for a password_reset_tokens row.
func newPasswordResetID() (string, error) {
	return newUID(passwordResetIDPrefix)
}

// hashPasswordResetToken returns the hex-encoded SHA-256 of token.
//
// It is not bcrypt, for the reason spelled out at hashAPITokenSecret: the token
// is 256 bits from crypto/rand, so there is no dictionary a slow hash would
// defend against, and the hash is the indexed lookup key of the table.
func hashPasswordResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IssuedPasswordReset is what an administrator gets back for a reset they
// started: the whole link, so they can send it by hand to somebody whose mailbox
// is not working, plus where it was mailed and how long it lasts.
type IssuedPasswordReset struct {
	ResetURL  string    `json:"reset_url"`
	ExpiresAt time.Time `json:"expires_at"`
	Email     string    `json:"email"`
}

// PasswordResetStatus is what an unauthenticated caller learns about a link:
// whether it still works and, only then, whom to greet and until when. Nothing
// else about the account is published — the token is a bearer credential of one
// narrow power, and a page that shows a name is already the most it needs.
type PasswordResetStatus struct {
	Valid       bool       `json:"valid"`
	DisplayName string     `json:"display_name,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// PasswordResetConfig bundles what NewPasswordReset needs.
type PasswordResetConfig struct {
	// Service is the auth domain service whose store, clock, hashing and
	// maintainer boundary the flow reuses (required).
	Service *Service
	// Mail schedules the message carrying the link. Optional: an instance that
	// wires none still issues links, it just sends nothing — which is exactly
	// what the administrator's copy of the link is for.
	Mail MailScheduler
	// LinkBase is the address of the page where somebody chooses a new password,
	// normally this instance's public URL plus /password-reset. The token is
	// appended as the last path segment. An empty base falls back to the
	// site-relative path, which is what an instance without a configured public
	// URL can honestly say.
	LinkBase string
	// TTL overrides how long a link lasts; zero or less means PasswordResetTTL.
	TTL time.Duration
}

// PasswordReset is the "somebody forgot their password" flow: an administrator
// issues a one-time link, the person behind it chooses their own password, and
// the administrator never learns what it is.
//
// It is a separate type for the same reason Registration and Approval are: it
// sends mail, and Service deliberately knows nothing about mail. The two public
// halves of the flow (is this link still good, here is my new password) need no
// authentication at all, which is why every refusal they can produce collapses
// into ErrPasswordResetInvalid.
type PasswordReset struct {
	svc      *Service
	mail     MailScheduler
	linkBase string
	ttl      time.Duration
}

// NewPasswordReset returns a PasswordReset from cfg, defaulting Mail to a
// scheduler that sends nothing, LinkBase to the site-relative reset path and TTL
// to PasswordResetTTL.
func NewPasswordReset(cfg PasswordResetConfig) *PasswordReset {
	mail := cfg.Mail
	if mail == nil {
		mail = noMail{}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.LinkBase), "/")
	if base == "" {
		base = passwordResetPath
	}
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = PasswordResetTTL
	}
	return &PasswordReset{svc: cfg.Service, mail: mail, linkBase: base, ttl: ttl}
}

// Issue starts a password reset for the account identified by uid: it mints a
// token, invalidates whatever unused link that account still had, schedules the
// mail carrying the new one and records the decision in the audit trail — all on
// one transaction, so a reset that fails at any point neither hands out a link
// nor promises anybody a mail.
//
// The returned link is the only copy anybody will ever see besides the mail: the
// database keeps a hash. It is answered to the administrator on purpose, so a
// person whose address is wrong (or whose mail is switched off on this instance)
// can still be helped by hand.
//
// actor is the role of the account performing the reset; the maintainer boundary
// applies exactly as it does to setting a password directly, because that is
// what this link ultimately does — a lower actor resetting a maintainer gets
// ErrMaintainerRequired. A blocked account is refused with ErrUserDisabled (a
// link that cannot be used is worse than no link), a missing one with
// ErrUserNotFound.
func (pr *PasswordReset) Issue(
	ctx context.Context, uid string, actor Role, entry audit.Entry,
) (IssuedPasswordReset, error) {
	if err := pr.svc.guardMaintainerBoundary(ctx, actor, uid, ""); err != nil {
		return IssuedPasswordReset{}, err
	}
	token, err := newToken()
	if err != nil {
		return IssuedPasswordReset{}, err
	}
	id, err := newPasswordResetID()
	if err != nil {
		return IssuedPasswordReset{}, err
	}
	now := pr.svc.now()
	row := PasswordResetToken{
		ID:        id,
		UserUID:   uid,
		TokenHash: hashPasswordResetToken(token),
		CreatedAt: now,
		ExpiresAt: now.Add(pr.ttl),
	}
	if entry.ActorUID != "" {
		row.IssuedByUID = &entry.ActorUID
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["reset_id"] = row.ID
	entry.Details["expires_at"] = row.ExpiresAt.UTC().Format(time.RFC3339)

	link := pr.link(token)
	user, err := pr.svc.store.IssuePasswordResetAudited(ctx, row, entry,
		func(ctx context.Context, tx pgx.Tx, user User) error {
			return pr.scheduleMail(ctx, tx, user, link)
		})
	if err != nil {
		return IssuedPasswordReset{}, err
	}
	return IssuedPasswordReset{ResetURL: link, ExpiresAt: row.ExpiresAt, Email: user.Email}, nil
}

// link assembles the address the person follows, the token as the last path
// segment of the configured base.
func (pr *PasswordReset) link(token string) string {
	return pr.linkBase + "/" + token
}

// scheduleMail enqueues the message carrying the link on tx, so it is scheduled
// if and only if the token commits.
//
// A message that will not schedule fails the whole reset, as an approval's does:
// the administrator sees the refusal and can simply issue another link, which is
// harmless precisely because issuing one invalidates the previous.
func (pr *PasswordReset) scheduleMail(ctx context.Context, tx pgx.Tx, user User, link string) error {
	m := mailjob.PasswordReset(user.Email, mailer.PasswordResetData{
		DisplayName: user.DisplayName,
		ResetURL:    link,
		ValidFor:    pr.ttl,
	})
	if err := pr.mail.Enqueue(ctx, tx, m); err != nil {
		return fmt.Errorf("auth: scheduling the password-reset mail: %w", err)
	}
	return nil
}

// Status reports whether token may still set a password, so the page behind the
// link can say "this link has expired" instead of showing a form that is going
// to fail. An unusable link is not an error: it answers a status with Valid
// false and nothing else, and only a real failure to read the database returns
// one.
func (pr *PasswordReset) Status(ctx context.Context, token string) (PasswordResetStatus, error) {
	row, user, err := pr.svc.store.GetPasswordResetByToken(ctx, hashPasswordResetToken(token))
	switch {
	case errors.Is(err, ErrPasswordResetInvalid):
		return PasswordResetStatus{}, nil
	case err != nil:
		return PasswordResetStatus{}, err
	}
	if !row.Usable(pr.svc.now()) || user.Disabled {
		return PasswordResetStatus{}, nil
	}
	expires := row.ExpiresAt
	return PasswordResetStatus{Valid: true, DisplayName: user.DisplayName, ExpiresAt: &expires}, nil
}

// Consume sets newPassword for the account the link belongs to and burns the
// link: it is marked used, so a second attempt fails, and *every* session of
// that account is deleted — including the one that may be signed in right now,
// since somebody resetting a password is exactly the case where an unwanted
// session might be open. The password goes through the same length rule and the
// same bcrypt hashing as any other password change.
//
// The whole thing is one transaction with its audit entry, whose actor and
// target are both the account: nobody else took part, and the person behind the
// link proved only that they hold it.
//
// It returns ErrPasswordResetInvalid for a link that is unknown, used, expired
// or whose account has been blocked, and ErrPasswordTooShort for a password the
// rules refuse.
func (pr *PasswordReset) Consume(
	ctx context.Context, token, newPassword string, entry audit.Entry,
) (User, error) {
	hash := hashPasswordResetToken(token)
	// A look before the expensive part: bcrypt costs a quarter of a second, and
	// a caller who does not hold a live link should not be able to spend it. It
	// also names the account the audit entry is attributed to. The transaction
	// below re-reads the row under a lock and repeats every check, so this is a
	// fast refusal and never the decision.
	row, user, err := pr.svc.store.GetPasswordResetByToken(ctx, hash)
	if err != nil {
		return User{}, err
	}
	if !row.Usable(pr.svc.now()) || user.Disabled {
		return User{}, ErrPasswordResetInvalid
	}
	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return User{}, err
	}
	entry.ActorUID = user.UID
	entry.TargetUID = user.UID
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["reset_id"] = row.ID
	return pr.svc.store.ConsumePasswordResetAudited(ctx, hash, passwordHash, pr.svc.now(), entry)
}
