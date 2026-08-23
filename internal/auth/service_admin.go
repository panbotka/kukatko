package auth

import (
	"context"
	"log"
	"time"
	"unicode/utf8"

	"github.com/panbotka/kukatko/internal/audit"
)

// BootstrapOutcome reports what Bootstrap did, so the caller can log an
// appropriate message.
type BootstrapOutcome int

const (
	// BootstrapSkippedHasUsers means the users table was non-empty; nothing done.
	BootstrapSkippedHasUsers BootstrapOutcome = iota
	// BootstrapSkippedNoCredentials means the table was empty but no bootstrap
	// username/password was configured; nothing done (caller should warn).
	BootstrapSkippedNoCredentials
	// BootstrapCreated means the initial maintainer account was created.
	BootstrapCreated
)

// CreateUserInput holds the fields needed to create a user (admin-only).
// DisplayName and Note are both optional and default to the empty string; Email
// is not — every account receives mail, so a syntactically valid address is
// required (see validateEmail).
//
// Its field order and types mirror createUserRequest so the HTTP layer can
// convert between them directly; keep the two in step.
type CreateUserInput struct {
	Username    string
	Password    string
	DisplayName string
	Email       string
	Role        Role
	Note        string
	// SubjectUID optionally names the person of the library this account is;
	// nil (or empty) leaves the account unlinked, which is the default.
	SubjectUID *string
}

// UpdateUserInput holds the mutable profile fields for an admin user update.
// Note is a pointer to distinguish "absent" from "empty": nil leaves the stored
// note untouched, while a pointer to "" clears it. Email has no such escape: the
// update replaces the whole profile and an account may not end up without an
// address, so every update carries a valid one.
//
// Its field order and types mirror updateUserRequest so the HTTP layer can
// convert between them directly; keep the two in step.
type UpdateUserInput struct {
	DisplayName string
	Email       string
	Role        Role
	Disabled    bool
	Note        *string
	// SubjectUID is the person of the library this account is. Unlike Note it is
	// part of the profile the update *replaces*, so nil clears the link — an
	// administrator who unlinks somebody does it by sending no subject, exactly
	// as they clear a display name by sending none.
	SubjectUID *string
}

// validateNote returns ErrNoteTooLong when note exceeds MaxNoteLen. Length is
// measured in runes rather than bytes so that a note of accented characters is
// judged by the same limit as an ASCII one.
func validateNote(note string) error {
	if utf8.RuneCountInString(note) > MaxNoteLen {
		return ErrNoteTooLong
	}
	return nil
}

// Bootstrap creates the first account when the users table is empty and a
// username and password are both provided. The account is created as a
// maintainer — the top of the role ladder — so the instance always has a root
// that can grant the maintainer role to others. It is given a placeholder e-mail
// address (see placeholderEmail), the one account allowed to start without a
// real one. It returns
// BootstrapSkippedHasUsers when users already exist,
// BootstrapSkippedNoCredentials when credentials are missing, or
// BootstrapCreated on success; errors are returned wrapped.
func (s *Service) Bootstrap(ctx context.Context, username, password string) (BootstrapOutcome, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return BootstrapSkippedHasUsers, err
	}
	if count > 0 {
		return BootstrapSkippedHasUsers, nil
	}
	if username == "" || password == "" {
		return BootstrapSkippedNoCredentials, nil
	}
	if _, err := s.CreateUser(ctx, CreateUserInput{
		Username:    username,
		Password:    password,
		DisplayName: username,
		// Nobody has been able to configure an address yet — the database is
		// empty and this account is what unlocks the admin area — so the
		// bootstrap maintainer gets an undeliverable placeholder and changes it
		// from there. A first start must not need a mailbox.
		Email: placeholderEmail(normalizeUsername(username)),
		Role:  RoleMaintainer,
	}); err != nil {
		return BootstrapSkippedNoCredentials, err
	}
	return BootstrapCreated, nil
}

// authorizeUserManagement enforces the maintainer boundary on a user-management
// action taken by an actor of role actor: granting the maintainer role (newRole
// is maintainer) or touching an account that already holds it (current is
// maintainer) is reserved to maintainers, while every other viewer/editor/admin
// action is allowed. A zero current ("") means a creation, where only newRole
// matters. It returns ErrMaintainerRequired when the boundary is crossed.
func authorizeUserManagement(actor, current, newRole Role) error {
	if actor.CanMaintain() {
		return nil
	}
	if newRole == RoleMaintainer || current == RoleMaintainer {
		return ErrMaintainerRequired
	}
	return nil
}

// guardMaintainerBoundary applies authorizeUserManagement for an action by actor
// against the existing account uid, which the action would leave with role
// newRole. It is a no-op for a maintainer actor (who may manage any role); for a
// lower actor it loads the target's current role and rejects touching or
// granting the maintainer role. A zero uid ("") means a creation, so no lookup
// happens and only newRole is checked. A newRole that is not RoleMaintainer (for
// example when disabling or resetting a password) leaves the check to rest on the
// target's current role alone. Store errors (including ErrUserNotFound) propagate.
func (s *Service) guardMaintainerBoundary(ctx context.Context, actor Role, uid string, newRole Role) error {
	if actor.CanMaintain() {
		return nil
	}
	var current Role
	if uid != "" {
		existing, err := s.store.GetUserByUID(ctx, uid)
		if err != nil {
			return err
		}
		current = existing.Role
	}
	return authorizeUserManagement(actor, current, newRole)
}

// CreateUser validates and inserts a new user, hashing the supplied password. It
// records no audit entry and is used for system-initiated creation (bootstrap,
// test seeding); handlers that must attribute the action to an admin call
// CreateUserAudited. It returns ErrInvalidRole for an unknown role,
// ErrPasswordTooShort for a weak password, ErrUsernameTooLong or ErrNoteTooLong
// for an over-length username or note, ErrInvalidEmail for a missing or
// malformed e-mail address, ErrUsernameTaken on a duplicate username, and the
// created user on success.
func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (User, error) {
	user, err := s.prepareNewUser(in)
	if err != nil {
		return User{}, err
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return User{}, err
	}
	return s.store.GetUserByUID(ctx, user.UID)
}

// CreateUserAudited creates a user like CreateUser and writes a user.create audit
// entry attributed to entry's actor in the same transaction as the insert (see
// internal/audit). The created user's username and role are recorded in the
// entry's details, and its UID becomes the entry's target. actor is the role of
// the account performing the creation: only a maintainer may create an account
// with the maintainer role, so a lower actor granting it gets ErrMaintainerRequired.
func (s *Service) CreateUserAudited(
	ctx context.Context, in CreateUserInput, actor Role, entry audit.Entry,
) (User, error) {
	if err := s.guardMaintainerBoundary(ctx, actor, "", in.Role); err != nil {
		return User{}, err
	}
	user, err := s.prepareNewUser(in)
	if err != nil {
		return User{}, err
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["username"] = user.Username
	entry.Details["role"] = string(user.Role)
	if uid := normalizeSubjectUID(user.SubjectUID); uid != nil {
		// Only when it was actually set: an account created without a link has
		// nothing to say here, and a null in every entry reads as noise.
		entry.Details["subject_uid"] = *uid
	}
	if err := s.store.CreateUserAudited(ctx, user, entry); err != nil {
		return User{}, err
	}
	return s.store.GetUserByUID(ctx, user.UID)
}

// prepareNewUser validates in and builds the User to insert, hashing the password
// and assigning a fresh UID. It is shared by CreateUser and CreateUserAudited and
// returns ErrInvalidRole, ErrUsernameTooLong, ErrPasswordTooShort,
// ErrNoteTooLong or ErrInvalidEmail on invalid input.
func (s *Service) prepareNewUser(in CreateUserInput) (User, error) {
	if !in.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	username := normalizeUsername(in.Username)
	// Login rejects an over-long username outright, so an account with one
	// could never be used; refuse to create it in the first place.
	if err := validateUsername(username); err != nil {
		return User{}, err
	}
	if err := validateNote(in.Note); err != nil {
		return User{}, err
	}
	email := normalizeEmail(in.Email)
	if err := validateEmail(email); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	uid, err := newUserUID()
	if err != nil {
		return User{}, err
	}
	return User{
		UID:          uid,
		Username:     username,
		DisplayName:  in.DisplayName,
		Email:        email,
		Role:         in.Role,
		PasswordHash: hash,
		Note:         in.Note,
		SubjectUID:   in.SubjectUID,
		ApprovedAt:   s.approvedNow(),
	}, nil
}

// approvedNow returns the approval stamp for an account being created here.
//
// Every path through prepareNewUser is an administrator making an account — the
// admin API, and the bootstrap that creates the first maintainer on an empty
// database — and an administrator creating an account *is* the approval, so
// there is nothing further to wait for. Self-service registration, when it
// arrives, is the case that must not come through here: an account nobody has
// approved yet is stored with a NULL approved_at and only an administrator's
// later decision fills it.
func (s *Service) approvedNow() *time.Time {
	at := s.now()
	return &at
}

// MarkWelcomeSeen records that the account identified by uid has seen the
// first-run welcome, and returns the refreshed user. The stamp is written only
// once: a repeat call is harmless and leaves the original time in place, so the
// client may send it as often as it likes.
//
// Like SetUserSubject it is self-service and therefore unaudited — the trail
// records what was done *to* an account by somebody else, and closing one's own
// welcome is nobody else's action. It returns ErrUserNotFound when the account
// no longer exists.
func (s *Service) MarkWelcomeSeen(ctx context.Context, uid string) (User, error) {
	return s.store.MarkWelcomeSeen(ctx, uid, s.now())
}

// SetUserSubject records which person of the library the account identified by
// uid is, or clears the link when subjectUID is nil or empty. It is the
// self-service path behind the account page, so it records no audit entry — the
// same bargain Service.ChangePassword strikes for a user acting on their own
// account; the administrator's path through UpdateUserAudited is audited.
//
// It returns the refreshed user, ErrUserNotFound when the account does not
// exist, or ErrSubjectNotFound when the UID names nobody.
func (s *Service) SetUserSubject(ctx context.Context, uid string, subjectUID *string) (User, error) {
	return s.store.SetUserSubject(ctx, uid, subjectUID)
}

// ListUsers returns every user ordered by username.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.store.ListUsers(ctx)
}

// GetUser returns the user identified by uid, or ErrUserNotFound.
func (s *Service) GetUser(ctx context.Context, uid string) (User, error) {
	return s.store.GetUserByUID(ctx, uid)
}

// UpdateUser updates a user's profile fields without recording an audit entry;
// handlers use UpdateUserAudited. When the update disables the account, all of
// that user's sessions are invalidated so the change takes effect immediately. A
// nil in.Note leaves the stored note untouched. It returns ErrInvalidRole for an
// unknown role, ErrInvalidEmail for a missing or malformed e-mail address,
// ErrNoteTooLong for an over-length note, ErrUserNotFound if no such user
// exists, and ErrLastMaintainer when demoting or disabling the account would
// leave the instance without a single enabled maintainer.
func (s *Service) UpdateUser(ctx context.Context, uid string, in UpdateUserInput) (User, error) {
	in, err := validateUserUpdate(in)
	if err != nil {
		return User{}, err
	}
	user, err := s.store.UpdateUserProfile(ctx, uid, in)
	if err != nil {
		return User{}, err
	}
	return s.invalidateIfDisabled(ctx, uid, in.Disabled, user)
}

// UpdateUserAudited updates a user's profile fields like UpdateUser and writes a
// user.update audit entry attributed to entry's actor in the same transaction as
// the change (see internal/audit). actor is the role of the account performing
// the update: only a maintainer may promote an account to, or modify an account
// that already holds, the maintainer role, so a lower actor doing so gets
// ErrMaintainerRequired. A maintainer is still refused, with ErrLastMaintainer,
// an update that would demote or disable the last enabled maintainer — including
// their own account.
func (s *Service) UpdateUserAudited(
	ctx context.Context, uid string, in UpdateUserInput, actor Role, entry audit.Entry,
) (User, error) {
	if err := s.guardMaintainerBoundary(ctx, actor, uid, in.Role); err != nil {
		return User{}, err
	}
	in, err := validateUserUpdate(in)
	if err != nil {
		return User{}, err
	}
	user, err := s.store.UpdateUserProfileAudited(ctx, uid, in, entry)
	if err != nil {
		return User{}, err
	}
	return s.invalidateIfDisabled(ctx, uid, in.Disabled, user)
}

// validateUserUpdate validates the role, e-mail address and optional note of an
// update input and returns the input with the address normalized, so what the
// caller stores is what was validated. A nil note skips the note check. It
// returns ErrInvalidRole, ErrInvalidEmail or ErrNoteTooLong.
func validateUserUpdate(in UpdateUserInput) (UpdateUserInput, error) {
	if !in.Role.Valid() {
		return in, ErrInvalidRole
	}
	in.Email = normalizeEmail(in.Email)
	if err := validateEmail(in.Email); err != nil {
		return in, err
	}
	if in.Note != nil {
		if err := validateNote(*in.Note); err != nil {
			return in, err
		}
	}
	return in, nil
}

// SetUserDisabled enables or disables the user identified by uid without
// recording an audit entry; handlers use SetUserDisabledAudited. Disabling also
// invalidates all of that user's sessions so the lockout is immediate. It returns
// the refreshed user, ErrUserNotFound if no such user exists, or
// ErrLastMaintainer when disabling would leave the instance without a single
// enabled maintainer.
func (s *Service) SetUserDisabled(ctx context.Context, uid string, disabled bool) (User, error) {
	user, err := s.store.SetUserDisabled(ctx, uid, disabled)
	if err != nil {
		return User{}, err
	}
	return s.invalidateIfDisabled(ctx, uid, disabled, user)
}

// SetUserDisabledAudited enables or disables a user like SetUserDisabled and
// writes a user.disable audit entry attributed to entry's actor in the same
// transaction as the change (see internal/audit). actor is the role of the
// account performing the change: only a maintainer may disable or re-enable a
// maintainer account, so a lower actor gets ErrMaintainerRequired. A maintainer
// is still refused, with ErrLastMaintainer, a disable that would take the last
// enabled maintainer — including their own account — off the instance.
func (s *Service) SetUserDisabledAudited(
	ctx context.Context, uid string, disabled bool, actor Role, entry audit.Entry,
) (User, error) {
	if err := s.guardMaintainerBoundary(ctx, actor, uid, ""); err != nil {
		return User{}, err
	}
	user, err := s.store.SetUserDisabledAudited(ctx, uid, disabled, entry)
	if err != nil {
		return User{}, err
	}
	return s.invalidateIfDisabled(ctx, uid, disabled, user)
}

// invalidateIfDisabled deletes all of the user's sessions when disabled is true,
// so a disable takes effect immediately, and returns user unchanged. Re-enabling
// (disabled false) leaves existing sessions alone. Shared by the plain and
// audited update/disable paths.
func (s *Service) invalidateIfDisabled(ctx context.Context, uid string, disabled bool, user User) (User, error) {
	if disabled {
		if _, err := s.store.DeleteUserSessions(ctx, uid); err != nil {
			return User{}, err
		}
	}
	return user, nil
}

// ResetPassword sets a new password for the user identified by uid and
// invalidates all of that user's sessions, without recording an audit entry;
// handlers use ResetPasswordAudited. It returns ErrPasswordTooShort for a weak
// password and ErrUserNotFound if no such user exists.
func (s *Service) ResetPassword(ctx context.Context, uid, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetPasswordHash(ctx, uid, hash); err != nil {
		return err
	}
	return s.invalidateSessions(ctx, uid)
}

// ResetPasswordAudited sets a new password like ResetPassword and writes a
// user.password audit entry attributed to entry's actor in the same transaction
// as the change (see internal/audit). actor is the role of the account
// performing the reset: only a maintainer may reset a maintainer account's
// password, so a lower actor gets ErrMaintainerRequired.
func (s *Service) ResetPasswordAudited(
	ctx context.Context, uid, newPassword string, actor Role, entry audit.Entry,
) error {
	if err := s.guardMaintainerBoundary(ctx, actor, uid, ""); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetPasswordHashAudited(ctx, uid, hash, entry); err != nil {
		return err
	}
	return s.invalidateSessions(ctx, uid)
}

// invalidateSessions deletes every session of the user identified by uid so a
// password change locks out other sessions immediately.
func (s *Service) invalidateSessions(ctx context.Context, uid string) error {
	if _, err := s.store.DeleteUserSessions(ctx, uid); err != nil {
		return err
	}
	return nil
}

// RunCleanup periodically deletes expired sessions until ctx is canceled. It is
// meant to run in its own goroutine; the interval is typically one hour. Cleanup
// errors are logged and do not stop the loop.
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := s.CleanupExpiredSessions(ctx); err != nil {
				log.Printf("auth: session cleanup failed: %v", err)
			} else if n > 0 {
				log.Printf("auth: cleaned up %d expired session(s)", n)
			}
		}
	}
}
