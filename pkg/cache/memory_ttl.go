package cache

import (
	"context"
	"sync"
	"time"
)

type memoryTTLEntry[V any] struct {
	value     V
	expiresAt time.Time
}

type memoryTTLLoad[V any] struct {
	done  chan struct{}
	value V
	err   error
	retry bool
}

// MemoryTTL stores a bounded number of short-lived values within one process.
// Values are copied on access. Callers must keep referenced maps or slices immutable.
// A nil cache is disabled, allowing callers to fall back to their data source.
type MemoryTTL[K comparable, V any] struct {
	mu         sync.Mutex
	entries    map[K]memoryTTLEntry[V]
	loads      map[K]*memoryTTLLoad[V]
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
		loads: make(map[K]*memoryTTLLoad[V]),
	}
}

func (cache *MemoryTTL[K, V]) Get(key K) (V, bool) {
	var zero V
	if cache == nil {
		return zero, false
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return cache.getLocked(key)
}

func (cache *MemoryTTL[K, V]) getLocked(key K) (V, bool) {
	var zero V
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

// GetOrLoad shares one in-flight load per key and caches only successful results.
// Waiting callers can cancel independently. If the loading caller is canceled,
// active waiting callers retry with their own context and loader.
func (cache *MemoryTTL[K, V]) GetOrLoad(ctx context.Context, key K, load func(context.Context) (V, error)) (V, error) {
	var zero V
	for {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		if cache == nil {
			return load(ctx)
		}
		cache.mu.Lock()
		if value, found := cache.getLocked(key); found {
			cache.mu.Unlock()
			return value, nil
		}
		if pending, found := cache.loads[key]; found {
			cache.mu.Unlock()
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-pending.done:
			}
			if pending.retry {
				continue
			}
			return pending.value, pending.err
		}
		pending := &memoryTTLLoad[V]{done: make(chan struct{}), retry: true}
		cache.loads[key] = pending
		cache.mu.Unlock()
		return cache.loadAndStore(ctx, key, load, pending)
	}
}

func (cache *MemoryTTL[K, V]) loadAndStore(ctx context.Context, key K, load func(context.Context) (V, error), pending *memoryTTLLoad[V]) (V, error) {
	// Always release waiting callers, including when a loader panics upstream.
	defer func() {
		cache.mu.Lock()
		delete(cache.loads, key)
		close(pending.done)
		cache.mu.Unlock()
	}()
	pending.value, pending.err = load(ctx)
	if pending.err == nil {
		cache.Set(key, pending.value)
	}
	pending.retry = pending.err != nil && ctx.Err() != nil
	return pending.value, pending.err
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
