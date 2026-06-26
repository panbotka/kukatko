package auth

import (
	"context"
	"log"
	"time"
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
type CreateUserInput struct {
	Username    string
	Password    string
	DisplayName string
	Email       string
	Role        Role
}

// UpdateUserInput holds the mutable profile fields for an admin user update.
type UpdateUserInput struct {
	DisplayName string
	Email       string
	Role        Role
	Disabled    bool
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
// returns ErrInvalidRole for an unknown role, ErrPasswordTooShort for a weak
// password, ErrUsernameTaken on a duplicate username, and the created user
// (without its password hash relevant to callers) on success.
func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (User, error) {
	if !in.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	uid, err := newUserUID()
	if err != nil {
		return User{}, err
	}
	user := User{
		UID:          uid,
		Username:     normalizeUsername(in.Username),
		DisplayName:  in.DisplayName,
		Email:        in.Email,
		Role:         in.Role,
		PasswordHash: hash,
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return User{}, err
	}
	return s.store.GetUserByUID(ctx, uid)
}

// ListUsers returns every user ordered by username.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	return s.store.ListUsers(ctx)
}

// GetUser returns the user identified by uid, or ErrUserNotFound.
func (s *Service) GetUser(ctx context.Context, uid string) (User, error) {
	return s.store.GetUserByUID(ctx, uid)
}

// UpdateUser updates a user's profile fields. When the update disables the
// account, all of that user's sessions are invalidated so the change takes
// effect immediately. It returns ErrInvalidRole for an unknown role and
// ErrUserNotFound if no such user exists.
func (s *Service) UpdateUser(ctx context.Context, uid string, in UpdateUserInput) (User, error) {
	if !in.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	user, err := s.store.UpdateUserProfile(ctx, uid, in.DisplayName, in.Email, in.Role, in.Disabled)
	if err != nil {
		return User{}, err
	}
	if in.Disabled {
		if _, err := s.store.DeleteUserSessions(ctx, uid); err != nil {
			return User{}, err
		}
	}
	return user, nil
}

// SetUserDisabled enables or disables the user identified by uid. Disabling also
// invalidates all of that user's sessions so the lockout is immediate. It
// returns the refreshed user, or ErrUserNotFound if no such user exists.
func (s *Service) SetUserDisabled(ctx context.Context, uid string, disabled bool) (User, error) {
	user, err := s.store.SetUserDisabled(ctx, uid, disabled)
	if err != nil {
		return User{}, err
	}
	if disabled {
		if _, err := s.store.DeleteUserSessions(ctx, uid); err != nil {
			return User{}, err
		}
	}
	return user, nil
}

// ResetPassword sets a new password for the user identified by uid and
// invalidates all of that user's sessions. It returns ErrPasswordTooShort for a
// weak password and ErrUserNotFound if no such user exists.
func (s *Service) ResetPassword(ctx context.Context, uid, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.store.SetPasswordHash(ctx, uid, hash); err != nil {
		return err
	}
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
