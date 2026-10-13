//go:build integration

package feishu

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

func TestMentionSendLimiterSharesRedisQuota(t *testing.T) {
	addr := os.Getenv("FEISHU_MENTION_REDIS_ADDR")
	if addr == "" {
		t.Skip("FEISHU_MENTION_REDIS_ADDR is required for Redis integration tests")
	}
	client := redis.NewClient(&redis.Options{Addr: addr, DB: 15, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: 1})
	t.Cleanup(func() { _ = client.Close() })
	cleanupKey := func(key string) {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = client.Del(ctx, key).Err()
		})
	}
	appID := "integration-" + uuid.NewString()
	first, err := NewMentionSendLimiter(client, appID, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMentionSendLimiter(client, appID, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := first.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	// Extend the actual first permit to avoid a slow CI scheduler expiring it.
	if err := client.PExpire(ctx, first.key, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	cleanupKey(first.key)
	waitCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	// Cancellation may happen during either the Redis command or the timer;
	// both paths must stop without granting a second permit.
	if err := second.Wait(waitCtx); err == nil || !errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
		t.Fatalf("another worker bypassed the shared permit: %v", err)
	}
	other, err := NewMentionSendLimiter(client, appID+"-other", 1)
	if err != nil {
		t.Fatal(err)
	}
	cleanupKey(other.key)
	if err := other.Wait(ctx); err != nil {
		t.Fatalf("another application's quota was blocked: %v", err)
	}
	if err := client.Del(ctx, first.key).Err(); err != nil {
		t.Fatal(err)
	}
	if err := second.Wait(ctx); err != nil {
		t.Fatalf("worker failed to acquire a released permit: %v", err)
	}
}
