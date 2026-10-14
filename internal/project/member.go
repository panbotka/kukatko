package project

import (
	"errors"
	"fmt"
	"time"
)

// MemberTargetType is the audit target type every membership mutation records;
// the target uid is the member's user uid and the details carry the project.
const MemberTargetType = "project_member"

// Role is an account's access level inside one project, on a strict ladder:
// viewer < curator < editor < admin. Each role inherits every permission of the
// roles below it, but Go has no rank arithmetic: every predicate names its roles
// explicitly, so an inserted rung never inherits a permission by accident.
// viewer is read-only; curator adds curation of the catalogue (faces, people,
// albums, labels) without rewriting a photo's own metadata; editor adds write
// access to media and metadata; admin adds the project's governance (its
// members, its audit log, emptying/purging its trash).
//
// There is deliberately no maintainer here: operating the instance is not a
// project role but the instance-wide role on users (internal/auth).
type Role string

const (
	// RoleAdmin is an editor plus the project's governance: its members, its
	// audit log, and emptying or purging its trash. Every project keeps at least
	// one (see ErrLastAdmin).
	RoleAdmin Role = "admin"
	// RoleEditor is a viewer plus write access to media and metadata, sorting and
	// review, and album/label management.
	RoleEditor Role = "editor"
	// RoleCurator is a viewer plus curation of the catalogue — faces, people,
	// albums and labels — without the wider write access that rewrites a photo's
	// own metadata. It satisfies CanCurate but neither CanWrite nor IsAdmin.
	RoleCurator Role = "curator"
	// RoleViewer has read-only access to the project's photos, albums and labels.
	RoleViewer Role = "viewer"
)

var (
	// ErrInvalidRole is returned when a string is not one of the project roles.
	ErrInvalidRole = errors.New("invalid project role")
	// ErrNotMember is returned when the account is not a member of the project.
	ErrNotMember = errors.New("not a member of the project")
	// ErrAlreadyMember is returned by AddMember when the account already is a
	// member; the existing membership is left unchanged.
	ErrAlreadyMember = errors.New("already a member of the project")
	// ErrLastAdmin is returned when a role change or a removal would leave the
	// project without any working admin: an approved member whose account is
	// not disabled (a pending or disabled admin cannot govern it).
	ErrLastAdmin = errors.New("the project must keep at least one admin")
	// ErrUserNotFound is returned when the account a membership names does not exist.
	ErrUserNotFound = errors.New("user not found")
	// ErrSubjectNotFound is returned by SetSubject when the subject does not exist.
	ErrSubjectNotFound = errors.New("subject not found")
)

// ParseRole returns the project role s names, or an error wrapping
// ErrInvalidRole. The instance role "maintainer" is not a project role and is
// refused like any other unknown value.
func ParseRole(s string) (Role, error) {
	r := Role(s)
	if !r.Valid() {
		return "", fmt.Errorf("%w: %q", ErrInvalidRole, s)
	}
	return r, nil
}

// Valid reports whether r is one of the four project roles.
func (r Role) Valid() bool {
	switch r {
	case RoleViewer, RoleCurator, RoleEditor, RoleAdmin:
		return true
	default:
		return false
	}
}

// CanWrite reports whether r is permitted to create or modify content in the
// project. Editors and admins can write; curators and viewers cannot.
func (r Role) CanWrite() bool {
	return r == RoleEditor || r == RoleAdmin
}

// CanCurate reports whether r may curate the project's catalogue — faces,
// people, albums and labels — without the wider write access that rewrites a
// photo's own metadata. A curator qualifies, and so does every role that already
// CanWrite.
func (r Role) CanCurate() bool {
	return r == RoleCurator || r.CanWrite()
}

// IsAdmin reports whether r holds the project's governance privileges (its
// members, its audit log, emptying/purging its trash). Only an admin qualifies.
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

// Member is one row of project_members joined with the account it belongs to.
type Member struct {
	// ProjectUID is the project the membership is in.
	ProjectUID string `json:"project_uid"`
	// UserUID is the member's account.
	UserUID string `json:"user_uid"`
	// Role is the member's role inside the project.
	Role Role `json:"role"`
	// ApprovedAt is when a project admin approved the member, or nil while the
	// membership still waits for approval.
	ApprovedAt *time.Time `json:"approved_at"`
	// SubjectUID is the person in this project the account says it is ("this is
	// me"), or nil.
	SubjectUID *string `json:"subject_uid"`
	// LastSeenAt is the member's last visit to the project, or nil.
	LastSeenAt *time.Time `json:"last_seen_at"`
	// VisitReferenceAt is the moment "what's new" counts from, or nil.
	VisitReferenceAt *time.Time `json:"visit_reference_at"`
	// TaskDigestAt is when the last task digest of this project was scheduled
	// for the member, or nil.
	TaskDigestAt *time.Time `json:"task_digest_at"`
	// CreatedAt is when the membership was created.
	CreatedAt time.Time `json:"created_at"`
	// Username is the account's login name (joined from users).
	Username string `json:"username"`
	// DisplayName is the account's display name, possibly empty (joined from users).
	DisplayName string `json:"display_name"`
	// LastLoginAt is the account's last sign-in, or nil (joined from users).
	LastLoginAt *time.Time `json:"last_login_at"`
	// HasPicture reports whether the account has a profile picture worth asking
	// GET /users/{uid}/avatar for: an uploaded or picked one, or a person this
	// membership links, whose face stands in. A linked person without a usable
	// face still answers 404 there, so a client keeps its initial as the
	// fallback.
	HasPicture bool `json:"has_picture"`
	// IdentityWithheld reports a membership made by an exact-match add
	// (AddByIdentifier) whose account has not entered the project yet. Until it
	// does, Username and DisplayName are empty, LastLoginAt is nil and
	// HasPicture false, and the account cannot be named in the project
	// (ApprovedMemberSQL): an add must not tell the admin who the account is.
	IdentityWithheld bool `json:"identity_withheld"`
}

// MembershipSummary is one project an account belongs to, as the project
// chooser lists it.
type MembershipSummary struct {
	// ProjectUID is the project's uid.
	ProjectUID string `json:"project_uid"`
	// Slug is the project's URL segment.
	Slug string `json:"slug"`
	// Name is the project's name.
	Name string `json:"name"`
	// Active is false for a deactivated project.
	Active bool `json:"active"`
	// Role is the account's role in the project.
	Role Role `json:"role"`
	// Approved reports whether a project admin approved the membership.
	Approved bool `json:"approved"`
	// SubjectUID is the person in this project the account says it is ("this
	// is me"), or nil when it has not said.
	SubjectUID *string `json:"subject_uid"`
	// SubjectName is that person's name. Only AllMemberships (the system
	// administrator's account list) fills it; MembershipsOf leaves it empty —
	// the account resolves its own person inside the project.
	SubjectName string `json:"subject_name,omitempty"`
}
