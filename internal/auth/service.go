package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// SessionPolicy configures session expiry: TTL is the sliding idle window and
// MaxLifetime caps a session's absolute age.
type SessionPolicy struct {
	TTL         time.Duration
	MaxLifetime time.Duration
}

// Service orchestrates the auth domain: it combines the Store with password
// hashing, UID/token generation, and the session-expiry policy. The now field is
// the time source, overridable in tests via WithClock; production uses
// time.Now.
type Service struct {
	store  *Store
	policy SessionPolicy
	now    func() time.Time
}

// NewService returns a Service backed by store and governed by policy, using
// time.Now as its clock.
func NewService(store *Store, policy SessionPolicy) *Service {
	return &Service{store: store, policy: policy, now: time.Now}
}

// WithClock overrides the service's time source and returns the same service for
// chaining. It exists for deterministic testing of sliding expiry and is not
// used in production.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// normalizeUsername lower-cases and trims surrounding whitespace so lookups and
// uniqueness are case-insensitive.
func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// validateUsername returns ErrUsernameTooLong when username exceeds
// MaxUsernameLen. Length is counted in runes so a name of accented characters is
// not rejected for being multi-byte. The caller passes an already normalized
// username; no real account can be longer, so rejecting up front keeps oversized
// input out of both the account store and the login rate limiter's key set.
func validateUsername(username string) error {
	if utf8.RuneCountInString(username) > MaxUsernameLen {
		return ErrUsernameTooLong
	}
	return nil
}

// placeholderDomain is the domain every generated placeholder address lives in.
// ".invalid" is reserved by RFC 2606 and guaranteed never to resolve, so a
// placeholder that slipped into a real send bounces at the resolver rather than
// arriving in a stranger's mailbox. internal/mailer and internal/mailjob refuse
// such a recipient outright; migration 0063 uses the same domain for the
// addresses it backfills.
const placeholderDomain = "kukatko.invalid"

// placeholderEmail returns the stand-in address for an account that genuinely
// has none to give — today only the bootstrap maintainer, created on an empty
// database before anybody could have configured one. The local part is username
// reduced to [a-z0-9-] and trimmed of separators, so a username of spaces or
// accents still yields a syntactically valid address; a username that reduces to
// nothing falls back to "user".
//
// It is deliberately not a general answer to a missing address: every other
// creation path must supply a real one, and validateEmail refuses the empty
// string.
func placeholderEmail(username string) string {
	var local strings.Builder
	dashed := false
	for _, r := range strings.ToLower(username) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			local.WriteRune(r)
			dashed = false
			continue
		}
		if !dashed && local.Len() > 0 {
			local.WriteByte('-')
			dashed = true
		}
	}
	name := strings.Trim(local.String(), "-")
	if name == "" {
		name = "user"
	}
	return name + "@" + placeholderDomain
}

// normalizeEmail trims surrounding whitespace and lower-cases the domain part,
// which is case-insensitive by definition. The local part is left exactly as it
// was typed: RFC 5321 leaves its interpretation to the receiving host, so
// lower-casing it could address a different mailbox than the one meant.
func normalizeEmail(email string) string {
	trimmed := strings.TrimSpace(email)
	at := strings.LastIndex(trimmed, "@")
	if at < 0 {
		return trimmed
	}
	return trimmed[:at] + "@" + strings.ToLower(trimmed[at+1:])
}

// validateEmail returns ErrInvalidEmail unless email is a single, syntactically
// valid mailbox that mail could actually be delivered to. The caller passes an
// already normalized address.
//
// The grammar comes from net/mail, but parsing alone is too generous for a
// stored account address: it accepts a display-name form ("Jan <jan@x.cz>") and
// a quoted local part that may hold spaces, neither of which is what an
// administrator meant to type into an address field. Requiring the parse to give
// back the input unchanged rules both out. A dotless domain is refused too —
// "jan@localhost" parses, but no account on this instance can receive mail there.
func validateEmail(email string) error {
	if email == "" || len(email) > MaxEmailLen {
		return ErrInvalidEmail
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return ErrInvalidEmail
	}
	domain := email[strings.LastIndex(email, "@")+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return ErrInvalidEmail
	}
	return nil
}

// Login verifies username/password and, on success, creates and returns a new
// session together with the authenticated user. Every failure mode an anonymous
// caller could probe with (unknown user, wrong password, disabled account)
// returns ErrInvalidCredentials so the caller cannot distinguish them — neither
// by the error nor by how long the attempt took, see checkLoginPassword; query
// and hashing failures are returned wrapped. The one exception is an account
// waiting for an administrator's approval, which returns ErrNotApproved and
// creates no session: it is only ever reached with the right password, and
// somebody who has just registered has to be told what they are waiting for. The
// caller is responsible for login rate limiting.
func (s *Service) Login(ctx context.Context, username, password string) (Session, User, error) {
	user, err := s.store.GetUserByUsername(ctx, normalizeUsername(username))
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return Session{}, User{}, err
	}
	if err := checkLoginPassword(user, err == nil, password); err != nil {
		return Session{}, User{}, err
	}

	sess, err := s.createSession(ctx, user)
	if err != nil {
		return Session{}, User{}, err
	}
	if err := s.store.SetLastLogin(ctx, user.UID, s.now()); err != nil {
		return Session{}, User{}, err
	}
	return sess, user, nil
}

// createSession generates identifiers and tokens for user, sets the initial
// expiry to now+TTL, persists the session, and returns it.
func (s *Service) createSession(ctx context.Context, user User) (Session, error) {
	id, err := newSessionID()
	if err != nil {
		return Session{}, err
	}
	token, err := newToken()
	if err != nil {
		return Session{}, err
	}
	downloadToken, err := newToken()
	if err != nil {
		return Session{}, err
	}
	now := s.now()
	sess := Session{
		ID:            id,
		Token:         token,
		DownloadToken: downloadToken,
		UserUID:       user.UID,
		Role:          user.Role,
		CreatedAt:     now,
		ExpiresAt:     now.Add(s.policy.TTL),
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Authenticate validates an opaque session token and returns the live user and
// the (possibly extended) session. It returns ErrSessionNotFound for an unknown
// token, ErrSessionExpired (and deletes the row) for a lapsed session, and
// ErrUserDisabled (and deletes the user's sessions) if the account was disabled.
// On success it slides the session's expiry forward per the policy.
func (s *Service) Authenticate(ctx context.Context, token string) (User, Session, error) {
	sess, err := s.store.GetSessionByToken(ctx, token)
	if err != nil {
		return User{}, Session{}, err
	}
	now := s.now()
	if !sess.ExpiresAt.After(now) {
		_ = s.store.DeleteSessionByToken(ctx, token)
		return User{}, Session{}, ErrSessionExpired
	}

	user, err := s.store.GetUserByUID(ctx, sess.UserUID)
	if err != nil {
		return User{}, Session{}, err
	}
	if user.Disabled {
		_, _ = s.store.DeleteUserSessions(ctx, user.UID)
		return User{}, Session{}, ErrUserDisabled
	}

	sess, err = s.extendSession(ctx, sess, user, now)
	if err != nil {
		return User{}, Session{}, err
	}
	return user, sess, nil
}

// extendSession applies the sliding-expiry policy to sess as of now and persists
// the new expiry when it has advanced by at least the renew interval. The
// session's cached role is refreshed from user so a mid-session role change
// takes effect. It returns the updated session value.
func (s *Service) extendSession(ctx context.Context, sess Session, user User, now time.Time) (Session, error) {
	sess.Role = user.Role
	newExpiry, extend := slideExpiry(sess.CreatedAt, sess.ExpiresAt, now, s.policy.TTL, s.policy.MaxLifetime)
	if !extend {
		return sess, nil
	}
	if err := s.store.UpdateSessionExpiry(ctx, sess.ID, newExpiry); err != nil {
		return Session{}, err
	}
	sess.ExpiresAt = newExpiry
	return sess, nil
}

// AuthenticateDownloadToken validates an opaque media download token and returns
// the live user and session. Unlike Authenticate it does not slide the session's
// expiry (media URLs are fetched far too often for that to be meaningful), but it
// applies the same liveness checks: ErrSessionNotFound for an unknown token,
// ErrSessionExpired (and deletes the row) for a lapsed session, and
// ErrUserDisabled (and deletes the user's sessions) if the account was disabled.
func (s *Service) AuthenticateDownloadToken(ctx context.Context, token string) (User, Session, error) {
	sess, err := s.store.GetSessionByDownloadToken(ctx, token)
	if err != nil {
		return User{}, Session{}, err
	}
	now := s.now()
	if !sess.ExpiresAt.After(now) {
		_ = s.store.DeleteSessionByToken(ctx, sess.Token)
		return User{}, Session{}, ErrSessionExpired
	}

	user, err := s.store.GetUserByUID(ctx, sess.UserUID)
	if err != nil {
		return User{}, Session{}, err
	}
	if user.Disabled {
		_, _ = s.store.DeleteUserSessions(ctx, user.UID)
		return User{}, Session{}, ErrUserDisabled
	}
	sess.Role = user.Role
	return user, sess, nil
}

// Logout deletes the session identified by token. It is idempotent: logging out
// an already-removed session is not an error.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSessionByToken(ctx, token)
}

// ChangePassword verifies the user's current password, stores a hash of the new
// one, and invalidates all of the user's other sessions (every session except
// keepToken, the caller's current session). It returns ErrInvalidCredentials if
// the current password is wrong and ErrPasswordTooShort if the new password is
// too short.
func (s *Service) ChangePassword(
	ctx context.Context, userUID, keepToken, currentPassword, newPassword string,
) error {
	user, err := s.store.GetUserByUID(ctx, userUID)
	if err != nil {
		return err
	}
	if err := CheckPassword(user.PasswordHash, currentPassword); err != nil {
		return ErrInvalidCredentials
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetPasswordHash(ctx, userUID, hash); err != nil {
		return err
	}
	if _, err := s.store.DeleteUserSessionsExcept(ctx, userUID, keepToken); err != nil {
		return err
	}
	return nil
}

// CleanupExpiredSessions deletes all sessions past their expiry as of now,
// returning the number removed. It is invoked on a schedule by RunCleanup.
func (s *Service) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx, s.now())
}

// CleanupFinishedPasswordResets deletes every reset link that can no longer be
// used as of now — expired, or already consumed — returning the number removed.
// It rides the same schedule as the expired-session cleanup (see RunCleanup):
// both are rows whose only remaining purpose was to be refused, and the audit
// trail keeps the permanent record of what happened to them.
func (s *Service) CleanupFinishedPasswordResets(ctx context.Context) (int64, error) {
	return s.store.DeleteFinishedPasswordResets(ctx, s.now())
}
