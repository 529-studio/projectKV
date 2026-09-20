package cache

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// SnapshotItem holds in-memory cached image bytes and metadata.
type SnapshotItem struct {
	Buffer      []byte
	ContentType string
	Timestamp   time.Time
}

// SnapshotCache provides thread-safe snapshot caching with singleflight deduplication.
type SnapshotCache struct {
	mu     sync.RWMutex
	items  map[string]*SnapshotItem
	ttl    time.Duration
	flight singleflight.Group
}

// NewSnapshotCache initializes a new snapshot cache.
func NewSnapshotCache(ttl time.Duration) *SnapshotCache {
	return &SnapshotCache{
		items: make(map[string]*SnapshotItem),
		ttl:   ttl,
	}
}

// Get retrieves an item from cache if it exists and has not exceeded TTL.
func (c *SnapshotCache) Get(id string) (*SnapshotItem, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[id]
	if !exists {
		return nil, false
	}

	if time.Since(item.Timestamp) < c.ttl {
		return item, true
	}

	return nil, false
}

// Set stores a new snapshot item in memory.
func (c *SnapshotCache) Set(id string, buffer []byte, contentType string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[id] = &SnapshotItem{
		Buffer:      buffer,
		ContentType: contentType,
		Timestamp:   time.Now(),
	}
}

// FetchOrGet retrieves the snapshot from cache or calls fetchFunc using singleflight
// to ensure only 1 upstream request is made when multiple concurrent callers request the same camera.
func (c *SnapshotCache) FetchOrGet(id string, fetchFunc func() ([]byte, string, error)) ([]byte, string, bool, error) {
	// Fast path: cache hit
	if item, hit := c.Get(id); hit {
		return item.Buffer, item.ContentType, true, nil
	}

	// Singleflight dedup
	res, err, _ := c.flight.Do(id, func() (interface{}, error) {
		// Double check cache
		if item, hit := c.Get(id); hit {
			return item, nil
		}

		buf, contentType, err := fetchFunc()
		if err != nil {
			return nil, err
		}

		item := &SnapshotItem{
			Buffer:      buf,
			ContentType: contentType,
			Timestamp:   time.Now(),
		}

		c.mu.Lock()
		c.items[id] = item
		c.mu.Unlock()

		return item, nil
	})

	if err != nil {
		return nil, "", false, err
	}

	item := res.(*SnapshotItem)
	return item.Buffer, item.ContentType, false, nil
}

// StartEvictionRoutine periodically purges entries older than maxAge.
func (c *SnapshotCache) StartEvictionRoutine(interval, maxAge time.Duration, stopCh <-chan struct{}) {
	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				c.evictExpired(maxAge)
			case <-stopCh:
				ticker.Stop()
				return
			}
		}
	}()
}

func (c *SnapshotCache) evictExpired(maxAge time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for id, item := range c.items {
		if now.Sub(item.Timestamp) > maxAge {
			delete(c.items, id)
		}
	}
}
