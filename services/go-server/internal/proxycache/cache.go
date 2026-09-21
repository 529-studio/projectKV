package proxycache

import (
	"golang.org/x/sync/singleflight"
	"sync"
	"time"
)

type Cache struct {
	mu    sync.RWMutex
	items map[string]cacheEntry
	ttl   time.Duration
	group singleflight.Group
}
type cacheEntry struct {
	data      []byte
	expiresAt time.Time
}

func NewCache(ttl time.Duration) *Cache {
	return &Cache{items: make(map[string]cacheEntry), ttl: ttl}
}
func (c *Cache) GetOrFetch(id string, fetch func() ([]byte, error)) ([]byte, error) {
	c.mu.RLock()
	item, ok := c.items[id]
	c.mu.RUnlock()
	if ok && time.Now().Before(item.expiresAt) {
		return item.data, nil
	}
	v, err, _ := c.group.Do(id, func() (interface{}, error) {
		return fetch()
	})
	if err != nil {
		return nil, err
	}
	data := v.([]byte)
	c.mu.Lock()
	c.items[id] = cacheEntry{data: data, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
	return data, nil
}
