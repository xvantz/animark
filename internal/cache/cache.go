// Package cache provides a simple TTL-aware in-memory cache for API responses.
package cache

import (
	"sync"
	"time"
)

// Item holds a cached value with expiration.
type Item struct {
	Value   interface{}
	Expires time.Time
}

// Cache is a generic TTL cache safe for concurrent use.
type Cache struct {
	mu       sync.RWMutex
	items    map[string]*Item
	defaultTTL time.Duration
	done     chan struct{}
}

// New creates a new cache with the given default TTL and cleanup interval.
func New(defaultTTL, cleanupInterval time.Duration) *Cache {
	c := &Cache{
		items:      make(map[string]*Item),
		defaultTTL: defaultTTL,
		done:       make(chan struct{}),
	}
	if cleanupInterval > 0 {
		go c.cleanup(cleanupInterval)
	}
	return c
}

// Get returns a cached value and whether it was found and not expired.
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(item.Expires) {
		c.Delete(key)
		return nil, false
	}
	return item.Value, true
}

// Set adds or updates a cached value with the default TTL.
func (c *Cache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.defaultTTL)
}

// SetWithTTL adds a value with a specific TTL.
func (c *Cache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = &Item{
		Value:   value,
		Expires: time.Now().Add(ttl),
	}
}

// Delete removes a key from the cache.
func (c *Cache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Clear removes all items.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*Item)
}

// Stop terminates the background cleanup goroutine.
func (c *Cache) Stop() {
	close(c.done)
}

func (c *Cache) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for k, v := range c.items {
				if now.After(v.Expires) {
					delete(c.items, k)
				}
			}
			c.mu.Unlock()
		case <-c.done:
			return
		}
	}
}
