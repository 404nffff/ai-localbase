package service

import (
	"container/list"
	"fmt"
	"hash/fnv"
	"sync"
)

const defaultEmbeddingCacheSize = 2048

// embeddingCacheKey uniquely identifies a single text's embedding under a given model config.
func embeddingCacheKey(provider, baseURL, model, text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(provider))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(baseURL))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(model))
	_, _ = h.Write([]byte("\x00"))
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%s:%s:%s:%x", provider, model, baseURL, h.Sum64())
}

type lruEmbeddingCacheEntry struct {
	key    string
	vector []float64
}

// lruEmbeddingCache is a fixed-capacity, thread-safe LRU cache for embedding vectors.
type lruEmbeddingCache struct {
	mu       sync.Mutex
	capacity int
	list     *list.List
	items    map[string]*list.Element
}

func newLRUEmbeddingCache(capacity int) *lruEmbeddingCache {
	if capacity <= 0 {
		capacity = defaultEmbeddingCacheSize
	}
	return &lruEmbeddingCache{
		capacity: capacity,
		list:     list.New(),
		items:    make(map[string]*list.Element, capacity),
	}
}

// Get returns a copy of the cached vector, or nil if not present.
func (c *lruEmbeddingCache) Get(key string) []float64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil
	}
	c.list.MoveToFront(elem)
	entry := elem.Value.(*lruEmbeddingCacheEntry)
	result := make([]float64, len(entry.vector))
	copy(result, entry.vector)
	return result
}

// Set stores a copy of the vector. Evicts the least-recently-used entry when capacity is exceeded.
func (c *lruEmbeddingCache) Set(key string, vector []float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.list.MoveToFront(elem)
		entry := elem.Value.(*lruEmbeddingCacheEntry)
		vec := make([]float64, len(vector))
		copy(vec, vector)
		entry.vector = vec
		return
	}

	if c.list.Len() >= c.capacity {
		oldest := c.list.Back()
		if oldest != nil {
			c.list.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEmbeddingCacheEntry).key)
		}
	}

	vec := make([]float64, len(vector))
	copy(vec, vector)
	elem := c.list.PushFront(&lruEmbeddingCacheEntry{key: key, vector: vec})
	c.items[key] = elem
}

// Len returns the current number of cached entries.
func (c *lruEmbeddingCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}
