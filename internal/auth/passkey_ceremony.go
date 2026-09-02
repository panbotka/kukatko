package auth

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// ceremony is one WebAuthn ceremony waiting for its answer: the challenge and
// parameters the library needs to verify it, and — for a registration — the
// account that began it.
//
// userUID is empty for a login, and that emptiness is checked rather than
// ignored: a registration ceremony must never be spendable as a login, or a
// caller who can begin one for their own account could use its challenge to
// finish a sign-in ceremony as somebody else.
type ceremony struct {
	session webauthn.SessionData
	userUID string
	expires time.Time
}

// maxCeremonies bounds how many ceremonies are held at once. Beginning a login
// is unauthenticated, so the map is reachable by an anonymous caller; the rate
// limiter in front of the endpoint is the first bound and this is the second,
// which is the one that holds if the limiter is ever misconfigured. A ceremony
// is well under a kilobyte, so a full map is a couple of megabytes.
const maxCeremonies = 4096

// ceremonyStore keeps in-flight ceremonies in memory, keyed by an opaque random
// id the client carries in a cookie.
//
// In memory rather than in the database on purpose: a challenge is worthless the
// moment it is answered or expires, it is never read by anything but the process
// that minted it, and a table of them would be a table whose every row is
// garbage within five minutes. The cost is that ceremonies do not survive a
// restart, which costs a person one retry of a sign-in they had not finished.
type ceremonyStore struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]ceremony
}

// newCeremonyStore returns an empty store whose ceremonies expire after ttl.
func newCeremonyStore(ttl time.Duration) *ceremonyStore {
	return &ceremonyStore{ttl: ttl, entries: make(map[string]ceremony)}
}

// ceremonyIDLen is the number of random base32 characters in a ceremony id, for
// ~125 bits of entropy. The id is not a credential — spending it still requires
// an authenticator's signature over the challenge it names — but it must not be
// guessable, or one caller could consume another's ceremony.
const ceremonyIDLen = 25

// put stores cer under a fresh random id, stamps its expiry at now+ttl and
// returns the id. Inserting into a full store first drops what has expired and,
// if that frees nothing, refuses: an in-memory map bounded by nothing is how an
// anonymous endpoint becomes a memory leak, and a caller told to try again in a
// moment is a better outcome than an instance that stops.
func (s *ceremonyStore) put(cer ceremony, now time.Time) (string, error) {
	id, err := randomString(ceremonyIDLen)
	if err != nil {
		return "", err
	}
	cer.expires = now.Add(s.ttl)

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= maxCeremonies {
		s.cleanupLocked(now)
	}
	if len(s.entries) >= maxCeremonies {
		return "", ErrPasskeyCeremony
	}
	s.entries[id] = cer
	return id, nil
}

// take removes and returns the ceremony stored under id, reporting whether one
// was there and still valid as of now. It is one-shot by construction: a
// challenge that has been answered — successfully or not — must never be
// answerable a second time, so the entry is deleted whether it verifies or not.
func (s *ceremonyStore) take(id string, now time.Time) (ceremony, bool) {
	if id == "" {
		return ceremony{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cer, ok := s.entries[id]
	if !ok {
		return ceremony{}, false
	}
	delete(s.entries, id)
	if !cer.expires.After(now) {
		return ceremony{}, false
	}
	return cer, true
}

// cleanup drops every ceremony that has expired as of now.
func (s *ceremonyStore) cleanup(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupLocked(now)
}

// cleanupLocked drops every expired ceremony. The caller must hold s.mu.
func (s *ceremonyStore) cleanupLocked(now time.Time) {
	for id, cer := range s.entries {
		if !cer.expires.After(now) {
			delete(s.entries, id)
		}
	}
}

// len reports how many ceremonies are currently held; it exists for the tests
// that assert a ceremony is consumed exactly once.
func (s *ceremonyStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
