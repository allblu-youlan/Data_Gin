package cache

import (
	"sync"
	"time"
)

type memoryTTLEntry[V any] struct {
	value     V
	expiresAt time.Time
}

// MemoryTTL stores a bounded number of short-lived values within one process.
// Values are copied on access. Callers must keep referenced maps or slices immutable.
// A nil cache is disabled, allowing callers to fall back to their data source.
type MemoryTTL[K comparable, V any] struct {
	mu         sync.Mutex
	entries    map[K]memoryTTLEntry[V]
	ttl        time.Duration
	maxEntries int
	now        func() time.Time
}

func NewMemoryTTL[K comparable, V any](ttl time.Duration, maxEntries int) *MemoryTTL[K, V] {
	if ttl <= 0 || maxEntries <= 0 {
		panic("cache: TTL and capacity must be positive")
	}
	return &MemoryTTL[K, V]{
		entries: make(map[K]memoryTTLEntry[V]), ttl: ttl, maxEntries: maxEntries, now: time.Now,
	}
}

func (cache *MemoryTTL[K, V]) Get(key K) (V, bool) {
	var zero V
	if cache == nil {
		return zero, false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, exists := cache.entries[key]
	if !exists {
		return zero, false
	}
	if !cache.now().Before(entry.expiresAt) {
		delete(cache.entries, key)
		return zero, false
	}
	return entry.value, true
}

func (cache *MemoryTTL[K, V]) Set(key K, value V) {
	if cache == nil {
		return
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := cache.now()
	if _, exists := cache.entries[key]; !exists && len(cache.entries) >= cache.maxEntries {
		var oldestKey K
		var oldestExpiry time.Time
		for candidate, entry := range cache.entries {
			if !now.Before(entry.expiresAt) {
				delete(cache.entries, candidate)
				continue
			}
			if oldestExpiry.IsZero() || entry.expiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = candidate, entry.expiresAt
			}
		}
		if len(cache.entries) >= cache.maxEntries {
			delete(cache.entries, oldestKey)
		}
	}
	cache.entries[key] = memoryTTLEntry[V]{value: value, expiresAt: now.Add(cache.ttl)}
}
