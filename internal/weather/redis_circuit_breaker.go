package weather

import (
	"context"
	"fmt"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

const redisCircuitAllowScript = `
local open_timeout_ms = tonumber(ARGV[1])
local probe_ttl_ms = tonumber(ARGV[2])
local ttl_ms = tonumber(ARGV[3])
local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local state = redis.call("HMGET", KEYS[1], "state", "opened_ms", "probe_until_ms")
local status = state[1]
local opened_ms = tonumber(state[2]) or 0
local probe_until_ms = tonumber(state[3]) or 0

if status == "open" then
  if now_ms - opened_ms < open_timeout_ms then
    return {0, "open"}
  end
  if now_ms < probe_until_ms then
    return {0, "open"}
  end
  redis.call("HMSET", KEYS[1], "state", "half_open", "probe_until_ms", now_ms + probe_ttl_ms)
  redis.call("PEXPIRE", KEYS[1], ttl_ms)
  return {1, "half_open"}
end

if status == "half_open" then
  if now_ms < probe_until_ms then
    return {0, "open"}
  end
  redis.call("HMSET", KEYS[1], "probe_until_ms", now_ms + probe_ttl_ms)
  redis.call("PEXPIRE", KEYS[1], ttl_ms)
  return {1, "half_open"}
end

redis.call("PEXPIRE", KEYS[1], ttl_ms)
return {1, "closed"}
`

const redisCircuitReportScript = `
local failure_threshold = tonumber(ARGV[1])
local ttl_ms = tonumber(ARGV[2])
local success = tonumber(ARGV[3])
local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local state = redis.call("HMGET", KEYS[1], "state", "failures")
local status = state[1]
local failures = tonumber(state[2]) or 0

if success == 1 then
  if status == "open" then
    return {"open", failures}
  end
  redis.call("DEL", KEYS[1])
  return {"closed", 0}
end

if status == "half_open" then
  failures = failure_threshold
else
  failures = failures + 1
end

if failures >= failure_threshold then
  redis.call("HMSET", KEYS[1], "state", "open", "failures", failures, "opened_ms", now_ms, "probe_until_ms", 0)
  redis.call("PEXPIRE", KEYS[1], ttl_ms)
  return {"open", failures}
end

redis.call("HMSET", KEYS[1], "state", "closed", "failures", failures, "opened_ms", 0, "probe_until_ms", 0)
redis.call("PEXPIRE", KEYS[1], ttl_ms)
return {"closed", failures}
`

type ProviderCircuitBreaker interface {
	Allow(ctx context.Context) (bool, error)
	ReportSuccess(ctx context.Context) error
	ReportFailure(ctx context.Context) error
}

type redisCircuitBreakerClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redisv8.Cmd
}

type RedisCircuitBreakerConfig struct {
	Key              string
	FailureThreshold int
	OpenTimeout      time.Duration
	ProbeTTL         time.Duration
	StateTTL         time.Duration
}

type RedisCircuitBreaker struct {
	client           redisCircuitBreakerClient
	key              string
	failureThreshold int
	openTimeoutMS    int64
	probeTTLMS       int64
	stateTTLMS       int64
}

func NewRedisCircuitBreaker(client *redisv8.Client, cfg RedisCircuitBreakerConfig) (*RedisCircuitBreaker, error) {
	return newRedisCircuitBreaker(client, cfg)
}

func newRedisCircuitBreaker(client redisCircuitBreakerClient, cfg RedisCircuitBreakerConfig) (*RedisCircuitBreaker, error) {
	if client == nil || cfg.Key == "" || len(cfg.Key) > 512 || cfg.FailureThreshold < 1 ||
		cfg.OpenTimeout <= 0 || cfg.ProbeTTL <= 0 {
		return nil, fmt.Errorf("weather circuit breaker: invalid configuration")
	}
	if cfg.StateTTL == 0 {
		cfg.StateTTL = 2*cfg.OpenTimeout + cfg.ProbeTTL
	}
	if cfg.StateTTL < cfg.OpenTimeout+cfg.ProbeTTL {
		return nil, fmt.Errorf("weather circuit breaker: state ttl is too short")
	}
	return &RedisCircuitBreaker{
		client: client, key: cfg.Key, failureThreshold: cfg.FailureThreshold,
		openTimeoutMS: cfg.OpenTimeout.Milliseconds(),
		probeTTLMS:    cfg.ProbeTTL.Milliseconds(),
		stateTTLMS:    cfg.StateTTL.Milliseconds(),
	}, nil
}

func (breaker *RedisCircuitBreaker) Allow(ctx context.Context) (bool, error) {
	if breaker == nil || breaker.client == nil || ctx == nil {
		return false, fmt.Errorf("weather circuit breaker: invalid allow")
	}
	result, err := breaker.client.Eval(
		ctx,
		redisCircuitAllowScript,
		[]string{breaker.key},
		breaker.openTimeoutMS,
		breaker.probeTTLMS,
		breaker.stateTTLMS,
	).Result()
	if err != nil {
		return false, fmt.Errorf("weather circuit breaker: allow: %w", err)
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return false, fmt.Errorf("weather circuit breaker: invalid allow response")
	}
	allowed, ok := redisInteger(values[0])
	if !ok || (allowed != 0 && allowed != 1) {
		return false, fmt.Errorf("weather circuit breaker: invalid allow result")
	}
	return allowed == 1, nil
}

func (breaker *RedisCircuitBreaker) ReportSuccess(ctx context.Context) error {
	return breaker.report(ctx, true)
}

func (breaker *RedisCircuitBreaker) ReportFailure(ctx context.Context) error {
	return breaker.report(ctx, false)
}

func (breaker *RedisCircuitBreaker) report(ctx context.Context, success bool) error {
	if breaker == nil || breaker.client == nil || ctx == nil {
		return fmt.Errorf("weather circuit breaker: invalid report")
	}
	successValue := 0
	if success {
		successValue = 1
	}
	result, err := breaker.client.Eval(
		ctx,
		redisCircuitReportScript,
		[]string{breaker.key},
		breaker.failureThreshold,
		breaker.stateTTLMS,
		successValue,
	).Result()
	if err != nil {
		return fmt.Errorf("weather circuit breaker: report: %w", err)
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return fmt.Errorf("weather circuit breaker: invalid report response")
	}
	return nil
}
