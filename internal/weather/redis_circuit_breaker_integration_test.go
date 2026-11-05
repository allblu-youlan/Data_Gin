//go:build integration

package weather

import (
	"context"
	"os"
	"testing"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

func TestRedisCircuitBreakerKeepsOpenAfterLateSuccess(t *testing.T) {
	client, breaker, key := newCircuitBreakerRedisFixture(t)
	ctx := t.Context()
	if allowed, err := breaker.Allow(ctx); err != nil || !allowed {
		t.Fatalf("initial Allow()=%t error=%v", allowed, err)
	}
	for range 2 {
		if err := breaker.ReportFailure(ctx); err != nil {
			t.Fatal(err)
		}
	}
	openedAt, err := client.HGet(ctx, key, "opened_ms").Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := breaker.ReportSuccess(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, err := breaker.Allow(ctx); err != nil || allowed {
		t.Fatalf("late success reopened circuit before cooldown: allowed=%t error=%v", allowed, err)
	}
	if got, err := client.HGet(ctx, key, "opened_ms").Result(); err != nil || got != openedAt {
		t.Fatalf("opened_ms=%q error=%v, want %q", got, err, openedAt)
	}
}

func TestRedisCircuitBreakerClosesAfterHalfOpenSuccess(t *testing.T) {
	client, breaker, key := newCircuitBreakerRedisFixture(t)
	ctx := t.Context()
	for range 2 {
		if err := breaker.ReportFailure(ctx); err != nil {
			t.Fatal(err)
		}
	}
	now, err := client.Time(ctx).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, key, "opened_ms", now.Add(-time.Minute-time.Second).UnixMilli()).Err(); err != nil {
		t.Fatal(err)
	}
	if allowed, err := breaker.Allow(ctx); err != nil || !allowed {
		t.Fatalf("half-open probe Allow()=%t error=%v", allowed, err)
	}
	if allowed, err := breaker.Allow(ctx); err != nil || allowed {
		t.Fatalf("concurrent half-open probe Allow()=%t error=%v", allowed, err)
	}
	if err := breaker.ReportSuccess(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, err := breaker.Allow(ctx); err != nil || !allowed {
		t.Fatalf("recovered circuit Allow()=%t error=%v", allowed, err)
	}
}

func TestRedisCircuitBreakerResetsClosedFailureCount(t *testing.T) {
	client, breaker, key := newCircuitBreakerRedisFixture(t)
	ctx := t.Context()
	if err := breaker.ReportFailure(ctx); err != nil {
		t.Fatal(err)
	}
	if err := breaker.ReportSuccess(ctx); err != nil {
		t.Fatal(err)
	}
	if count, err := client.Exists(ctx, key).Result(); err != nil || count != 0 {
		t.Fatalf("successful closed request retained failure state: count=%d error=%v", count, err)
	}
	if err := breaker.ReportFailure(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, err := breaker.Allow(ctx); err != nil || !allowed {
		t.Fatalf("fresh failure should stay below threshold: allowed=%t error=%v", allowed, err)
	}
}

func newCircuitBreakerRedisFixture(t *testing.T) (*redisv8.Client, *RedisCircuitBreaker, string) {
	t.Helper()
	addr := os.Getenv("MALL_WEATHER_REDIS_ADDR")
	if addr == "" {
		t.Skip("MALL_WEATHER_REDIS_ADDR is required for Redis integration tests")
	}
	client := redisv8.NewClient(&redisv8.Options{
		Addr: addr, DB: 15, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: 1,
	})
	t.Cleanup(func() { _ = client.Close() })
	key := "integration:mall-weather:circuit:" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Errorf("clean circuit breaker key: %v", err)
		}
	})
	breaker, err := NewRedisCircuitBreaker(client, RedisCircuitBreakerConfig{
		Key: key, FailureThreshold: 2, OpenTimeout: time.Minute, ProbeTTL: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, breaker, key
}
