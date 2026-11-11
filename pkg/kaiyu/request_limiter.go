package kaiyu

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

// RequestLimiter lets application runtimes share the credential's request budget.
type RequestLimiter interface{ Wait(context.Context) error }

const requestInterval = 200 * time.Millisecond

var credentialLimiters sync.Map

type localRequestLimiter struct {
	mu   sync.Mutex
	next time.Time
}

func sharedRequestLimiter(securityID string) RequestLimiter {
	key := sha256.Sum256([]byte(securityID))
	limiter, _ := credentialLimiters.LoadOrStore(key, &localRequestLimiter{})
	return limiter.(*localRequestLimiter)
}

func (l *localRequestLimiter) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.mu.Lock()
		delay := time.Until(l.next)
		if delay <= 0 {
			l.next = time.Now().Add(requestInterval)
		}
		l.mu.Unlock()
		if delay <= 0 {
			return nil
		}
		if err := waitRequestBudget(ctx, delay); err != nil {
			return err
		}
	}
}

const redisRequestBudgetScript = `
local clock = redis.call('TIME')
local now = clock[1] * 1000 + math.floor(clock[2] / 1000)
local next = tonumber(redis.call('GET', KEYS[1])) or 0
if next > now then return next - now end
redis.call('SET', KEYS[1], now + tonumber(ARGV[1]), 'PX', tonumber(ARGV[1]) * 2)
return 0
`

type redisBudgetClient interface {
	Eval(context.Context, string, []string, ...interface{}) *redisv8.Cmd
}

type redisRequestLimiter struct {
	client redisBudgetClient
	key    string
}

// NewRedisRequestLimiter enforces five requests/second per credential across
// processes. Redis failure stops requests instead of exceeding the shared budget.
func NewRedisRequestLimiter(client *redisv8.Client, securityID string) (RequestLimiter, error) {
	if client == nil || strings.TrimSpace(securityID) == "" {
		return nil, fmt.Errorf("kaiyu: redis client and security id are required")
	}
	return &redisRequestLimiter{client: client, key: fmt.Sprintf("kaiyu:request-budget:%x", sha256.Sum256([]byte(securityID)))}, nil
}

func (l *redisRequestLimiter) Wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		waitMS, err := l.client.Eval(ctx, redisRequestBudgetScript, []string{l.key}, requestInterval.Milliseconds()).Int64()
		if err != nil {
			return fmt.Errorf("read Kaiyu shared request budget: %w", err)
		}
		if waitMS < 0 || waitMS > requestInterval.Milliseconds() {
			return fmt.Errorf("kaiyu: invalid shared request budget response")
		}
		if waitMS == 0 {
			return nil
		}
		if err := waitRequestBudget(ctx, time.Duration(waitMS)*time.Millisecond); err != nil {
			return err
		}
	}
}

func waitRequestBudget(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
