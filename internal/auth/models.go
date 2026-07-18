package auth

import (
	"errors"
	"time"
)

// Sentinel errors returned by the store and service so callers (handlers, tests)
// can branch with errors.Is.
var (
	// ErrUserNotFound indicates no user matched the given username or UID.
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrSessionNotFound indicates no session matched the given token.
	ErrSessionNotFound = errors.New("auth: session not found")
	// ErrUsernameTaken indicates a username unique-constraint violation.
	ErrUsernameTaken = errors.New("auth: username already taken")
	// ErrInvalidCredentials is returned for any failed login (unknown user,
	// wrong password, or disabled account); it is intentionally unspecific so
	// callers cannot probe which usernames exist.
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	// ErrSessionExpired indicates the session existed but has passed its expiry.
	ErrSessionExpired = errors.New("auth: session expired")
	// ErrInvalidRole indicates a role value outside viewer/editor/admin/maintainer.
	ErrInvalidRole = errors.New("auth: invalid role")
	// ErrMaintainerRequired indicates a non-maintainer tried to grant the
	// maintainer role or to modify an account that already holds it. Only a
	// maintainer may create, promote to, or alter a maintainer account.
	ErrMaintainerRequired = errors.New("auth: only a maintainer may manage the maintainer role")
	// ErrUserDisabled indicates the account is disabled.
	ErrUserDisabled = errors.New("auth: user is disabled")
	// ErrNoteTooLong indicates the user note exceeds MaxNoteLen characters. Its
	// message names the offending field so it can be surfaced verbatim in a 400.
	ErrNoteTooLong = errors.New("auth: note must be at most 1000 characters")
)

// MaxNoteLen is the maximum length of a user note, measured in runes rather than
// bytes so that accented text is not penalised against the limit.
const MaxNoteLen = 1000

// User is a local account. PasswordHash is never serialised to clients (no JSON
// tag exposure); the JSON form is used by the HTTP layer for user-management
// responses, which omit the hash.
type User struct {
	UID         string     `json:"uid"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Role        Role       `json:"role"`
	Disabled    bool       `json:"disabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	// PasswordHash holds the bcrypt hash; it is excluded from JSON output.
	PasswordHash string `json:"-"`
	// Note is a free-text administrative note. It is excluded from JSON output
	// so it can never leak through the login or /auth/me payloads, which embed a
	// User verbatim. The admin-only user endpoints re-add it explicitly via
	// adminUserResponse.
	Note string `json:"-"`
}

// Session is an authenticated session bound to a user. Token is the opaque value
// stored in the HttpOnly cookie; DownloadToken authorises media-download URLs
// separately. Role caches the user's role at creation so authorization does not
// require a user lookup on every request.
type Session struct {
	ID            string    `json:"-"`
	Token         string    `json:"-"`
	DownloadToken string    `json:"download_token"`
	UserUID       string    `json:"user_uid"`
	Role          Role      `json:"role"`
	CreatedAt     time.Time `json:"created_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// slidingRenewInterval is the minimum amount by which a session's expiry must
// move forward before the store is updated. It coalesces the per-request expiry
// extension so an active session triggers at most one write per interval instead
// of one per request.
const slidingRenewInterval = time.Minute

// slideExpiry computes a session's new expiry given the current time, the
// sliding idle window (ttl) and the absolute max lifetime. The candidate expiry
// is now+ttl, capped at created+maxLifetime. It returns the new expiry and
// whether it should be persisted: persistence is skipped when the gain over the
// current expiry is below slidingRenewInterval (avoiding a write on every
// request) or when the cap has already been reached.
func slideExpiry(created, current, now time.Time, ttl, maxLifetime time.Duration) (time.Time, bool) {
	lifetimeCap := created.Add(maxLifetime)
	candidate := now.Add(ttl)
	if candidate.After(lifetimeCap) {
		candidate = lifetimeCap
	}
	if candidate.Sub(current) < slidingRenewInterval {
		return current, false
	}
	return candidate, true
}
