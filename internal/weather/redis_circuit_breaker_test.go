package weather

import (
	"context"
	"errors"
	"testing"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

func TestRedisCircuitBreakerAllowsFromRedisState(t *testing.T) {
	client := &fakeRedisCircuitBreakerClient{results: []interface{}{
		[]interface{}{int64(1), "closed", int64(0)},
		[]interface{}{int64(0), "open", int64(60000)},
	}}
	breaker, err := newRedisCircuitBreaker(client, RedisCircuitBreakerConfig{
		Key: "app:circuit:caiyun", FailureThreshold: 3, OpenTimeout: time.Minute, ProbeTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("newRedisCircuitBreaker() error=%v", err)
	}

	allowed, remaining, err := breaker.Allow(context.Background())
	if err != nil || !allowed || remaining != 0 {
		t.Fatalf("Allow() allowed=%t remaining=%s error=%v, want allowed", allowed, remaining, err)
	}
	allowed, remaining, err = breaker.Allow(context.Background())
	if err != nil || allowed || remaining != time.Minute {
		t.Fatalf("Allow() allowed=%t remaining=%s error=%v, want blocked for one minute", allowed, remaining, err)
	}
	if len(client.calls) != 2 || client.calls[0].key != "app:circuit:caiyun" ||
		client.calls[0].args[0] != int64(time.Minute/time.Millisecond) {
		t.Fatalf("calls=%+v", client.calls)
	}
}

func TestRedisCircuitBreakerReportsSuccessAndFailure(t *testing.T) {
	client := &fakeRedisCircuitBreakerClient{results: []interface{}{
		[]interface{}{"open", int64(3)},
		[]interface{}{"closed", int64(0)},
	}}
	breaker, err := newRedisCircuitBreaker(client, RedisCircuitBreakerConfig{
		Key: "app:circuit:caiyun", FailureThreshold: 3, OpenTimeout: time.Minute, ProbeTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("newRedisCircuitBreaker() error=%v", err)
	}

	if err := breaker.ReportFailure(context.Background()); err != nil {
		t.Fatalf("ReportFailure() error=%v", err)
	}
	if err := breaker.ReportSuccess(context.Background()); err != nil {
		t.Fatalf("ReportSuccess() error=%v", err)
	}
	if len(client.calls) != 2 || client.calls[0].args[0] != 3 || client.calls[0].args[2] != 0 ||
		client.calls[1].args[2] != 1 {
		t.Fatalf("calls=%+v", client.calls)
	}
}

func TestRedisCircuitBreakerRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name string
		cfg  RedisCircuitBreakerConfig
	}{
		{name: "missing key", cfg: RedisCircuitBreakerConfig{FailureThreshold: 1, OpenTimeout: time.Second, ProbeTTL: time.Second}},
		{name: "missing threshold", cfg: RedisCircuitBreakerConfig{Key: "k", OpenTimeout: time.Second, ProbeTTL: time.Second}},
		{name: "missing open timeout", cfg: RedisCircuitBreakerConfig{Key: "k", FailureThreshold: 1, ProbeTTL: time.Second}},
		{name: "short ttl", cfg: RedisCircuitBreakerConfig{Key: "k", FailureThreshold: 1, OpenTimeout: time.Second, ProbeTTL: time.Second, StateTTL: time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newRedisCircuitBreaker(&fakeRedisCircuitBreakerClient{}, tt.cfg); err == nil {
				t.Fatalf("newRedisCircuitBreaker(%+v) error=nil", tt.cfg)
			}
		})
	}
}

func TestRedisCircuitBreakerPassesRateLimitMinimumDelay(t *testing.T) {
	for _, test := range []struct {
		name       string
		retryAfter time.Duration
		wantDelay  time.Duration
	}{
		{name: "missing delay", wantDelay: time.Minute},
		{name: "negative delay", retryAfter: -time.Second, wantDelay: time.Minute},
		{name: "long delay", retryAfter: 3 * time.Minute, wantDelay: 3 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeRedisCircuitBreakerClient{results: []interface{}{[]interface{}{"open", int64(3)}}}
			breaker, err := newRedisCircuitBreaker(client, RedisCircuitBreakerConfig{
				Key: "app:circuit:caiyun", FailureThreshold: 3, OpenTimeout: time.Minute, ProbeTTL: time.Second,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := breaker.ReportRateLimited(t.Context(), test.retryAfter); err != nil {
				t.Fatal(err)
			}
			if len(client.calls) != 1 || len(client.calls[0].args) != 5 ||
				client.calls[0].args[3] != time.Minute.Milliseconds() || client.calls[0].args[4] != test.wantDelay.Milliseconds() {
				t.Fatalf("rate limit report calls=%+v, want minimum delay=%s", client.calls, test.wantDelay)
			}
		})
	}
}

func TestRedisCircuitBreakerPropagatesRedisFailure(t *testing.T) {
	client := &fakeRedisCircuitBreakerClient{errors: []error{errors.New("redis unavailable")}}
	breaker, err := newRedisCircuitBreaker(client, RedisCircuitBreakerConfig{
		Key: "app:circuit:caiyun", FailureThreshold: 3, OpenTimeout: time.Minute, ProbeTTL: time.Second,
	})
	if err != nil {
		t.Fatalf("newRedisCircuitBreaker() error=%v", err)
	}
	if _, _, err := breaker.Allow(context.Background()); err == nil {
		t.Fatal("Allow() error=nil")
	}
}

type fakeRedisCircuitBreakerCall struct {
	key  string
	args []interface{}
}

type fakeRedisCircuitBreakerClient struct {
	results []interface{}
	errors  []error
	calls   []fakeRedisCircuitBreakerCall
}

func (client *fakeRedisCircuitBreakerClient) Eval(_ context.Context, _ string, keys []string, args ...interface{}) *redisv8.Cmd {
	index := len(client.calls)
	call := fakeRedisCircuitBreakerCall{args: append([]interface{}(nil), args...)}
	if len(keys) > 0 {
		call.key = keys[0]
	}
	client.calls = append(client.calls, call)

	command := redisv8.NewCmd(context.Background())
	if index < len(client.results) {
		command.SetVal(client.results[index])
	}
	if index < len(client.errors) {
		command.SetErr(client.errors[index])
	}
	return command
}
