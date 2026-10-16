package auth

import (
	"errors"
	"time"

	"github.com/panbotka/kukatko/internal/project"
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
	// ErrInvalidRole indicates an account role value outside viewer/curator/editor/admin/maintainer.
	ErrInvalidRole = errors.New("auth: invalid role")
	// ErrMaintainerRequired indicates a non-maintainer tried to grant the
	// maintainer role or to modify an account that already holds it. Only a
	// maintainer may create, promote to, or alter a maintainer account.
	ErrMaintainerRequired = errors.New("auth: only a maintainer may manage the maintainer role")
	// ErrLastMaintainer indicates a change was refused because it would leave the
	// instance without a single enabled maintainer. That state is irreversible
	// through the API — granting the maintainer role is itself reserved to
	// maintainers and there is no delete-user endpoint that would let Bootstrap
	// run again — so every operations surface (backup, restore, import,
	// maintenance, jobs, processing) would need direct database surgery to come
	// back.
	ErrLastMaintainer = errors.New("auth: cannot remove the last maintainer")
	// ErrUserDisabled indicates the account is disabled.
	ErrUserDisabled = errors.New("auth: user is disabled")
	// ErrNotApproved indicates the credentials were right but nobody has let
	// this account in yet (no membership is approved). It is deliberately its own
	// outcome: telling somebody who registered and is waiting that their
	// password is wrong would send them round in circles, and it is not the same
	// as ErrUserDisabled either — one account has never been let in, the other
	// was let in and then shut out. It is only ever returned after the password
	// has been verified, so it reveals nothing to a caller who does not already
	// hold the credentials.
	ErrNotApproved = errors.New("auth: account is waiting for approval")
	// ErrRegistrationClosed indicates self-service registration is not open on
	// this instance: an administrator switched it off, or switched it on without
	// a shared secret, which is the same thing — an open door with no lock is
	// never what they meant.
	ErrRegistrationClosed = errors.New("auth: registration is not open")
	// ErrRegistrationSecret indicates the registration carried the wrong shared
	// secret. It is separate from ErrRegistrationClosed only inside the
	// process; over HTTP both get the same status and the same message, so the
	// answer never tells an open door from a closed one.
	ErrRegistrationSecret = errors.New("auth: wrong registration secret")
	// ErrRegistrationLink indicates the registration named an upload link in
	// place of the shared secret, and the link is unknown, expired or revoked —
	// or this instance wires no upload links at all.
	ErrRegistrationLink = errors.New("auth: the upload link is not valid")
	// ErrRegistrationInvite indicates the registration named an invitation link
	// and the invite is unknown, expired, revoked or used up — or this instance
	// wires no invitations at all. One error for every reason, so registration
	// confirms nothing about which codes exist.
	ErrRegistrationInvite = errors.New("auth: the invitation is not valid")
	// ErrNoteTooLong indicates the user note exceeds MaxNoteLen characters. Its
	// message names the offending field so it can be surfaced verbatim in a 400.
	ErrNoteTooLong = errors.New("auth: note must be at most 1000 characters")
	// ErrSubjectNotFound indicates the subject_uid a create or update carried
	// names no person of the project the link is made in (a person of another
	// project is nobody there). It is a rejected input rather than a
	// missing account, so callers answer it with 400 and not 404.
	ErrSubjectNotFound = errors.New("auth: no such person in the library")
	// ErrNoProject indicates a project-scoped read or write was asked for
	// without naming its project. There is no default project, so it is a
	// programming error, never a client's.
	ErrNoProject = errors.New("auth: no project given")
	// ErrUsernameTooLong indicates the username exceeds MaxUsernameLen
	// characters. Its message names the offending field so it can be surfaced
	// verbatim in a 400.
	ErrUsernameTooLong = errors.New("auth: username must be at most 64 characters")
	// ErrUsernameRequired indicates an account was given a username that is
	// empty once normalized (nothing but whitespace). Nobody could sign in with
	// it, so creating or renaming an account to it is refused. Its message names
	// the offending field so it can be surfaced verbatim in a 400.
	ErrUsernameRequired = errors.New("auth: username is required")
	// ErrInvalidEmail indicates the account was given no e-mail address, or one
	// that is not a syntactically valid mailbox. Every account receives mail —
	// registration, approval, password reset — so an address is not optional.
	// Its message names the offending field so it can be surfaced verbatim in a
	// 400.
	ErrInvalidEmail = errors.New("auth: email must be a valid address")
)

// MaxNoteLen is the maximum length of a user note, measured in runes rather than
// bytes so that accented text is not penalised against the limit.
const MaxNoteLen = 1000

// MaxEmailLen is the maximum length of an e-mail address, in bytes. RFC 5321
// caps a path at 256 octets including the angle brackets, which leaves 254 for
// the address itself; nothing longer can be delivered, so nothing longer is
// worth storing.
const MaxEmailLen = 254

// MaxUsernameLen is the maximum length of a username, measured in runes rather
// than bytes so that accented text is not penalised against the limit. Login
// enforces it before the username reaches the rate limiter, so an unauthenticated
// caller cannot use oversized usernames to inflate the limiter's key set.
const MaxUsernameLen = 64

// User is a local account. PasswordHash is never serialised to clients (no JSON
// tag exposure); the JSON form is used by the HTTP layer for user-management
// responses, which omit the hash.
type User struct {
	UID         string `json:"uid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	// Role is the account's instance role (users.role): user or maintainer.
	Role        Role       `json:"instance_role"`
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
	// ApprovedAt is when an administrator let this account in, or nil when
	// nobody has yet — "registered, waiting". Approval lives on the membership;
	// this is derived from them: the earliest approved membership, or the
	// account's creation for a maintainer (who enters every project anyway) and
	// for an account in no project (waiting for nobody). An
	// account an administrator creates is approved with its membership, so nil
	// is what self-service registration makes until a project admin approves it.
	//
	// It is not the inverse of Disabled and must never be read as one. "Never
	// approved" is an account that has not been let in; "approved and later
	// blocked" is one that was let in and then shut out again. Both are barred
	// from signing in and they mean opposite things to the person holding the
	// account, so every surface that shows one shows the other too.
	ApprovedAt *time.Time `json:"approved_at"`
	// WelcomeSeenAt is when the account's owner last dismissed or completed the
	// first-run welcome (the Markdown an admin writes in internal/settings), or
	// nil when they never have. It is the caller's own fact about themselves and
	// is serialised because the client cannot decide whether to open the welcome
	// without it.
	//
	// Only ever moved forward, and only by the account itself — see
	// Service.MarkWelcomeSeen.
	WelcomeSeenAt *time.Time `json:"welcome_seen_at"`
	// join is the membership a new account is created with by CreateUser and
	// its audited twins — none when its role is empty, which is what the
	// /admin/users creation and a bare seeded account make. It is set only by
	// the seeding vocabulary (CreateUserInput, the bootstrap project) and by
	// self-service registration (the project registered into), and is never
	// read back from the database: the account record carries nothing about a
	// project.
	join newMembership
}

// newMembership is the membership an account is created with: the project, the
// role there (empty: no membership at all) and the person of that project the
// account is linked to (nil: nobody), which checkSubject confirms is one of the
// project's before the insert.
type newMembership struct {
	project string
	role    project.Role
	subject *string
}

// InProject returns u set up to be created as a member of the project
// projectUID with role — the membership Store.CreateUser inserts together with
// the account, approved exactly when u.ApprovedAt says. It is the seeding
// counterpart of what CreateUserInput and registration do, for a caller that
// builds the User itself (the integration-test seeders); an account read never
// carries it, and without it Store.CreateUser creates no membership.
func (u User) InProject(projectUID string, role project.Role) User {
	u.join = newMembership{project: projectUID, role: role}
	return u
}

// HasPassword reports whether the account has a password at all. One created
// for somebody else (a project's first admin, see ProjectSetup) has none until
// its owner follows their set-password link; nothing can sign in with it.
func (u User) HasPassword() bool {
	return u.PasswordHash != ""
}

// LinkedUser is an account as one project sees it: the account record plus the
// person of that project the account says it is ("this is me"), nil when it has
// not said. Who you are is a fact about one project's people, so it lives on the
// membership and never on the account record itself; PUT /p/{project}/auth/subject
// answers with this.
type LinkedUser struct {
	User
	SubjectUID *string `json:"subject_uid"`
}

// Session is an authenticated session bound to a user. Token is the opaque value
// stored in the HttpOnly cookie; DownloadToken authorises media-download URLs
// separately. It caches no role: authorization reads the live account and its
// membership of the request's project on every request.
type Session struct {
	ID            string    `json:"-"`
	Token         string    `json:"-"`
	DownloadToken string    `json:"download_token"`
	UserUID       string    `json:"user_uid"`
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

// governs reports whether u holds the governance privileges of an
// account-wide decision that is not about one project — the rate-limit exemption
// on API tokens, revoking somebody else's token. A token works in every project
// its owner belongs to, so that is the system administrator's (a maintainer's)
// alone: a project's admin governs that project, never the accounts.
func (u User) governs() bool {
	return u.Role.CanMaintain()
}
