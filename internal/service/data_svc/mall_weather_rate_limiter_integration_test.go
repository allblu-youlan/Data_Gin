//go:build integration

package data_svc

import (
	"context"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

func TestMallWeatherRateLimiterSharesPacedQuota(t *testing.T) {
	addr := os.Getenv("MALL_WEATHER_REDIS_ADDR")
	if addr == "" {
		t.Skip("MALL_WEATHER_REDIS_ADDR is required for Redis integration tests")
	}
	client := redisv8.NewClient(&redisv8.Options{
		Addr: addr, DB: 15, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: 1,
	})
	t.Cleanup(func() { _ = client.Close() })
	key := "integration:mall-weather:rate:" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Errorf("clean rate limiter key: %v", err)
		}
	})
	first, err := newMallWeatherRateLimiter(client, key, 8)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newMallWeatherRateLimiter(client, key, 8)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ready := make(chan struct{})
	results := make(chan time.Duration, 20)
	failures := make(chan error, 20)
	var workers sync.WaitGroup
	started := time.Now()
	for index := range 20 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-ready
			limiter := first
			if index%2 == 1 {
				limiter = second
			}
			if err := limiter.Wait(ctx); err != nil {
				failures <- err
				return
			}
			results <- time.Since(started)
		}()
	}
	close(ready)
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Errorf("Wait() error=%v", err)
	}
	var offsets []time.Duration
	for elapsed := range results {
		offsets = append(offsets, elapsed)
	}
	sort.Slice(offsets, func(left, right int) bool { return offsets[left] < offsets[right] })
	firstSecond := 0
	for _, elapsed := range offsets {
		if elapsed < time.Second {
			firstSecond++
		}
	}
	if len(offsets) != 20 || firstSecond > 8 {
		t.Fatalf("two workers at 8 QPS granted %d permits in the first second, total=%d offsets=%v", firstSecond, len(offsets), offsets)
	}
	t.Logf("two limiter instances, 20 concurrent requests, first second permits=%d, offsets=%v", firstSecond, offsets)
}
