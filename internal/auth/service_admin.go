package auth

import (
	"context"
	"log"
	"time"
	"unicode/utf8"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/project"
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

// CreateUserInput is the seeding shape of an account: the fields of
// NewAccountInput with the role in the five-rung AccountRole vocabulary, which
// Service.CreateUser turns into the instance role plus a membership of the
// bootstrap project (see AccountRole). Bootstrap and the integration-test
// seeders use it; no HTTP route does. DisplayName and Note are both optional and
// default to the empty string; Email is not — every account receives mail, so a
// syntactically valid address is required (see validateEmail).
type CreateUserInput struct {
	Username    string
	Password    string
	DisplayName string
	Email       string
	// Role is the account's role in the five-rung seeding vocabulary:
	// maintainer, or the role in the bootstrap project (see AccountRole.Split).
	Role AccountRole
	Note string
	// SubjectUID optionally names the person of the bootstrap project this
	// account is, on that membership; nil (or empty) leaves it unlinked.
	SubjectUID *string
}

// NewAccountInput holds the fields of an account the system administrator
// creates (POST /admin/users). It is the account record only: the account is
// created with no membership at all, and joins a project when that project's
// admin adds it, or when it accepts an invitation. DisplayName and Note are
// optional; Email is required (see validateEmail). An empty InstanceRole is
// RoleUser.
//
// Its field order and types mirror createUserRequest so the HTTP layer can
// convert between them directly; keep the two in step.
type NewAccountInput struct {
	Username     string
	Password     string
	DisplayName  string
	Email        string
	InstanceRole Role
	Note         string
}

// UpdateUserInput holds the mutable profile fields for an admin user update —
// the account record, never a membership. Note is a pointer to distinguish
// "absent" from "empty": nil leaves the stored note untouched, while a pointer
// to "" clears it. Email has no such escape: the update replaces the whole
// profile and an account may not end up without an address, so every update
// carries a valid one.
//
// Its field order and types mirror updateUserRequest so the HTTP layer can
// convert between them directly; keep the two in step.
type UpdateUserInput struct {
	DisplayName string
	Email       string
	// InstanceRole replaces the account's instance role: user or maintainer.
	InstanceRole Role
	Disabled     bool
	Note         *string
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
		Role:  AccountMaintainer,
	}); err != nil {
		return BootstrapSkippedNoCredentials, err
	}
	return BootstrapCreated, nil
}

// authorizeUserManagement enforces the maintainer boundary on a user-management
// action taken by an actor of role actor: granting the maintainer role (newRole
// is maintainer) or touching an account that already holds it (current is
// maintainer) is reserved to maintainers, while every other
// viewer/curator/editor/admin action is allowed. A zero current ("") means a
// creation, where only newRole matters. It returns ErrMaintainerRequired when the boundary is crossed.
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
// test seeding) in the seeding vocabulary of CreateUserInput, so the account is
// created with its membership of the bootstrap project; the system
// administrator's audited path is CreateUserAudited. It returns ErrInvalidRole
// for an unknown role,
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

// CreateUserAudited creates the account described by in — with no membership
// at all — and writes a user.create audit entry attributed to entry's actor in
// the same transaction as the insert (see internal/audit). The created
// account's username and instance role are recorded in the entry's details, and
// its UID becomes the entry's target. actor is the instance role of the account
// performing the creation: only a maintainer may create an account with the
// maintainer role, so a lower actor granting it gets ErrMaintainerRequired. It
// returns the validation errors of prepareAccount and ErrUsernameTaken.
func (s *Service) CreateUserAudited(
	ctx context.Context, in NewAccountInput, actor Role, entry audit.Entry,
) (User, error) {
	user, err := s.prepareAccount(in)
	if err != nil {
		return User{}, err
	}
	if err := s.guardMaintainerBoundary(ctx, actor, "", user.Role); err != nil {
		return User{}, err
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["username"] = user.Username
	entry.Details["instance_role"] = string(user.Role)
	if err := s.store.CreateUserAudited(ctx, user, entry); err != nil {
		return User{}, err
	}
	return s.store.GetUserByUID(ctx, user.UID)
}

// prepareNewUser validates the seeding input in and builds the User to insert:
// the account of prepareAccount plus, for a role below maintainer as for a
// maintainer, its membership of the bootstrap project (see AccountRole.Split).
// It returns ErrInvalidRole for a role outside the five rungs, and
// prepareAccount's validation errors.
func (s *Service) prepareNewUser(in CreateUserInput) (User, error) {
	if !in.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	instance, inProject := in.Role.Split()
	user, err := s.prepareAccount(NewAccountInput{
		Username: in.Username, Password: in.Password, DisplayName: in.DisplayName, Email: in.Email,
		InstanceRole: instance, Note: in.Note,
	})
	if err != nil {
		return User{}, err
	}
	user.join = newMembership{project: project.BootstrapUID, role: inProject, subject: in.SubjectUID}
	return user, nil
}

// prepareAccount validates in and builds the account to insert — no
// membership — hashing the password, assigning a fresh UID and stamping the
// approval (an administrator creating an account is the approval, see
// approvedNow). An empty instance role is RoleUser. It returns ErrInvalidRole,
// ErrUsernameRequired, ErrUsernameTooLong, ErrPasswordTooShort, ErrNoteTooLong
// or ErrInvalidEmail on invalid input.
func (s *Service) prepareAccount(in NewAccountInput) (User, error) {
	if in.InstanceRole == "" {
		in.InstanceRole = RoleUser
	}
	if !in.InstanceRole.Valid() {
		return User{}, ErrInvalidRole
	}
	username := normalizeUsername(in.Username)
	// Login rejects an over-long username outright and nobody can type an
	// empty one, so an account with either could never be used; refuse to
	// create it in the first place.
	if err := validateAccountUsername(username); err != nil {
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
		Role:         in.InstanceRole,
		PasswordHash: hash,
		Note:         in.Note,
		ApprovedAt:   s.approvedNow(),
	}, nil
}

// prepareRegistration builds the account a self-service registration creates:
// the same validation, normalization and password hashing as prepareAccount, a
// plain user joining projectUID with role, minus the approval. The nil
// ApprovedAt is the whole point — nobody has let this person in yet, and only an
// administrator's later decision fills it — so it is stripped here rather than
// never set, which keeps every rule about usernames, addresses and passwords in
// one place. An invitation, which is that decision made in advance, puts the
// stamp back.
func (s *Service) prepareRegistration(in NewAccountInput, projectUID string, role project.Role) (User, error) {
	in.InstanceRole = RoleUser
	user, err := s.prepareAccount(in)
	if err != nil {
		return User{}, err
	}
	user.ApprovedAt = nil
	user.join = newMembership{project: projectUID, role: role}
	return user, nil
}

// approvedNow returns the approval stamp for an account being created here.
//
// Every path through prepareAccount is an administrator making an account — the
// admin API, and the bootstrap that creates the first maintainer on an empty
// database — and an administrator creating an account *is* the approval, so
// there is nothing further to wait for. The one caller that must not keep the
// stamp is self-service registration, which goes through prepareRegistration and
// strips it again: an account nobody has approved yet gets a membership with a
// NULL approved_at.
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
// uid is in the project projectUID — on its membership there — or clears the
// link when subjectUID is nil or empty. It is the self-service path behind the
// account page, so it records no audit entry — the same bargain
// Service.ChangePassword strikes for a user acting on their own account; the
// administrative write (internal/project's Store.SetSubject) is audited.
//
// It returns the account with that project's link, ErrUserNotFound when the
// account does not exist, ErrSubjectNotFound when the UID names nobody, or
// ErrNoProject when projectUID is empty.
func (s *Service) SetUserSubject(
	ctx context.Context, projectUID, uid string, subjectUID *string,
) (LinkedUser, error) {
	return s.store.SetUserSubject(ctx, projectUID, uid, subjectUID)
}

// ListUsers returns the users matching filter, ordered by username. A zero
// filter is every account.
func (s *Service) ListUsers(ctx context.Context, filter UserFilter) ([]User, error) {
	return s.store.ListUsers(ctx, filter)
}

// GetUser returns the user identified by uid, or ErrUserNotFound.
func (s *Service) GetUser(ctx context.Context, uid string) (User, error) {
	return s.store.GetUserByUID(ctx, uid)
}

// UpdateUser updates a user's profile fields without recording an audit entry;
// handlers use UpdateUserAudited. When the update disables the account, all of
// that user's sessions are invalidated so the change takes effect immediately. A
// nil in.Note leaves the stored note untouched. It returns ErrInvalidRole for an
// unknown instance role, ErrInvalidEmail for a missing or malformed e-mail address,
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
	if err := s.guardMaintainerBoundary(ctx, actor, uid, in.InstanceRole); err != nil {
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

// validateUserUpdate validates the instance role, e-mail address and optional
// note of an update input and returns the input with the address normalized, so
// what the caller stores is what was validated. A nil note skips the note
// check. It returns ErrInvalidRole, ErrInvalidEmail or ErrNoteTooLong.
func validateUserUpdate(in UpdateUserInput) (UpdateUserInput, error) {
	if !in.InstanceRole.Valid() {
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

// RunCleanup periodically deletes expired sessions and the password-reset links
// that can no longer be used until ctx is canceled. It is meant to run in its own
// goroutine; the interval is typically one hour. Cleanup errors are logged and do
// not stop the loop, and neither half stops the other from running.
func (s *Service) RunCleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupOnce(ctx)
		}
	}
}

// cleanupOnce runs both prunes of one tick, logging what each removed. It is
// separate from the loop so a test can exercise it without a ticker.
func (s *Service) cleanupOnce(ctx context.Context) {
	if n, err := s.CleanupExpiredSessions(ctx); err != nil {
		log.Printf("auth: session cleanup failed: %v", err)
	} else if n > 0 {
		log.Printf("auth: cleaned up %d expired session(s)", n)
	}
	if n, err := s.CleanupFinishedPasswordResets(ctx); err != nil {
		log.Printf("auth: password-reset cleanup failed: %v", err)
	} else if n > 0 {
		log.Printf("auth: cleaned up %d finished password-reset link(s)", n)
	}
}
