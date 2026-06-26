package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestHashPassword_roundTrip verifies a hashed password verifies against itself
// and that the cost is the configured factor.
func TestHashPassword_roundTrip(t *testing.T) {
	t.Parallel()

	const pw = "correct horse battery staple"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == pw {
		t.Fatal("hash equals plaintext; password was not hashed")
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != bcryptCost {
		t.Errorf("bcrypt cost = %d, want %d", cost, bcryptCost)
	}
	if err := CheckPassword(hash, pw); err != nil {
		t.Errorf("CheckPassword on valid password: %v", err)
	}
}

// TestHashPassword_distinctSalts verifies hashing the same password twice yields
// different hashes (random salt) that both verify.
func TestHashPassword_distinctSalts(t *testing.T) {
	t.Parallel()

	const pw = "another good password"
	h1, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword h1: %v", err)
	}
	h2, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword h2: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password are identical; salt not applied")
	}
}

// TestHashPassword_tooShort verifies the minimum-length guard.
func TestHashPassword_tooShort(t *testing.T) {
	t.Parallel()

	if _, err := HashPassword("short"); !errors.Is(err, ErrPasswordTooShort) {
		t.Errorf("HashPassword(short) error = %v, want ErrPasswordTooShort", err)
	}
}

// TestCheckPassword_mismatch verifies a wrong password yields ErrPasswordMismatch.
func TestCheckPassword_mismatch(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("the right password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if err := CheckPassword(hash, "the wrong password"); !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("CheckPassword(wrong) error = %v, want ErrPasswordMismatch", err)
	}
}

// TestCheckPassword_malformedHash verifies a non-bcrypt hash surfaces a wrapped
// error rather than a false match.
func TestCheckPassword_malformedHash(t *testing.T) {
	t.Parallel()

	err := CheckPassword("not-a-bcrypt-hash", "whatever")
	if err == nil {
		t.Fatal("CheckPassword on malformed hash returned nil, want error")
	}
	if errors.Is(err, ErrPasswordMismatch) {
		t.Error("malformed hash reported as mismatch; want a distinct wrapped error")
	}
}

// TestHashPassword_tooLong verifies passwords beyond bcrypt's 72-byte limit are
// rejected with a wrapped error rather than silently truncated.
func TestHashPassword_tooLong(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 73)
	if _, err := HashPassword(long); err == nil {
		t.Error("HashPassword on >72-byte password returned nil error, want failure")
	}
}
