package providerhttp

import (
	"context"
	"fmt"
	"strconv"
	"time"

	redisv8 "github.com/go-redis/redis/v8"
)

const redisCircuitAllowScript = `
local open_timeout_ms = tonumber(ARGV[1])
local probe_ttl_ms = tonumber(ARGV[2])
local ttl_ms = tonumber(ARGV[3])
local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local state = redis.call("HMGET", KEYS[1], "state", "opened_ms", "probe_until_ms", "open_until_ms")
local status = state[1]
local opened_ms = tonumber(state[2]) or 0
local probe_until_ms = tonumber(state[3]) or 0
local open_until_ms = tonumber(state[4]) or 0
if open_until_ms <= 0 then
  open_until_ms = opened_ms + open_timeout_ms
end

if status == "open" then
  if now_ms < open_until_ms then
    return {0, "open", open_until_ms - now_ms}
  end
  if now_ms < probe_until_ms then
    return {0, "open", probe_until_ms - now_ms}
  end
  redis.call("HMSET", KEYS[1], "state", "half_open", "probe_until_ms", now_ms + probe_ttl_ms)
  redis.call("PEXPIRE", KEYS[1], ttl_ms)
  return {1, "half_open", 0}
end

if status == "half_open" then
  if now_ms < probe_until_ms then
    return {0, "open", probe_until_ms - now_ms}
  end
  redis.call("HMSET", KEYS[1], "probe_until_ms", now_ms + probe_ttl_ms)
  redis.call("PEXPIRE", KEYS[1], ttl_ms)
  return {1, "half_open", 0}
end

redis.call("PEXPIRE", KEYS[1], ttl_ms)
return {1, "closed", 0}
`

const redisCircuitReportScript = `
local failure_threshold = tonumber(ARGV[1])
local ttl_ms = tonumber(ARGV[2])
local success = tonumber(ARGV[3])
local open_timeout_ms = tonumber(ARGV[4])
local minimum_delay_ms = tonumber(ARGV[5])
local redis_time = redis.call("TIME")
local now_ms = redis_time[1] * 1000 + math.floor(redis_time[2] / 1000)
local state = redis.call("HMGET", KEYS[1], "state", "failures", "opened_ms", "open_until_ms")
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
if minimum_delay_ms > 0 then
  failures = math.max(failures, failure_threshold)
end

if failures >= failure_threshold or status == "open" then
  local open_until_ms = now_ms + math.max(open_timeout_ms, minimum_delay_ms)
  if status == "open" then
    local previous_open_until_ms = tonumber(state[4]) or 0
    if previous_open_until_ms <= 0 then
      previous_open_until_ms = (tonumber(state[3]) or 0) + open_timeout_ms
    end
    open_until_ms = math.max(open_until_ms, previous_open_until_ms)
  end
  local retention_ms = ttl_ms - open_timeout_ms
  local retained_ttl_ms = math.max(ttl_ms, open_until_ms - now_ms + retention_ms, redis.call("PTTL", KEYS[1]))
  redis.call("HMSET", KEYS[1], "state", "open", "failures", failures, "opened_ms", now_ms, "open_until_ms", open_until_ms, "probe_until_ms", 0)
  redis.call("PEXPIRE", KEYS[1], retained_ttl_ms)
  return {"open", failures}
end

redis.call("HMSET", KEYS[1], "state", "closed", "failures", failures, "opened_ms", 0, "open_until_ms", 0, "probe_until_ms", 0)
redis.call("PEXPIRE", KEYS[1], ttl_ms)
return {"closed", failures}
`

type ProviderCircuitBreaker interface {
	Allow(ctx context.Context) (bool, time.Duration, error)
	ReportSuccess(ctx context.Context) error
	ReportFailure(ctx context.Context) error
	ReportRateLimited(ctx context.Context, retryAfter time.Duration) error
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
		return nil, fmt.Errorf("provider circuit breaker: invalid configuration")
	}
	if cfg.StateTTL == 0 {
		cfg.StateTTL = 2*cfg.OpenTimeout + cfg.ProbeTTL
	}
	if cfg.StateTTL < cfg.OpenTimeout+cfg.ProbeTTL {
		return nil, fmt.Errorf("provider circuit breaker: state ttl is too short")
	}
	return &RedisCircuitBreaker{
		client: client, key: cfg.Key, failureThreshold: cfg.FailureThreshold,
		openTimeoutMS: cfg.OpenTimeout.Milliseconds(),
		probeTTLMS:    cfg.ProbeTTL.Milliseconds(),
		stateTTLMS:    cfg.StateTTL.Milliseconds(),
	}, nil
}

func (breaker *RedisCircuitBreaker) Allow(ctx context.Context) (bool, time.Duration, error) {
	if breaker == nil || breaker.client == nil || ctx == nil {
		return false, 0, fmt.Errorf("provider circuit breaker: invalid allow")
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
		return false, 0, fmt.Errorf("provider circuit breaker: allow: %w", err)
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return false, 0, fmt.Errorf("provider circuit breaker: invalid allow response")
	}
	allowed, ok := circuitRedisInteger(values[0])
	if !ok || (allowed != 0 && allowed != 1) {
		return false, 0, fmt.Errorf("provider circuit breaker: invalid allow result")
	}
	waitMS, ok := circuitRedisInteger(values[2])
	if !ok || waitMS < 0 {
		return false, 0, fmt.Errorf("provider circuit breaker: invalid retry delay")
	}
	return allowed == 1, time.Duration(waitMS) * time.Millisecond, nil
}

func (breaker *RedisCircuitBreaker) ReportSuccess(ctx context.Context) error {
	return breaker.report(ctx, true, 0)
}

func (breaker *RedisCircuitBreaker) ReportFailure(ctx context.Context) error {
	return breaker.report(ctx, false, 0)
}

func (breaker *RedisCircuitBreaker) ReportRateLimited(ctx context.Context, retryAfter time.Duration) error {
	if breaker == nil {
		return fmt.Errorf("provider circuit breaker: invalid report")
	}
	minimumDelay := max(retryAfter, time.Duration(breaker.openTimeoutMS)*time.Millisecond)
	return breaker.report(ctx, false, minimumDelay)
}

func (breaker *RedisCircuitBreaker) report(ctx context.Context, success bool, minimumDelay time.Duration) error {
	if breaker == nil || breaker.client == nil || ctx == nil {
		return fmt.Errorf("provider circuit breaker: invalid report")
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
		breaker.openTimeoutMS,
		minimumDelay.Milliseconds(),
	).Result()
	if err != nil {
		return fmt.Errorf("provider circuit breaker: report: %w", err)
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return fmt.Errorf("provider circuit breaker: invalid report response")
	}
	return nil
}

func circuitRedisInteger(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	case []byte:
		parsed, err := strconv.ParseInt(string(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
