package mapsapi

import (
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/mapy"
)

// geocodeEntry is one cached reverse-geocode answer with its expiry.
type geocodeEntry struct {
	result    mapy.GeocodeResult
	expiresAt time.Time
}

// geocodeCache is a small, concurrency-safe, TTL- and capacity-bounded cache of
// reverse-geocode answers. It conserves mapy.com credits by reusing answers for
// repeated or near-identical coordinates rather than spending four credits per
// lookup. Eviction is lazy on read for expired entries and drops the
// soonest-to-expire entry when the capacity is reached.
type geocodeCache struct {
	mu      sync.Mutex
	entries map[string]geocodeEntry
	maxSize int
}

// newGeocodeCache returns a geocodeCache holding at most maxSize entries.
func newGeocodeCache(maxSize int) *geocodeCache {
	return &geocodeCache{entries: make(map[string]geocodeEntry), maxSize: maxSize}
}

// get returns the cached answer for key when present and unexpired. An expired
// entry is dropped and reported as a miss.
func (c *geocodeCache) get(key string) (mapy.GeocodeResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return mapy.GeocodeResult{}, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(c.entries, key)
		return mapy.GeocodeResult{}, false
	}
	return entry.result, true
}

// set stores result under key with the given TTL. A non-positive TTL disables
// caching (the call is a no-op). When the cache is full it first drops the
// soonest-to-expire entry to make room.
func (c *geocodeCache) set(key string, result mapy.GeocodeResult, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxSize {
		c.evictOldest()
	}
	c.entries[key] = geocodeEntry{result: result, expiresAt: time.Now().Add(ttl)}
}

// evictOldest removes the entry with the earliest expiry. The caller must hold
// the lock.
func (c *geocodeCache) evictOldest() {
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
