package weather

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

func TestRedisTaskLockerAcquiresAndReleasesOwnedLock(t *testing.T) {
	client := &fakeRedisTaskLockClient{setNXResult: true, evalResult: int64(1)}
	locker, err := newRedisTaskLocker(client, "app:lock:mall_weather:", 10*time.Minute, func() (string, error) {
		return "token-1", nil
	})
	if err != nil {
		t.Fatalf("newRedisTaskLocker() error=%v", err)
	}
	lock, acquired, err := locker.Acquire(context.Background(), "7:full:full:7:2026072203")
	if err != nil || !acquired || lock == nil {
		t.Fatalf("Acquire() lock=%v acquired=%t error=%v", lock, acquired, err)
	}
	if client.setNXKey != "app:lock:mall_weather:7:full:full:7:2026072203" || client.setNXValue != "token-1" || client.setNXTTL != 10*time.Minute {
		t.Fatalf("SetNX call=%+v", client)
	}
	if err := lock.Release(context.Background()); err != nil {
		t.Fatalf("Release() error=%v", err)
	}
	if client.evalCalls != 1 || client.evalKeys[0] != client.setNXKey || client.evalArgs[0] != "token-1" {
		t.Fatalf("Eval call=%+v", client)
	}
	if err := lock.Release(context.Background()); err != nil || client.evalCalls != 1 {
		t.Fatalf("second Release() error=%v calls=%d", err, client.evalCalls)
	}
}

func TestRedisTaskLockRenewsOnlyOwnedLease(t *testing.T) {
	tests := []struct {
		name       string
		evalResult int64
		wantLost   bool
	}{
		{name: "owned", evalResult: 1},
		{name: "lost", evalResult: 0, wantLost: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeRedisTaskLockClient{setNXResult: true, evalResult: test.evalResult}
			locker, err := newRedisTaskLocker(client, "app:lock:", 2*time.Minute, func() (string, error) {
				return "token-renew", nil
			})
			if err != nil {
				t.Fatalf("newRedisTaskLocker() error=%v", err)
			}
			lock, acquired, err := locker.Acquire(t.Context(), "cron:data_collect")
			if err != nil || !acquired {
				t.Fatalf("Acquire() acquired=%t error=%v", acquired, err)
			}
			renewable, ok := lock.(RenewableTaskLock)
			if !ok {
				t.Fatal("acquired redis lock is not renewable")
			}
			err = renewable.Renew(t.Context())
			if test.wantLost && !errors.Is(err, ErrTaskLockLeaseLost) {
				t.Fatalf("Renew() error=%v wantLost=%t", err, test.wantLost)
			}
			if !test.wantLost && err != nil {
				t.Fatalf("Renew() error=%v, want nil", err)
			}
			if client.evalCalls != 1 || client.evalKeys[0] != "app:lock:cron:data_collect" ||
				client.evalArgs[0] != "token-renew" || client.evalArgs[1] != int64((2*time.Minute)/time.Millisecond) {
				t.Fatalf("renew Eval call=%+v", client)
			}
			const expectedRenewScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`
			if client.evalScript != expectedRenewScript {
				t.Fatalf("renew script is not token-fenced: %s", client.evalScript)
			}
		})
	}
}

func TestRedisTaskLockRenewFailsClosedOnRedisError(t *testing.T) {
	client := &fakeRedisTaskLockClient{setNXResult: true, evalErr: errors.New("redis unavailable")}
	locker, err := newRedisTaskLocker(client, "app:lock:", time.Minute, func() (string, error) {
		return "token", nil
	})
	if err != nil {
		t.Fatalf("newRedisTaskLocker() error=%v", err)
	}
	lock, _, err := locker.Acquire(t.Context(), "cron:data_collect")
	if err != nil {
		t.Fatalf("Acquire() error=%v", err)
	}
	err = lock.(RenewableTaskLock).Renew(t.Context())
	if err == nil || errors.Is(err, ErrTaskLockLeaseLost) {
		t.Fatalf("Renew() error=%v, want Redis error", err)
	}
}

func TestRedisTaskLockLetsRedisOrderRenewWithRelease(t *testing.T) {
	client := newOrderedRedisTaskLockClient()
	locker, err := newRedisTaskLocker(client, "app:lock:", time.Minute, func() (string, error) {
		return "token", nil
	})
	if err != nil {
		t.Fatalf("newRedisTaskLocker() error=%v", err)
	}
	lock, _, err := locker.Acquire(t.Context(), "cron:data_collect")
	if err != nil {
		t.Fatalf("Acquire() error=%v", err)
	}
	releaseResult := make(chan error, 1)
	go func() {
		releaseResult <- lock.Release(t.Context())
	}()
	<-client.releaseStarted
	if err := lock.(RenewableTaskLock).Renew(t.Context()); err != nil {
		t.Fatalf("Renew() during release error=%v", err)
	}
	close(client.releaseContinue)
	if err := <-releaseResult; err != nil {
		t.Fatalf("Release() error=%v", err)
	}
	if client.isHeld() {
		t.Fatal("lease remained held after release")
	}
	if err := lock.(RenewableTaskLock).Renew(t.Context()); !errors.Is(err, ErrTaskLockLeaseLost) {
		t.Fatalf("Renew() after release error=%v, want lease lost", err)
	}
}

func TestRedisTaskLockerClampsSubMillisecondTTL(t *testing.T) {
	client := &fakeRedisTaskLockClient{setNXResult: true, evalResult: int64(1)}
	locker, err := newRedisTaskLocker(client, "app:lock:", time.Microsecond, func() (string, error) {
		return "token", nil
	})
	if err != nil {
		t.Fatalf("newRedisTaskLocker() error=%v", err)
	}
	lock, acquired, err := locker.Acquire(t.Context(), "cron:data_collect")
	if err != nil || !acquired {
		t.Fatalf("Acquire() acquired=%t error=%v", acquired, err)
	}
	if client.setNXTTL != time.Millisecond {
		t.Fatalf("SetNX TTL=%v, want 1ms", client.setNXTTL)
	}
	if err := lock.(RenewableTaskLock).Renew(t.Context()); err != nil {
		t.Fatalf("Renew() error=%v", err)
	}
	if client.evalArgs[1] != int64(1) {
		t.Fatalf("renew TTL milliseconds=%v, want 1", client.evalArgs[1])
	}
	if _, err := newRedisTaskLocker(client, "app:lock:", time.Millisecond, func() (string, error) {
		return "token", nil
	}); err != nil {
		t.Fatalf("newRedisTaskLocker(1ms) error=%v", err)
	}
}

func TestRedisTaskLockerDoesNotBypassContentionOrRedisFailure(t *testing.T) {
	tests := []struct {
		name       string
		client     *fakeRedisTaskLockClient
		wantLocked bool
		wantError  bool
	}{
		{name: "already owned", client: &fakeRedisTaskLockClient{}, wantLocked: false},
		{name: "redis unavailable", client: &fakeRedisTaskLockClient{setNXErr: errors.New("redis unavailable")}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			locker, err := newRedisTaskLocker(test.client, "app:lock:", time.Minute, func() (string, error) {
				return "token", nil
			})
			if err != nil {
				t.Fatalf("newRedisTaskLocker() error=%v", err)
			}
			_, acquired, err := locker.Acquire(context.Background(), "7:full:window")
			if acquired != test.wantLocked || (err != nil) != test.wantError {
				t.Fatalf("Acquire() acquired=%t error=%v", acquired, err)
			}
		})
	}
}

func TestRedisTaskLockerRejectsUnsafeKey(t *testing.T) {
	locker, err := newRedisTaskLocker(&fakeRedisTaskLockClient{}, "app:lock:", time.Minute, func() (string, error) {
		return "token", nil
	})
	if err != nil {
		t.Fatalf("newRedisTaskLocker() error=%v", err)
	}
	if _, _, err := locker.Acquire(context.Background(), "../unsafe"); err == nil {
		t.Fatal("Acquire() accepted unsafe key")
	}
}

func TestRedisTaskLockAllowsReleaseRetryAfterRedisFailure(t *testing.T) {
	client := &fakeRedisTaskLockClient{setNXResult: true, evalErr: errors.New("redis unavailable")}
	locker, err := newRedisTaskLocker(client, "app:lock:", time.Minute, func() (string, error) {
		return "token", nil
	})
	if err != nil {
		t.Fatalf("newRedisTaskLocker() error=%v", err)
	}
	lock, acquired, err := locker.Acquire(context.Background(), "7:full:window")
	if err != nil || !acquired {
		t.Fatalf("Acquire() acquired=%t error=%v", acquired, err)
	}
	if err := lock.Release(context.Background()); err == nil {
		t.Fatal("Release() error=nil")
	}
	client.evalErr = nil
	client.evalResult = int64(1)
	if err := lock.Release(context.Background()); err != nil || client.evalCalls != 2 {
		t.Fatalf("retry Release() error=%v calls=%d", err, client.evalCalls)
	}
}

type fakeRedisTaskLockClient struct {
	setNXResult bool
	setNXErr    error
	setNXKey    string
	setNXValue  interface{}
	setNXTTL    time.Duration
	evalResult  interface{}
	evalErr     error
	evalCalls   int
	evalScript  string
	evalKeys    []string
	evalArgs    []interface{}
}

func (client *fakeRedisTaskLockClient) SetNX(_ context.Context, key string, value interface{}, ttl time.Duration) *redisv8.BoolCmd {
	client.setNXKey = key
	client.setNXValue = value
	client.setNXTTL = ttl
	command := redisv8.NewBoolCmd(context.Background())
	command.SetVal(client.setNXResult)
	command.SetErr(client.setNXErr)
	return command
}

func (client *fakeRedisTaskLockClient) Eval(_ context.Context, script string, keys []string, args ...interface{}) *redisv8.Cmd {
	client.evalCalls++
	client.evalScript = script
	client.evalKeys = append([]string(nil), keys...)
	client.evalArgs = append([]interface{}(nil), args...)
	command := redisv8.NewCmd(context.Background())
	command.SetVal(client.evalResult)
	command.SetErr(client.evalErr)
	return command
}

type orderedRedisTaskLockClient struct {
	mu              sync.Mutex
	held            bool
	token           string
	releaseStarted  chan struct{}
	releaseContinue chan struct{}
}

func newOrderedRedisTaskLockClient() *orderedRedisTaskLockClient {
	return &orderedRedisTaskLockClient{
		releaseStarted:  make(chan struct{}),
		releaseContinue: make(chan struct{}),
	}
}

func (client *orderedRedisTaskLockClient) SetNX(_ context.Context, _ string, value interface{}, _ time.Duration) *redisv8.BoolCmd {
	client.mu.Lock()
	client.held = true
	client.token, _ = value.(string)
	client.mu.Unlock()
	command := redisv8.NewBoolCmd(context.Background())
	command.SetVal(true)
	return command
}

func (client *orderedRedisTaskLockClient) Eval(ctx context.Context, script string, _ []string, args ...interface{}) *redisv8.Cmd {
	if script == releaseTaskLockScript {
		close(client.releaseStarted)
		select {
		case <-ctx.Done():
			command := redisv8.NewCmd(context.Background())
			command.SetErr(ctx.Err())
			return command
		case <-client.releaseContinue:
		}
	}
	client.mu.Lock()
	result := int64(0)
	if client.held && len(args) > 0 && args[0] == client.token {
		result = 1
		if script == releaseTaskLockScript {
			client.held = false
		}
	}
	client.mu.Unlock()
	command := redisv8.NewCmd(context.Background())
	command.SetVal(result)
	return command
}

func (client *orderedRedisTaskLockClient) isHeld() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.held
}
