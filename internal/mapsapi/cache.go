package mapsapi

import (
	"sync"
	"time"
)

// ttlEntry is one cached value with its expiry.
type ttlEntry[T any] struct {
	value     T
	expiresAt time.Time
}

// ttlCache is a small, concurrency-safe, TTL- and capacity-bounded cache of
// answers keyed by a string. It conserves mapy.com credits by reusing answers for
// repeated lookups rather than spending credits on a question already asked —
// which is what makes a per-keystroke typeahead affordable at all. Eviction is
// lazy on read for expired entries and drops the soonest-to-expire entry when the
// capacity is reached.
//
// It is deliberately not an LRU: entries are equally cheap and equally likely to
// be asked for again, so expiry order is as good a victim as recency and costs no
// bookkeeping. (The tile cache, whose entries are big and unequal, has its own.)
type ttlCache[T any] struct {
	mu      sync.Mutex
	entries map[string]ttlEntry[T]
	maxSize int
}

// newTTLCache returns a ttlCache holding at most maxSize entries.
func newTTLCache[T any](maxSize int) *ttlCache[T] {
	return &ttlCache[T]{entries: make(map[string]ttlEntry[T]), maxSize: maxSize}
}

// get returns the cached value for key when present and unexpired. An expired
// entry is dropped and reported as a miss.
func (c *ttlCache[T]) get(key string) (T, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(c.entries, key)
		}
		var zero T
		return zero, false
	}
	return entry.value, true
}

// set stores value under key with the given TTL. A non-positive TTL disables
// caching (the call is a no-op). When the cache is full it first drops the
// soonest-to-expire entry to make room.
func (c *ttlCache[T]) set(key string, value T, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxSize {
		c.evictOldest()
	}
	c.entries[key] = ttlEntry[T]{value: value, expiresAt: time.Now().Add(ttl)}
}

// evictOldest removes the entry with the earliest expiry. The caller must hold
// the lock.
func (c *ttlCache[T]) evictOldest() {
	var oldestKey string
	var oldestAt time.Time
	for key, entry := range c.entries {
		if oldestKey == "" || entry.expiresAt.Before(oldestAt) {
			oldestKey, oldestAt = key, entry.expiresAt
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}
