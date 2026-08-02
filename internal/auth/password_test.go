package auth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// productionBcryptCost is the work factor a shipped build must mint hashes at,
// spelled out as a literal so the tests below break if bcryptCost is ever
// lowered — the whole point of a build-tag-tunable cost is that the production
// value stays put. It is declared here, in the untagged test file, so both the
// production-build and the integration-build assertions can use it.
const productionBcryptCost = 12

// TestHashPassword_roundTrip verifies a hashed password verifies against itself
// and that the cost is the one this build mints at (bcryptCost, or the cheaper
// integration-test cost when built with the `integration` tag).
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
	if cost != hashCost {
		t.Errorf("bcrypt cost = %d, want %d", cost, hashCost)
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

// TestCheckPassword_mixedCosts verifies the property the tunable cost rests on:
// a hash carries the cost it was minted at, so hashes made at different costs
// all verify through the same CheckPassword. Without this, hashes written by a
// cheap test build and by production could not coexist in one column.
func TestCheckPassword_mixedCosts(t *testing.T) {
	t.Parallel()

	const pw = "a password hashed at several costs"
	for _, cost := range []int{bcrypt.MinCost, productionBcryptCost} {
		raw, err := bcrypt.GenerateFromPassword([]byte(pw), cost)
		if err != nil {
			t.Fatalf("bcrypt.GenerateFromPassword(cost=%d): %v", cost, err)
		}
		if got, err := bcrypt.Cost(raw); err != nil || got != cost {
			t.Fatalf("bcrypt.Cost = %d (err %v), want %d", got, err, cost)
		}
		if err := CheckPassword(string(raw), pw); err != nil {
			t.Errorf("CheckPassword on a cost-%d hash: %v", cost, err)
		}
		if err := CheckPassword(string(raw), "the wrong password"); !errors.Is(err, ErrPasswordMismatch) {
			t.Errorf("CheckPassword(wrong) on a cost-%d hash = %v, want ErrPasswordMismatch", cost, err)
		}
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
