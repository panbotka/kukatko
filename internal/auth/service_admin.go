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
	// BootstrapCreated means the initial admin account was created.
	BootstrapCreated
)

// CreateUserInput holds the fields needed to create a user (admin-only).
// DisplayName and Note are both optional and default to the empty string.
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
}

// UpdateUserInput holds the mutable profile fields for an admin user update.
// Note is a pointer to distinguish "absent" from "empty": nil leaves the stored
// note untouched, while a pointer to "" clears it.
//
// Its field order and types mirror updateUserRequest so the HTTP layer can
// convert between them directly; keep the two in step.
type UpdateUserInput struct {
	DisplayName string
	Email       string
	Role        Role
	Disabled    bool
	Note        *string
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

// Bootstrap creates the first admin account when the users table is empty and a
// username and password are both provided. It returns BootstrapSkippedHasUsers
// when users already exist, BootstrapSkippedNoCredentials when credentials are
// missing, or BootstrapCreated on success; errors are returned wrapped.
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
		Role:        RoleAdmin,
	}); err != nil {
		return BootstrapSkippedNoCredentials, err
	}
	return BootstrapCreated, nil
}

// CreateUser validates and inserts a new user, hashing the supplied password. It
// records no audit entry and is used for system-initiated creation (bootstrap,
// test seeding); handlers that must attribute the action to an admin call
// CreateUserAudited. It returns ErrInvalidRole for an unknown role,
// ErrPasswordTooShort for a weak password, ErrNoteTooLong for an over-length
// note, ErrUsernameTaken on a duplicate username, and the created user on success.
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
// entry's details, and its UID becomes the entry's target.
func (s *Service) CreateUserAudited(ctx context.Context, in CreateUserInput, entry audit.Entry) (User, error) {
	user, err := s.prepareNewUser(in)
	if err != nil {
		return User{}, err
	}
	if entry.Details == nil {
		entry.Details = map[string]any{}
	}
	entry.Details["username"] = user.Username
	entry.Details["role"] = string(user.Role)
	if err := s.store.CreateUserAudited(ctx, user, entry); err != nil {
		return User{}, err
	}
	return s.store.GetUserByUID(ctx, user.UID)
}

// prepareNewUser validates in and builds the User to insert, hashing the password
// and assigning a fresh UID. It is shared by CreateUser and CreateUserAudited and
// returns ErrInvalidRole, ErrPasswordTooShort or ErrNoteTooLong on invalid input.
func (s *Service) prepareNewUser(in CreateUserInput) (User, error) {
	if !in.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	if err := validateNote(in.Note); err != nil {
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
		Username:     normalizeUsername(in.Username),
		DisplayName:  in.DisplayName,
		Email:        in.Email,
		Role:         in.Role,
		PasswordHash: hash,
		Note:         in.Note,
	}, nil
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
// unknown role, ErrNoteTooLong for an over-length note, and ErrUserNotFound if no
// such user exists.
func (s *Service) UpdateUser(ctx context.Context, uid string, in UpdateUserInput) (User, error) {
	if err := validateUserUpdate(in); err != nil {
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
// the change (see internal/audit).
func (s *Service) UpdateUserAudited(
	ctx context.Context, uid string, in UpdateUserInput, entry audit.Entry,
) (User, error) {
	if err := validateUserUpdate(in); err != nil {
		return User{}, err
	}
	user, err := s.store.UpdateUserProfileAudited(ctx, uid, in, entry)
	if err != nil {
		return User{}, err
	}
	return s.invalidateIfDisabled(ctx, uid, in.Disabled, user)
}

// validateUserUpdate validates the role and optional note of an update input,
// returning ErrInvalidRole or ErrNoteTooLong. A nil note skips the note check.
func validateUserUpdate(in UpdateUserInput) error {
	if !in.Role.Valid() {
		return ErrInvalidRole
	}
	if in.Note != nil {
		return validateNote(*in.Note)
	}
	return nil
}

// SetUserDisabled enables or disables the user identified by uid without
// recording an audit entry; handlers use SetUserDisabledAudited. Disabling also
// invalidates all of that user's sessions so the lockout is immediate. It returns
// the refreshed user, or ErrUserNotFound if no such user exists.
func (s *Service) SetUserDisabled(ctx context.Context, uid string, disabled bool) (User, error) {
	user, err := s.store.SetUserDisabled(ctx, uid, disabled)
	if err != nil {
		return User{}, err
	}
	return s.invalidateIfDisabled(ctx, uid, disabled, user)
}

// SetUserDisabledAudited enables or disables a user like SetUserDisabled and
// writes a user.disable audit entry attributed to entry's actor in the same
// transaction as the change (see internal/audit).
func (s *Service) SetUserDisabledAudited(
	ctx context.Context, uid string, disabled bool, entry audit.Entry,
) (User, error) {
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
// as the change (see internal/audit).
func (s *Service) ResetPasswordAudited(ctx context.Context, uid, newPassword string, entry audit.Entry) error {
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
