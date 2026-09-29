// Package proxycache implements a TTL-based in-memory image proxy cache with
// singleflight deduplication, satisfying AGENTS.md §3 (10–15 s cache TTL,
// no direct frontend fetches from the upstream DOT portal).
package proxycache

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// cacheEntry holds a cached image payload and its expiry instant.
type cacheEntry struct {
	data        []byte
	contentType string
	osdTime     string
	rawOSD      string
	expiresAt   time.Time
}

// Cache is a thread-safe, TTL-based in-memory store for proxied snapshot
// images.  A singleflight.Group collapses concurrent cache-miss fetches for
// the same camera ID into a single upstream request.
//
//   - GetOrFetch returns (data, contentType, cacheHit, error).
//   - The fetch func is called at most once per inflight group (singleflight).
type Cache struct {
	mu    sync.RWMutex
	items map[string]cacheEntry
	ttl   time.Duration
	group singleflight.Group
}

// NewCache creates a Cache with the given TTL applied to every stored entry.
func NewCache(ttl time.Duration) *Cache {
	return &Cache{
		items: make(map[string]cacheEntry),
		ttl:   ttl,
	}
}

// fetchResult is the internal return type passed through singleflight.
type fetchResult struct {
	data        []byte
	contentType string
}

// GetOrFetch returns the cached payload for id if still fresh.  On a cache
// miss it calls fetch exactly once (concurrent callers share the inflight
// result via singleflight), stores the result and returns it.
//
// Returns: (data, contentType, isHit, error).
func (c *Cache) GetOrFetch(id string, fetch func() (data []byte, contentType string, err error)) ([]byte, string, bool, error) {
	// Fast path — read lock only.
	c.mu.RLock()
	entry, ok := c.items[id]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.data, entry.contentType, true, nil
	}

	// Slow path — deduplicated upstream fetch.
	v, err, _ := c.group.Do(id, func() (interface{}, error) {
		data, ct, fetchErr := fetch()
		if fetchErr != nil {
			return nil, fetchErr
		}
		return fetchResult{data: data, contentType: ct}, nil
	})
	if err != nil {
		return nil, "", false, err
	}

	res := v.(fetchResult)
	osdTime, rawOSD := ExtractOSD(res.data)

	c.mu.Lock()
	c.items[id] = cacheEntry{
		data:        res.data,
		contentType: res.contentType,
		osdTime:     osdTime,
		rawOSD:      rawOSD,
		expiresAt:   time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return res.data, res.contentType, false, nil
}

// GetOSD returns the cached OSD timestamp and raw text for the given camera ID.
func (c *Cache) GetOSD(id string) (osdTime string, rawOSD string, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, found := c.items[id]
	if found && time.Now().Before(entry.expiresAt) {
		return entry.osdTime, entry.rawOSD, true
	}
	return "", "", false
}

// Get returns the cached image payload for id if present and not expired.
func (c *Cache) Get(id string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, found := c.items[id]
	if found && time.Now().Before(entry.expiresAt) {
		return entry.data, true
	}
	return nil, false
}

// Purge removes all expired entries from the cache.  Call periodically to
// prevent unbounded memory growth.
func (c *Cache) Purge() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, e := range c.items {
		if now.After(e.expiresAt) {
			delete(c.items, id)
		}
	}
}
