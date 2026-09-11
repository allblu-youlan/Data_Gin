package weather

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

const releaseTaskLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

const renewTaskLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`

var ErrTaskLockLeaseLost = errors.New("task lock lease lost")

var taskLockKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9:_-]{0,255}$`)

type TaskLock interface {
	Release(ctx context.Context) error
}

type TaskLocker interface {
	Acquire(ctx context.Context, key string) (TaskLock, bool, error)
}

type RenewableTaskLock interface {
	TaskLock
	Renew(ctx context.Context) error
}

type redisTaskLockClient interface {
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *redisv8.BoolCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redisv8.Cmd
}

type RedisTaskLocker struct {
	client   redisTaskLockClient
	prefix   string
	ttl      time.Duration
	newToken func() (string, error)
}

func NewRedisTaskLocker(client *redisv8.Client, prefix string, ttl time.Duration) (*RedisTaskLocker, error) {
	return newRedisTaskLocker(client, prefix, ttl, randomTaskLockToken)
}

func newRedisTaskLocker(client redisTaskLockClient, prefix string, ttl time.Duration, newToken func() (string, error)) (*RedisTaskLocker, error) {
	if client == nil || prefix == "" || len(prefix) > 256 || ttl <= 0 || newToken == nil {
		return nil, fmt.Errorf("weather task lock: invalid configuration")
	}
	if ttl < time.Millisecond {
		ttl = time.Millisecond
	}
	return &RedisTaskLocker{client: client, prefix: prefix, ttl: ttl, newToken: newToken}, nil
}

func (locker *RedisTaskLocker) Acquire(ctx context.Context, key string) (TaskLock, bool, error) {
	if locker == nil || locker.client == nil || ctx == nil || !taskLockKeyPattern.MatchString(key) {
		return nil, false, fmt.Errorf("weather task lock: invalid acquisition")
	}
	token, err := locker.newToken()
	if err != nil {
		return nil, false, fmt.Errorf("weather task lock: generate token: %w", err)
	}
	redisKey := locker.prefix + key
	acquired, err := locker.client.SetNX(ctx, redisKey, token, locker.ttl).Result()
	if err != nil {
		return nil, false, fmt.Errorf("weather task lock: acquire: %w", err)
	}
	if !acquired {
		return nil, false, nil
	}
	return &redisTaskLock{client: locker.client, key: redisKey, token: token, ttl: locker.ttl}, true, nil
}

type redisTaskLock struct {
	client redisTaskLockClient
	key    string
	token  string
	ttl    time.Duration

	mu          sync.Mutex
	releaseDone chan struct{}
	releaseErr  error
	released    bool
}

func (lock *redisTaskLock) Renew(ctx context.Context) error {
	if lock == nil || lock.client == nil || ctx == nil || lock.ttl <= 0 {
		return fmt.Errorf("weather task lock: invalid renewal")
	}
	lock.mu.Lock()
	unavailable := lock.released
	lock.mu.Unlock()
	if unavailable {
		return ErrTaskLockLeaseLost
	}
	renewed, err := lock.client.Eval(
		ctx,
		renewTaskLockScript,
		[]string{lock.key},
		lock.token,
		lock.ttl.Milliseconds(),
	).Int64()
	if err != nil {
		return fmt.Errorf("weather task lock: renew: %w", err)
	}
	if renewed != 1 {
		return ErrTaskLockLeaseLost
	}
	return nil
}

func (lock *redisTaskLock) Release(ctx context.Context) error {
	if lock == nil || lock.client == nil || ctx == nil {
		return fmt.Errorf("weather task lock: invalid release")
	}
	lock.mu.Lock()
	if lock.released {
		lock.mu.Unlock()
		return nil
	}
	if lock.releaseDone != nil {
		done := lock.releaseDone
		lock.mu.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("weather task lock: wait for release: %w", ctx.Err())
		case <-done:
		}
		lock.mu.Lock()
		err := lock.releaseErr
		lock.mu.Unlock()
		return err
	}
	done := make(chan struct{})
	lock.releaseDone = done
	lock.mu.Unlock()

	_, err := lock.client.Eval(ctx, releaseTaskLockScript, []string{lock.key}, lock.token).Result()
	if err != nil {
		err = fmt.Errorf("weather task lock: release: %w", err)
	}
	lock.mu.Lock()
	lock.releaseErr = err
	lock.released = err == nil
	lock.releaseDone = nil
	close(done)
	lock.mu.Unlock()
	return err
}

func randomTaskLockToken() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
