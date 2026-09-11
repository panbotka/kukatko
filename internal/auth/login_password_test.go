package auth

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// theRealPassword is the password of the fixture account the login tests below
// build; every wrong guess differs from it.
const theRealPassword = "correct horse battery"

// fixtureUser returns an enabled, approved account whose password is
// theRealPassword — the ordinary account every other case here varies from.
func fixtureUser(t *testing.T) User {
	t.Helper()
	hash, err := HashPassword(theRealPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	approved := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	return User{Username: "someone", PasswordHash: hash, ApprovedAt: &approved}
}

// TestCheckLoginPassword_outcomes verifies only a known, enabled, approved
// account with the right password succeeds; that every combination an anonymous
// caller could probe with is reported as the same indistinguishable
// ErrInvalidCredentials; and that an account still waiting for an administrator
// is the one exception — but only once the password was right, so it says
// nothing to somebody guessing.
func TestCheckLoginPassword_outcomes(t *testing.T) {
	t.Parallel()

	user := fixtureUser(t)
	disabled := user
	disabled.Disabled = true
	waiting := user
	waiting.ApprovedAt = nil
	// Blocked *and* never approved: the blocking wins, because "this account is
	// shut out" must stay indistinguishable from a wrong password.
	waitingDisabled := waiting
	waitingDisabled.Disabled = true

	tests := []struct {
		name     string
		user     User
		known    bool
		password string
		wantErr  error
	}{
		{name: "known enabled user with the right password", user: user, known: true,
			password: theRealPassword, wantErr: nil},
		{name: "known enabled user with a wrong password", user: user, known: true,
			password: "not it at all", wantErr: ErrInvalidCredentials},
		{name: "disabled user with the right password", user: disabled, known: true,
			password: theRealPassword, wantErr: ErrInvalidCredentials},
		{name: "disabled user with a wrong password", user: disabled, known: true,
			password: "not it at all", wantErr: ErrInvalidCredentials},
		{name: "unknown user", user: User{}, known: false,
			password: theRealPassword, wantErr: ErrInvalidCredentials},
		{name: "unapproved user with the right password", user: waiting, known: true,
			password: theRealPassword, wantErr: ErrNotApproved},
		{name: "unapproved user with a wrong password", user: waiting, known: true,
			password: "not it at all", wantErr: ErrInvalidCredentials},
		{name: "unapproved and disabled user with the right password", user: waitingDisabled, known: true,
			password: theRealPassword, wantErr: ErrInvalidCredentials},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkLoginPassword(tt.user, tt.known, tt.password)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("checkLoginPassword() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestDummyPasswordHash_matchesProductionCost verifies the stand-in hash is
// minted at exactly the work factor real accounts use. A cheaper dummy would
// hand the timing oracle straight back, and the difference would not show up in
// any functional test.
func TestDummyPasswordHash_matchesProductionCost(t *testing.T) {
	t.Parallel()

	cost, err := bcrypt.Cost([]byte(dummyPasswordHash()))
	if err != nil {
		t.Fatalf("bcrypt.Cost on the dummy hash: %v", err)
	}
	if cost != hashCost {
		t.Errorf("dummy hash cost = %d, want %d (the cost real accounts are hashed at)", cost, hashCost)
	}
	if CheckPassword(dummyPasswordHash(), theRealPassword) == nil {
		t.Error("the dummy hash accepted a password; it must match nothing a caller can send")
	}
}

// TestCheckLoginPassword_timingIsIndistinguishable is SEC-006: an anonymous
// caller must not be able to tell an existing account from a nonexistent or
// disabled one by how long the answer takes.
//
// The comparison is deliberately coarse — the fastest of a few rounds per
// branch, within a small factor of each other. Precision is not the point: the
// failure this catches is a branch that skips bcrypt entirely, which is three
// orders of magnitude faster, not one that is 30 % slower. Taking the minimum
// rather than the mean is what keeps it stable on a loaded machine, where
// scheduling can only ever add time to a CPU-bound loop.
//
// The rounds interleave the branches instead of measuring each one to
// completion in turn. Run back to back, whichever branch goes first absorbs the
// load the machine is under at the start — on a busy CI runner that alone
// inflated the first branch past a 2× tolerance while the last ran on a quiet
// core, failing a test whose subject does the identical work on every path.
func TestCheckLoginPassword_timingIsIndistinguishable(t *testing.T) {
	t.Parallel()

	user := fixtureUser(t)
	disabled := user
	disabled.Disabled = true
	// Warm the lazily minted dummy hash so its one-off cost is not charged to the
	// first branch that happens to need it.
	dummyPasswordHash()

	// tolerance leaves room for the scheduling noise of a shared runner while
	// staying three orders of magnitude below the regression this guards: a
	// branch that returns without hashing answers in microseconds, not in a
	// quarter of the time.
	const (
		rounds    = 3
		tolerance = 4.0
	)
	branches := []struct {
		name  string
		user  User
		known bool
	}{
		{name: "unknown user", user: User{}, known: false},
		{name: "disabled user", user: disabled, known: true},
		{name: "wrong password", user: user, known: true},
	}

	fastest := make(map[string]time.Duration, len(branches))
	for round := range rounds {
		// Rotate the starting branch so that over the rounds each one pays the
		// cost of going first exactly once, and the minimum below picks every
		// branch's quietest round rather than its position in a fixed order.
		for offset := range branches {
			branch := branches[(round+offset)%len(branches)]
			start := time.Now()
			if err := checkLoginPassword(branch.user, branch.known, "definitely not the password"); err == nil {
				t.Fatalf("%s: checkLoginPassword succeeded, want a failure", branch.name)
			}
			if elapsed := time.Since(start); round == 0 || elapsed < fastest[branch.name] {
				fastest[branch.name] = elapsed
			}
		}
	}
	for _, branch := range branches {
		t.Logf("%s: fastest of %d = %s", branch.name, rounds, fastest[branch.name])
	}

	for _, a := range branches {
		for _, b := range branches {
			ratio := float64(fastest[a.name]) / float64(fastest[b.name])
			if ratio > tolerance {
				t.Errorf("%q took %s and %q took %s (%.1f× apart, tolerance %.1f×): "+
					"one of these login branches is not doing the bcrypt work",
					a.name, fastest[a.name], b.name, fastest[b.name], ratio, tolerance)
			}
		}
	}
}
