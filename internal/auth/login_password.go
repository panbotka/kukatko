package auth

import "sync"

// dummyHashInput is the fixed string behind the hash the unknown-user branch of
// a login compares against. It is not a credential and nothing accepts it: its
// value is irrelevant to correctness, because the comparison's result is
// discarded. It exists only so that branch performs the same bcrypt work as a
// real one.
const dummyHashInput = "kukatko: no account, this hash decides nothing"

// dummyPasswordHash returns a bcrypt hash minted at this build's work factor,
// computed once and reused. It is minted through HashPassword so it always
// carries the same cost as the hashes of real accounts — a hash of a *different*
// cost would restore the very timing difference it is here to remove.
//
// HashPassword can only fail on an input below minPasswordLen or one bcrypt
// rejects outright (over 72 bytes). dummyHashInput is a compile-time
// constant that is neither, so a failure here is a mistake in this file rather
// than a runtime condition, and there is no sensible degraded value to fall back
// to: an unparseable hash would make the branch return in microseconds.
var dummyPasswordHash = sync.OnceValue(func() string {
	hash, err := HashPassword(dummyHashInput)
	if err != nil {
		panic("auth: hashing the dummy login input: " + err.Error())
	}
	return hash
})

// checkLoginPassword performs the password half of a login attempt and reports
// whether it succeeded, mapping every failure onto ErrInvalidCredentials. known
// says whether the username resolved to an account; when it did not, user is the
// zero value.
//
// It always runs exactly one bcrypt comparison — against the account's hash when
// there is one, against dummyPasswordHash when there is not — so an unknown
// user, a disabled user and a wrong password all cost the same ~250 ms
// (SEC-006). Returning early on the first two, as this used to, answered in
// microseconds and so told an anonymous caller which usernames exist; that
// oracle is what makes unlimited guessing worth attempting in the first place.
//
// An account nobody has approved yet is the one failure that answers with
// something other than ErrInvalidCredentials: it returns ErrNotApproved, so the
// sign-in screen can tell somebody who registered that they are waiting rather
// than that they mistyped. It is decided last, after the password has been
// verified, so only a caller who already holds the credentials learns it — and
// after the disabled check, because a blocked account stays indistinguishable
// from a wrong password however it came to exist.
func checkLoginPassword(user User, known bool, password string) error {
	hash := user.PasswordHash
	if !known {
		hash = dummyPasswordHash()
	}
	err := CheckPassword(hash, password)
	if !known || user.Disabled || err != nil {
		return ErrInvalidCredentials
	}
	if user.ApprovedAt == nil {
		return ErrNotApproved
	}
	return nil
}
