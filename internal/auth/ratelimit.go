package auth

import (
	"sort"
	"sync"
	"time"
)

// Limiter is a concurrency-safe sliding-window rate limiter keyed by an
// arbitrary string (the login handler keys on username+client IP). It records
// failed-attempt timestamps per key and blocks a key once it accumulates max
// attempts within the trailing window. A successful login should Reset the key.
//
// Timestamps are supplied by the caller (Allow/Reset take an explicit now) so
// the limiter is deterministic under test; in production the caller passes
// time.Now().
//
// The key set is hard-capped at maxKeys and pruned on insertion, so an attacker
// flooding the public login endpoint with distinct keys cannot grow the map
// without bound between the periodic Cleanup ticks.
type Limiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	attempts map[string]*keyState
}

// keyState is one key's throttling state. timestamps holds the attempts still
// inside the window; lastSeen is the time of the key's most recent Allow call,
// including calls that were blocked. Eviction ranks by lastSeen rather than by
// the newest timestamp so that a blocked key — which by definition stops
// recording attempts — cannot be flushed out of the limiter by a flood of fresh
// keys, which would hand an attacker a way to clear their own block.
type keyState struct {
	timestamps []time.Time
	lastSeen   time.Time
}

// maxKeys bounds the number of live keys the limiter tracks. Login keys are
// username+IP pairs and usernames are capped at MaxUsernameLen, so a full map
// stays on the order of a megabyte.
const maxKeys = 8192

// evictTargetKeys is the size the key set is cut down to when maxKeys is reached
// and dropping expired keys alone does not free room. Evicting in batches keeps
// the O(n log n) eviction scan amortised over many insertions instead of running
// on every request once the map is full.
const evictTargetKeys = maxKeys * 3 / 4

// NewLimiter returns a Limiter that permits at most max attempts per key within
// window. max is clamped to a minimum of 1 and window to a minimum of one
// nanosecond so the limiter is always well-formed.
func NewLimiter(maxAttempts int, window time.Duration) *Limiter {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if window <= 0 {
		window = time.Nanosecond
	}
	return &Limiter{
		max:      maxAttempts,
		window:   window,
		attempts: make(map[string]*keyState),
	}
}

// Allow records an attempt for key at time now and reports whether the attempt
// is permitted. It first discards attempts older than the window; if the key is
// already at the limit it returns false without recording (so a blocked key
// does not extend its own block), otherwise it records now and returns true.
//
// Inserting a key that would push the map past maxKeys first frees room (see
// makeRoom), so memory stays bounded no matter how many distinct keys arrive
// between Cleanup ticks.
func (l *Limiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	state, known := l.attempts[key]
	if !known {
		l.makeRoom(now)
		state = &keyState{}
		l.attempts[key] = state
	}
	state.lastSeen = now
	state.timestamps = l.prune(state.timestamps, now)
	if len(state.timestamps) >= l.max {
		return false
	}
	state.timestamps = append(state.timestamps, now)
	return true
}

// makeRoom frees space for one new key when the map has reached maxKeys. It
// first drops keys whose every attempt has aged out; if that is not enough
// (a sustained flood keeps every key live) it evicts the least recently seen
// keys down to evictTargetKeys. The caller must hold l.mu.
func (l *Limiter) makeRoom(now time.Time) {
	if len(l.attempts) < maxKeys {
		return
	}
	l.cleanupLocked(now)
	if len(l.attempts) < maxKeys {
		return
	}
	l.evictLocked()
}

// evictLocked deletes the least recently seen keys until at most
// evictTargetKeys remain. The caller must hold l.mu.
func (l *Limiter) evictLocked() {
	type ranked struct {
		key      string
		lastSeen time.Time
	}
	keys := make([]ranked, 0, len(l.attempts))
	for key, state := range l.attempts {
		keys = append(keys, ranked{key: key, lastSeen: state.lastSeen})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].lastSeen.Before(keys[j].lastSeen) })
	for i := 0; i < len(keys) && len(l.attempts) > evictTargetKeys; i++ {
		delete(l.attempts, keys[i].key)
	}
}

// Reset clears all recorded attempts for key, called after a successful login so
// the user is not penalised for earlier failures.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// Cleanup drops keys whose every recorded attempt has aged out of the window as
// of now, bounding memory for the many distinct keys seen over time. It is safe
// to call periodically from a background goroutine.
func (l *Limiter) Cleanup(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)
}

// cleanupLocked drops every key whose recorded attempts have all aged out of the
// window as of now, keeping the pruned timestamps for the keys that remain. A
// key with no live attempts is indistinguishable from an unseen one, so dropping
// it changes nothing. The caller must hold l.mu.
func (l *Limiter) cleanupLocked(now time.Time) {
	for key, state := range l.attempts {
		if state.timestamps = l.prune(state.timestamps, now); len(state.timestamps) == 0 {
			delete(l.attempts, key)
		}
	}
}

// prune returns the subset of ts that falls within the trailing window ending at
// now (attempts strictly older than now-window are dropped). The input slice may
// be reused, so callers must use the returned slice.
func (l *Limiter) prune(timestamps []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	live := timestamps[:0]
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			live = append(live, ts)
		}
	}
	return live
}
