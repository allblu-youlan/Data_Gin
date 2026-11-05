package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type cacheLoadResult struct {
	value int
	err   error
}

// Observing Done makes waiting for the shared load deterministic in these tests.
type cacheWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (ctx *cacheWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.waiting) })
	return ctx.Context.Done()
}

func TestMemoryTTLGetOrLoadSharesConcurrentMissesAndErrors(t *testing.T) {
	for _, loadErr := range []error{nil, errors.New("database unavailable")} {
		t.Run(fmt.Sprintf("error=%v", loadErr), func(t *testing.T) {
			cache := NewMemoryTTL[string, int](time.Minute, 2)
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			load := func(context.Context) (int, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return 42, loadErr
			}
			results := make(chan cacheLoadResult, 32)
			go func() {
				value, err := cache.GetOrLoad(t.Context(), "key", load)
				results <- cacheLoadResult{value, err}
			}()
			<-started
			for range 31 {
				ctx := &cacheWaitContext{Context: t.Context(), waiting: make(chan struct{})}
				go func() {
					value, err := cache.GetOrLoad(ctx, "key", load)
					results <- cacheLoadResult{value, err}
				}()
				<-ctx.waiting
			}
			close(release)
			for range 32 {
				result := <-results
				if result.value != 42 || !errors.Is(result.err, loadErr) {
					t.Fatalf("shared result=%+v", result)
				}
			}
			if calls.Load() != 1 || len(cache.loads) != 0 {
				t.Fatalf("loads=%d pending=%d, want one completed load", calls.Load(), len(cache.loads))
			}
			value, err := cache.GetOrLoad(t.Context(), "key", func(context.Context) (int, error) {
				calls.Add(1)
				return 42, nil
			})
			wantCalls := int32(1)
			if loadErr != nil {
				wantCalls++
			}
			if err != nil || value != 42 || calls.Load() != wantCalls {
				t.Fatalf("reload=%d err=%v calls=%d, want calls=%d", value, err, calls.Load(), wantCalls)
			}
		})
	}
}

func TestMemoryTTLGetOrLoadIsolatesCallerCancellation(t *testing.T) {
	for _, cancelLeader := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel leader=%t", cancelLeader), func(t *testing.T) {
			cache := NewMemoryTTL[string, int](time.Minute, 2)
			leaderCtx, cancelLeaderCtx := context.WithCancel(t.Context())
			defer cancelLeaderCtx()
			waiterCtx, cancelWaiterCtx := context.WithCancel(t.Context())
			defer cancelWaiterCtx()
			waitCtx := &cacheWaitContext{Context: waiterCtx, waiting: make(chan struct{})}
			started, release := make(chan struct{}), make(chan struct{})
			leaderResult, waiterResult := make(chan cacheLoadResult, 1), make(chan cacheLoadResult, 1)
			go func() {
				value, err := cache.GetOrLoad(leaderCtx, "key", func(ctx context.Context) (int, error) {
					close(started)
					select {
					case <-ctx.Done():
						return 0, ctx.Err()
					case <-release:
						return 42, nil
					}
				})
				leaderResult <- cacheLoadResult{value, err}
			}()
			<-started
			go func() {
				value, err := cache.GetOrLoad(waitCtx, "key", func(context.Context) (int, error) { return 42, nil })
				waiterResult <- cacheLoadResult{value, err}
			}()
			<-waitCtx.waiting
			if cancelLeader {
				cancelLeaderCtx()
				if result := <-leaderResult; !errors.Is(result.err, context.Canceled) {
					t.Fatalf("canceled leader=%+v", result)
				}
				if result := <-waiterResult; result.err != nil || result.value != 42 {
					t.Fatalf("waiting caller failed to retry=%+v", result)
				}
			} else {
				cancelWaiterCtx()
				if result := <-waiterResult; !errors.Is(result.err, context.Canceled) {
					t.Fatalf("canceled waiting caller=%+v", result)
				}
				close(release)
				if result := <-leaderResult; result.err != nil || result.value != 42 {
					t.Fatalf("independent loader=%+v", result)
				}
			}
		})
	}
}

func TestMemoryTTLGetOrLoadAllowsDifferentKeysAndDisabledCache(t *testing.T) {
	cache := NewMemoryTTL[string, int](time.Minute, 2)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.GetOrLoad(t.Context(), "slow", func(context.Context) (int, error) {
			close(started)
			<-release
			return 1, nil
		})
	}()
	<-started
	defer func() { close(release); <-done }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	value, err := cache.GetOrLoad(ctx, "fast", func(context.Context) (int, error) { return 2, nil })
	if value != 2 || err != nil {
		t.Fatalf("different key=%d err=%v", value, err)
	}
	var disabled *MemoryTTL[string, int]
	value, err = disabled.GetOrLoad(ctx, "key", func(context.Context) (int, error) { return 3, nil })
	if value != 3 || err != nil {
		t.Fatalf("disabled cache=%d err=%v", value, err)
	}
}

func TestMemoryTTLGetOrLoadClearsLoadAfterPanic(t *testing.T) {
	cache := NewMemoryTTL[string, int](time.Minute, 2)
	func() {
		defer func() {
			if recover() != "loader failed" {
				t.Fatal("loader panic was swallowed")
			}
		}()
		_, _ = cache.GetOrLoad(t.Context(), "key", func(context.Context) (int, error) { panic("loader failed") })
	}()
	value, err := cache.GetOrLoad(t.Context(), "key", func(context.Context) (int, error) { return 42, nil })
	if value != 42 || err != nil || len(cache.loads) != 0 {
		t.Fatalf("load after panic=%d err=%v pending=%d", value, err, len(cache.loads))
	}
}
