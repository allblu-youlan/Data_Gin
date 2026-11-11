package kaiyu

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

func TestKaiyuClientsShareCredentialRequestBudget(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var calls []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls = append(calls, time.Now())
		mu.Unlock()
		fmt.Fprint(w, `{"status":1,"data":[]}`)
	}))
	defer server.Close()
	for range 3 {
		client, err := NewClient(Config{BaseURL: server.URL, SecurityID: t.Name(), SecurityCode: "secret", HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ListShops(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(calls) != 3 || calls[2].Sub(calls[0]) < 350*time.Millisecond {
		t.Fatalf("separate clients exceeded shared budget: %v", calls)
	}
}

func TestKaiyuRequestBudgetCancelsWithoutRequest(t *testing.T) {
	t.Parallel()
	limiter := &localRequestLimiter{next: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := limiter.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	ctx, cancel = context.WithTimeout(t.Context(), 5*time.Millisecond)
	defer cancel()
	if err := limiter.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}

type fakeBudgetClient struct {
	values []int64
	err    error
	keys   []string
	calls  int
}

func (f *fakeBudgetClient) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redisv8.Cmd {
	f.calls++
	f.keys = keys
	if !strings.Contains(script, "redis.call('TIME')") || len(args) != 1 || args[0] != int64(200) {
		return redisv8.NewCmdResult(nil, errors.New("invalid budget script"))
	}
	if f.err != nil {
		return redisv8.NewCmdResult(nil, f.err)
	}
	value := f.values[0]
	f.values = f.values[1:]
	return redisv8.NewCmdResult(value, nil)
}

func TestKaiyuRedisBudgetWaitsAndFailsClosed(t *testing.T) {
	t.Parallel()
	client := &fakeBudgetClient{values: []int64{1, 0}}
	limiter := &redisRequestLimiter{client: client, key: "kaiyu:test-budget"}
	if err := limiter.Wait(t.Context()); err != nil || client.calls != 2 {
		t.Fatalf("calls=%d err=%v", client.calls, err)
	}
	want := errors.New("redis unavailable")
	client.err = want
	if err := limiter.Wait(t.Context()); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	rds := redisv8.NewClient(&redisv8.Options{Addr: "127.0.0.1:1"})
	defer rds.Close()
	shared, err := NewRedisRequestLimiter(rds, "private-id")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(shared.(*redisRequestLimiter).key, "private-id") {
		t.Fatal("credential exposed in Redis key")
	}
}
