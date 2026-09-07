package fingerprint

import (
	"container/list"
	"sync"
	"time"
)

type cacheEntry struct {
	key       string
	result    *Result
	createdAt time.Time
}

// LRUCache provides thread-safe in-memory caching of fingerprint lookups.
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	ttl      time.Duration
	items    map[string]*list.Element
	evict    *list.List
}

// NewLRUCache creates an LRUCache with given capacity and entry time-to-live.
func NewLRUCache(capacity int, ttl time.Duration) *LRUCache {
	if capacity <= 0 {
		capacity = 100
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	return &LRUCache{
		capacity: capacity,
		ttl:      ttl,
		items:    make(map[string]*list.Element),
		evict:    list.New(),
	}
}

// Get retrieves a result by key if present and not expired.
func (c *LRUCache) Get(key string) (*Result, bool) {
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, false
	}

	entry := elem.Value.(*cacheEntry)
	if time.Since(entry.createdAt) > c.ttl {
		c.evict.Remove(elem)
		delete(c.items, key)
		return nil, false
	}

	c.evict.MoveToFront(elem)
	return entry.result, true
}

// Put inserts or updates a result in the cache, evicting the oldest element if full.
func (c *LRUCache) Put(key string, val *Result) {
	if key == "" || val == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, exists := c.items[key]; exists {
		c.evict.MoveToFront(elem)
		entry := elem.Value.(*cacheEntry)
		entry.result = val
		entry.createdAt = time.Now()
		return
	}

	if c.evict.Len() >= c.capacity {
		oldest := c.evict.Back()
		if oldest != nil {
			c.evict.Remove(oldest)
			oldEntry := oldest.Value.(*cacheEntry)
			delete(c.items, oldEntry.key)
		}
	}

	entry := &cacheEntry{
		key:       key,
		result:    val,
		createdAt: time.Now(),
	}
	elem := c.evict.PushFront(entry)
	c.items[key] = elem
}

// Len returns the current number of cached items.
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear flushes all entries from the cache.
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.evict.Init()
}
