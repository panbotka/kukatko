package auth

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the bcrypt work factor every shipped build mints hashes at. Cost
// 12 is a deliberate security choice: slow enough to resist offline brute force
// on modern hardware.
//
// A bcrypt hash records the cost it was minted at, and CompareHashAndPassword
// reads the cost from the hash rather than from this constant, so hashes made at
// different costs verify correctly side by side. That is what lets the
// integration-test build mint cheap hashes (password_cost_integration.go) while
// leaving verification untouched — the ~15 packages that seed accounts through
// the real auth path spent nearly all of their runtime here.
//
// The knob is a build-tag-selected identifier, not a variable: a settable
// var could be lowered by anything that imports this package, whereas the cheap
// cost is simply not compiled into a build that lacks the `integration` tag.
// Nothing a running server can reach can weaken production hashing.
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

// HashPassword returns a bcrypt hash of password at hashCost, which is
// bcryptCost in every build except the integration-test one. It returns
// ErrPasswordTooShort if password is shorter than minPasswordLen, or a wrapped
// error if bcrypt fails (for example when the password exceeds bcrypt's 72-byte
// input limit).
func HashPassword(password string) (string, error) {
	if len(password) < minPasswordLen {
		return "", ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), hashCost)
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
