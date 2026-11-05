package cache

import (
	"testing"
	"time"
)

func TestMemoryTTLExpiresValuesAndBoundsEntries(t *testing.T) {
	now := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	cache := NewMemoryTTL[string, int](30*time.Second, 2)
	cache.now = func() time.Time { return now }
	cache.Set("first", 10)
	now = now.Add(time.Second)
	cache.Set("second", 20)
	cache.Set("third", 30)
	if _, found := cache.Get("first"); found {
		t.Fatal("oldest entry was retained after exceeding capacity")
	}
	if value, found := cache.Get("second"); !found || value != 20 {
		t.Fatalf("second entry = %d, found=%t", value, found)
	}
	now = now.Add(30 * time.Second)
	if _, found := cache.Get("second"); found {
		t.Fatal("expired value was returned")
	}
	cache.Set("fresh", 40)
	if value, found := cache.Get("fresh"); !found || value != 40 {
		t.Fatalf("fresh entry = %d, found=%t", value, found)
	}
}

func TestMemoryTTLCanBeDisabledAndCachesZeroValues(t *testing.T) {
	var disabled *MemoryTTL[string, int]
	disabled.Set("key", 1)
	if _, found := disabled.Get("key"); found {
		t.Fatal("disabled cache returned a hit")
	}
	cache := NewMemoryTTL[string, int](time.Minute, 1)
	cache.Set("zero", 0)
	if value, found := cache.Get("zero"); !found || value != 0 {
		t.Fatalf("zero value = %d, found=%t", value, found)
	}
}
