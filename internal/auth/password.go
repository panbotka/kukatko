package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the bcrypt work factor used for all password hashes. Cost 12 is
// a deliberate security choice (slow enough to resist offline brute force on
// modern hardware) and is fixed so every stored hash is consistent.
const bcryptCost = 12

// minPasswordLen is the shortest password accepted; bcrypt additionally ignores
// any bytes past 72, but a sensible floor is enforced here.
const minPasswordLen = 8

// Sentinel errors for password operations so callers can branch with errors.Is.
var (
	// ErrPasswordTooShort indicates a password below minPasswordLen bytes.
	ErrPasswordTooShort = fmt.Errorf("auth: password must be at least %d characters", minPasswordLen)
	// ErrPasswordMismatch indicates a candidate password did not match the hash.
	ErrPasswordMismatch = errors.New("auth: password does not match")
)

// HashPassword returns a bcrypt hash of password at the package's fixed cost.
// It returns ErrPasswordTooShort if password is shorter than minPasswordLen, or
// a wrapped error if bcrypt fails (for example when the password exceeds
// bcrypt's 72-byte input limit).
func HashPassword(password string) (string, error) {
	if len(password) < minPasswordLen {
		return "", ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("auth: hashing password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether password matches the bcrypt hash. It returns nil
// on a match, ErrPasswordMismatch when the password is wrong, and a wrapped
// error for a malformed or unsupported hash.
func CheckPassword(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
		return ErrPasswordMismatch
	default:
		return fmt.Errorf("auth: comparing password: %w", err)
	}
}
