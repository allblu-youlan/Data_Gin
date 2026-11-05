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
	if allowed, _, err := breaker.Allow(ctx); err != nil || !allowed {
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
	if allowed, _, err := breaker.Allow(ctx); err != nil || allowed {
		t.Fatalf("late success reopened circuit before cooldown: allowed=%t error=%v", allowed, err)
	}
	if got, err := client.HGet(ctx, key, "opened_ms").Result(); err != nil || got != openedAt {
		t.Fatalf("opened_ms=%q error=%v, want %q", got, err, openedAt)
	}
}

func TestRedisCircuitBreakerRateLimitImmediatelyOpensSharedCooldown(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryAfter time.Duration
		wantDelay  time.Duration
	}{
		{name: "missing retry after", wantDelay: time.Minute},
		{name: "short retry after", retryAfter: 5 * time.Second, wantDelay: time.Minute},
		{name: "long retry after", retryAfter: 3 * time.Minute, wantDelay: 3 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, breaker, key := newCircuitBreakerRedisFixture(t)
			ctx := t.Context()
			before, err := client.Time(ctx).Result()
			if err != nil {
				t.Fatal(err)
			}
			if err := breaker.ReportRateLimited(ctx, test.retryAfter); err != nil {
				t.Fatal(err)
			}
			other, err := NewRedisCircuitBreaker(client, RedisCircuitBreakerConfig{
				Key: key, FailureThreshold: 2, OpenTimeout: time.Minute, ProbeTTL: 10 * time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			if allowed, _, err := other.Allow(ctx); err != nil || allowed {
				t.Fatalf("first rate limit must block another worker: allowed=%t error=%v", allowed, err)
			}
			until, err := client.HGet(ctx, key, "open_until_ms").Int64()
			if err != nil || until < before.Add(test.wantDelay).UnixMilli() {
				t.Fatalf("open_until_ms=%d error=%v, want at least %d", until, err, before.Add(test.wantDelay).UnixMilli())
			}
			ttl, err := client.PTTL(ctx, key).Result()
			if err != nil || ttl < test.wantDelay {
				t.Fatalf("state TTL=%s error=%v, want longer than cooldown %s", ttl, err, test.wantDelay)
			}
		})
	}
}

func TestRedisCircuitBreakerPreservesLongerCooldownAndTTL(t *testing.T) {
	for _, test := range []struct {
		name   string
		report func(context.Context, *RedisCircuitBreaker) error
	}{
		{name: "short rate limit", report: func(ctx context.Context, breaker *RedisCircuitBreaker) error {
			return breaker.ReportRateLimited(ctx, 5*time.Second)
		}},
		{name: "ordinary failure", report: func(ctx context.Context, breaker *RedisCircuitBreaker) error {
			return breaker.ReportFailure(ctx)
		}},
		{name: "late success", report: func(ctx context.Context, breaker *RedisCircuitBreaker) error {
			return breaker.ReportSuccess(ctx)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, breaker, key := newCircuitBreakerRedisFixture(t)
			ctx := t.Context()
			now, err := client.Time(ctx).Result()
			if err != nil {
				t.Fatal(err)
			}
			if err := client.HSet(ctx, key, map[string]interface{}{
				"state": "open", "failures": 2, "opened_ms": now.UnixMilli(),
				"open_until_ms": now.Add(3 * time.Minute).UnixMilli(), "probe_until_ms": 0,
			}).Err(); err != nil {
				t.Fatal(err)
			}
			until, err := client.HGet(ctx, key, "open_until_ms").Result()
			if err != nil {
				t.Fatal(err)
			}
			if err := client.PExpire(ctx, key, 5*time.Minute).Err(); err != nil {
				t.Fatal(err)
			}
			if err := test.report(ctx, breaker); err != nil {
				t.Fatal(err)
			}
			if got, err := client.HGet(ctx, key, "open_until_ms").Result(); err != nil || got != until {
				t.Fatalf("open_until_ms=%q error=%v, want %q", got, err, until)
			}
			if ttl, err := client.PTTL(ctx, key).Result(); err != nil || ttl < 4*time.Minute {
				t.Fatalf("longer state TTL was shortened: TTL=%s error=%v", ttl, err)
			}
			if allowed, _, err := breaker.Allow(ctx); err != nil || allowed {
				t.Fatalf("existing cooldown was bypassed: allowed=%t error=%v", allowed, err)
			}
		})
	}
}

func TestRedisCircuitBreakerAllowsFromDeadlineAndLegacyState(t *testing.T) {
	for _, test := range []struct {
		name        string
		openedAgo   time.Duration
		remaining   time.Duration
		wantAllowed bool
	}{
		{name: "legacy still open", openedAgo: 30 * time.Second},
		{name: "legacy expired", openedAgo: 61 * time.Second, wantAllowed: true},
		{name: "extended deadline", openedAgo: 2 * time.Minute, remaining: time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, breaker, key := newCircuitBreakerRedisFixture(t)
			ctx := t.Context()
			now, err := client.Time(ctx).Result()
			if err != nil {
				t.Fatal(err)
			}
			state := map[string]interface{}{
				"state": "open", "failures": 2, "opened_ms": now.Add(-test.openedAgo).UnixMilli(), "probe_until_ms": 0,
			}
			if test.remaining > 0 {
				state["open_until_ms"] = now.Add(test.remaining).UnixMilli()
			}
			if err := client.HSet(ctx, key, state).Err(); err != nil {
				t.Fatal(err)
			}
			if err := client.PExpire(ctx, key, 2*time.Minute).Err(); err != nil {
				t.Fatal(err)
			}
			allowed, remaining, err := breaker.Allow(ctx)
			if err != nil || allowed != test.wantAllowed {
				t.Fatalf("Allow()=%t error=%v, want %t", allowed, err, test.wantAllowed)
			}
			wantRemaining := time.Duration(0)
			if !test.wantAllowed {
				wantRemaining = time.Minute - test.openedAgo
				if test.remaining > 0 {
					wantRemaining = test.remaining
				}
			}
			if remaining > wantRemaining || remaining < wantRemaining-time.Second {
				t.Fatalf("remaining=%s, want approximately %s", remaining, wantRemaining)
			}
		})
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
	if err := client.HSet(ctx, key,
		"opened_ms", now.Add(-time.Minute-time.Second).UnixMilli(),
		"open_until_ms", now.Add(-time.Second).UnixMilli(),
	).Err(); err != nil {
		t.Fatal(err)
	}
	if allowed, _, err := breaker.Allow(ctx); err != nil || !allowed {
		t.Fatalf("half-open probe Allow()=%t error=%v", allowed, err)
	}
	if allowed, remaining, err := breaker.Allow(ctx); err != nil || allowed || remaining <= 0 || remaining > 10*time.Second {
		t.Fatalf("concurrent half-open probe Allow()=%t remaining=%s error=%v", allowed, remaining, err)
	}
	if err := breaker.ReportSuccess(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, _, err := breaker.Allow(ctx); err != nil || !allowed {
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
	if allowed, _, err := breaker.Allow(ctx); err != nil || !allowed {
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
