// Package invite is the domain of invitation links: a short link a project
// admin creates and sends to somebody, through which that person joins the
// project — signed in, by accepting it, or anonymous, by registering through it.
//
// An invite carries a project role, an expiry (DefaultDays unless asked
// otherwise, never more than MaxDays) and an optional cap on how many times it
// may be used. Its short code (/invite/<code>) is the whole credential and is
// handled exactly like an upload link's (internal/uploadlink, whose code
// helpers it reuses): looked up through its SHA-256 (code_hash) and kept
// readable (code) so the project's admins can copy the URL again from the list.
// Revocation is final.
//
// Every lookup by code answers one error, ErrInvalid, for an unknown, expired,
// revoked or used-up invite — and for one of a deactivated project — so a
// guesser learns nothing about which codes exist or why one stopped working.
//
// Accepting never lowers a role: a member below the invite's role is raised to
// it, one at or above it keeps what it has, and accepting an invite whose grant
// one already holds changes nothing and counts no use, which makes a repeated
// accept idempotent. Registering through an invite (RedeemTx, on the account's
// transaction) grants at most RegistrationRole — an invite never mints an admin
// account. Every create, revoke and accept writes its audit entry in the
// mutation's transaction.
//
// The package owns one table, project_invites (migration 0109). It holds no
// HTTP code (internal/inviteapi) and creates no accounts (internal/auth).
package invite

import (
	"errors"
	"time"

	"github.com/panbotka/kukatko/internal/project"
)

// TargetType is the audit target type of every invite mutation.
const TargetType = "project_invites"

// Validity bounds of an invite, in days.
const (
	// DefaultDays is the validity of an invite created without one.
	DefaultDays = 7
	// MaxDays is the longest validity an invite may have.
	MaxDays = 90
)

// Errors the store returns. The HTTP layer maps them onto status codes.
var (
	// ErrInvalid means a code names no usable invite: unknown, expired,
	// revoked, used up, or of a deactivated project — deliberately one answer.
	ErrInvalid = errors.New("invite: the invitation is not valid")
	// ErrNotFound means no invite of the project has this UID (another
	// project's is answered the same).
	ErrNotFound = errors.New("invite: invitation not found")
	// ErrNoProject means a project-scoped call named no project.
	ErrNoProject = errors.New("invite: no project given")
	// ErrInvalidRole means the requested role is not a project role.
	ErrInvalidRole = errors.New("invite: invalid role")
	// ErrInvalidDays means the requested validity is outside 1..MaxDays.
	ErrInvalidDays = errors.New("invite: validity must be between 1 and 90 days")
	// ErrInvalidMaxUses means a use cap below one was requested.
	ErrInvalidMaxUses = errors.New("invite: max_uses must be at least 1")
)

// State is where an invite stands in its life.
type State string

const (
	// StateActive means the invite may be accepted.
	StateActive State = "active"
	// StateExpired means its expiry passed.
	StateExpired State = "expired"
	// StateRevoked means an admin revoked it for good.
	StateRevoked State = "revoked"
	// StateUsedUp means it was used as many times as its cap allows.
	StateUsedUp State = "used_up"
)

// Invite is one invitation link as the admin's list shows it.
type Invite struct {
	// UID is the primary key, "iv" followed by random base32 characters. It is
	// not the code: the code is the credential, the UID merely names the row.
	UID string `json:"uid"`
	// Role is what accepting grants.
	Role project.Role `json:"role"`
	// ExpiresAt is when the invite stops working.
	ExpiresAt time.Time `json:"expires_at"`
	// MaxUses caps how many accepts the invite grants; nil is no cap.
	MaxUses *int `json:"max_uses"`
	// Uses is how many accepts it granted so far.
	Uses int `json:"uses"`
	// RevokedAt is when it was revoked, nil while it is not.
	RevokedAt *time.Time `json:"revoked_at"`
	// CreatedBy is the creator's account UID, nil once the account is gone.
	CreatedBy *string `json:"created_by"`
	// CreatorName is the creator's display name (or username), empty when the
	// account is gone — and when it is no member of the invite's project any
	// more, which CreatorFormer reports (project.FormerMemberSQL).
	CreatorName   string    `json:"created_by_name"`
	CreatorFormer bool      `json:"created_by_former"`
	CreatedAt     time.Time `json:"created_at"`
	// Code is the readable short code. It is never serialised from here: the
	// HTTP layer shows it only to the project's admins.
	Code string `json:"-"`
	// ProjectUID, ProjectSlug and ProjectName name the invite's project. A
	// public lookup by code learns the project from here.
	ProjectUID  string `json:"-"`
	ProjectSlug string `json:"-"`
	ProjectName string `json:"-"`
	// projectActive is whether the project is active; an invite of a
	// deactivated project is answered like an unknown one.
	projectActive bool
}

// StateAt reports the invite's state at now: revoked wins over expired, and
// both over used up, because a dead invite stays dead whatever its counter says.
func (i Invite) StateAt(now time.Time) State {
	switch {
	case i.RevokedAt != nil:
		return StateRevoked
	case !now.Before(i.ExpiresAt):
		return StateExpired
	case i.usedUp():
		return StateUsedUp
	default:
		return StateActive
	}
}

// usedUp reports whether the invite reached its use cap.
func (i Invite) usedUp() bool {
	return i.MaxUses != nil && i.Uses >= *i.MaxUses
}

// openAt reports whether the invite may still be honoured at now apart from its
// use cap: not revoked, not expired, and its project active. A repeated accept
// of a grant already held is answered on this alone, so it stays idempotent
// once the cap is reached.
func (i Invite) openAt(now time.Time) bool {
	return i.RevokedAt == nil && now.Before(i.ExpiresAt) && i.projectActive
}

// NewInvite is what a project admin asks for when creating an invite.
type NewInvite struct {
	// Role is what accepting grants (required).
	Role project.Role
	// Days is the validity in days; 0 is DefaultDays.
	Days int
	// MaxUses caps the accepts; nil is no cap.
	MaxUses *int
	// CreatedBy is the creating account's UID (required).
	CreatedBy string
}

// Validate checks in for what Create refuses: an invalid role, a validity
// outside 1..MaxDays (0 means DefaultDays) or a use cap below one. It is
// exported so the HTTP layer can answer before it opens a transaction.
func (in NewInvite) Validate() error {
	switch {
	case !in.Role.Valid():
		return ErrInvalidRole
	case in.Days < 0 || in.Days > MaxDays:
		return ErrInvalidDays
	case in.MaxUses != nil && *in.MaxUses < 1:
		return ErrInvalidMaxUses
	}
	return nil
}

// days returns the validity in days, DefaultDays when none was asked for.
func (in NewInvite) days() int {
	if in.Days == 0 {
		return DefaultDays
	}
	return in.Days
}

// Acceptance is the outcome of Accept.
type Acceptance struct {
	// ProjectUID and ProjectSlug name the project joined; the client navigates
	// to /p/<slug>/.
	ProjectUID  string
	ProjectSlug string
	// Role is the account's role in the project after accepting.
	Role project.Role
	// Changed reports whether accepting granted anything (a new membership, a
	// raised role or an approval) and so counted a use.
	Changed bool
}

// RegistrationRole is the role an account registered through an invite of role
// gets: the invite's role capped at editor. Registration never mints an admin —
// an admin invite grants admin only to an existing account that accepts it
// signed in.
func RegistrationRole(role project.Role) project.Role {
	if role.IsAdmin() {
		return project.RoleEditor
	}
	return role
}

// rank orders the project roles for the one comparison accepting needs: is the
// role a member already holds at least the invite's? The ladder is
// viewer < curator < editor < admin (see project.Role); an unknown role ranks
// below all of them.
func rank(role project.Role) int {
	switch role {
	case project.RoleViewer:
		return 1
	case project.RoleCurator:
		return 2
	case project.RoleEditor:
		return 3
	case project.RoleAdmin:
		return 4
	default:
		return 0
	}
}

// higher returns whichever of a and b ranks higher, a on a tie.
func higher(a, b project.Role) project.Role {
	if rank(b) > rank(a) {
		return b
	}
	return a
}
