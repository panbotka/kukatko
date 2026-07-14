package mapsapi

import (
	"strconv"
	"sync"
	"time"

	"github.com/panbotka/kukatko/internal/mapy"
)

// Bounds of the server-side tile cache.
const (
	// maxCachedTileBytes is the largest single tile the cache will hold. Real
	// mapy.com tiles are tens of kilobytes; anything above this is streamed
	// through uncached rather than pinned in memory.
	maxCachedTileBytes = 512 << 10
)

// tileEntry is one cached tile: its bytes, its content type and the bookkeeping
// for expiry and LRU eviction.
type tileEntry struct {
	body        []byte
	contentType string
	expiresAt   time.Time
	lastUsed    time.Time
}

// tileCache is a concurrency-safe, TTL- and byte-bounded cache of proxied tiles.
// Every tile served from it is one mapy.com credit not spent: the free tier bills
// one credit per tile, so without it every pan over an already-seen area costs
// again. Only successful fetches are ever stored — an error response must never
// be cached, or a transient upstream failure (or a rejected key) would freeze
// into a hole in the map for the whole TTL.
//
// Eviction is lazy on read for expired entries, and least-recently-used when the
// byte budget is exhausted. The LRU victim is found by scanning the entries,
// which is O(n) but happens only on insert into a full cache and over a map of a
// few thousand tiles.
type tileCache struct {
	mu       sync.Mutex
	entries  map[string]tileEntry
	bytes    int64
	maxBytes int64
	now      func() time.Time
}

// newTileCache returns a tileCache holding at most maxBytes of tile data. A
// non-positive maxBytes disables caching entirely (every get misses and every set
// is a no-op).
func newTileCache(maxBytes int64) *tileCache {
	return &tileCache{
		entries:  make(map[string]tileEntry),
		maxBytes: maxBytes,
		now:      time.Now,
	}
}

// tileCacheKey identifies a tile in the cache. The retina variant is a distinct
// image, so it is a distinct key.
func tileCacheKey(params mapy.TileParams) string {
	key := params.Mapset + "/" +
		strconv.Itoa(params.Z) + "/" +
		strconv.Itoa(params.X) + "/" +
		strconv.Itoa(params.Y)
	if params.Retina {
		key += "@2x"
	}
	return key
}

// get returns the cached tile for key when present and unexpired, refreshing its
// LRU timestamp. An expired entry is dropped and reported as a miss.
func (c *tileCache) get(key string) (tileEntry, bool) {
	if c.maxBytes <= 0 {
		return tileEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return tileEntry{}, false
	}
	now := c.now()
	if now.After(entry.expiresAt) {
		c.remove(key)
		return tileEntry{}, false
	}
	entry.lastUsed = now
	c.entries[key] = entry
	return entry, true
}

// set stores body under key with the given TTL, evicting least-recently-used
// entries until it fits. It is a no-op when caching is disabled (non-positive TTL
// or byte budget) or when the tile is too large to be worth pinning; the caller
// must hand over a body it no longer writes to.
func (c *tileCache) set(key string, body []byte, contentType string, ttl time.Duration) {
	if c.maxBytes <= 0 || ttl <= 0 {
		return
	}
	size := int64(len(body))
	if size == 0 || size > maxCachedTileBytes || size > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remove(key)
	for c.bytes+size > c.maxBytes {
		if !c.evictLRU() {
			return
		}
	}
	now := c.now()
	c.entries[key] = tileEntry{
		body:        body,
		contentType: contentType,
		expiresAt:   now.Add(ttl),
		lastUsed:    now,
	}
	c.bytes += size
}

// remove drops key and its byte count. The caller must hold the lock.
func (c *tileCache) remove(key string) {
	entry, ok := c.entries[key]
	if !ok {
		return
	}
	delete(c.entries, key)
	c.bytes -= int64(len(entry.body))
}

// evictLRU drops the least-recently-used entry, reporting whether one was
// dropped (false means the cache is already empty). The caller must hold the
// lock.
func (c *tileCache) evictLRU() bool {
	var victim string
	var oldest time.Time
	for key, entry := range c.entries {
		if victim == "" || entry.lastUsed.Before(oldest) {
			victim, oldest = key, entry.lastUsed
		}
	}
	if victim == "" {
		return false
	}
	c.remove(victim)
	return true
}
