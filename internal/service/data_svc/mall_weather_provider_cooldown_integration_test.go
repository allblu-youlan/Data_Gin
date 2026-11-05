//go:build integration

package data_svc

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/providerhttp"

	redisv8 "github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

func TestMallWeatherProviderCooldownSharesLongRetryAfterAcrossProcessors(t *testing.T) {
	addr := os.Getenv("MALL_WEATHER_REDIS_ADDR")
	if addr == "" {
		t.Skip("MALL_WEATHER_REDIS_ADDR is required for Redis integration tests")
	}
	client := redisv8.NewClient(&redisv8.Options{
		Addr: addr, DB: 15, DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, MaxRetries: 1,
	})
	key := "integration:mall-weather:provider-cooldown:" + uuid.NewString()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := client.Del(ctx, key).Err(); err != nil {
			t.Errorf("clean circuit key: %v", err)
		}
		if err := client.Close(); err != nil {
			t.Errorf("close Redis client: %v", err)
		}
	})
	config := weatherdomain.RedisCircuitBreakerConfig{
		Key: key, FailureThreshold: 2, OpenTimeout: time.Minute, ProbeTTL: 10 * time.Second,
	}
	firstBreaker, err := weatherdomain.NewRedisCircuitBreaker(client, config)
	if err != nil {
		t.Fatal(err)
	}
	secondBreaker, err := weatherdomain.NewRedisCircuitBreaker(client, config)
	if err != nil {
		t.Fatal(err)
	}
	firstProvider := &fakeMallWeatherProvider{
		weatherResponse: &caiyun.ProviderResponse{
			EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: http.StatusTooManyRequests,
			RawBody: []byte("Rate limit exceeded"),
		},
		weatherErr: &caiyun.ProviderError{
			Class: providerhttp.ErrorClassRateLimited, HTTPStatus: http.StatusTooManyRequests,
			Retryable: true, RetryAfter: 3 * time.Minute,
		},
	}
	first := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, firstProvider,
		newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired), &fakeWeatherTaskLocker{acquired: true},
		&fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, firstBreaker)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	firstResult := first.Process(ctx, job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611052340"})
	var firstError *MallWeatherProcessError
	if !errors.As(firstResult, &firstError) || firstError.RetryDelay() != 3*time.Minute+time.Second {
		t.Fatalf("first Process() error=%v, want 181s retry delay", firstResult)
	}
	secondProvider := &fakeMallWeatherProvider{}
	secondStore := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	secondStore.execution.Mall.ID = 8
	secondStore.execution.Run.MallID = 8
	second := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, secondProvider,
		secondStore, &fakeWeatherTaskLocker{acquired: true},
		&fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, secondBreaker)
	secondResult := second.Process(ctx, job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 8, TaskWindow: "fast:8:202611052340"})
	var secondError *MallWeatherProcessError
	if !errors.As(secondResult, &secondError) || secondError.Code != "CIRCUIT_OPEN" || !secondError.Retryable {
		t.Fatalf("second Process() error=%v, want retryable open circuit", secondResult)
	}
	if delay := secondError.RetryDelay(); delay <= time.Minute+time.Second || delay > 3*time.Minute+time.Second {
		t.Fatalf("second Process() retry delay=%s did not inherit the shared cooldown", delay)
	}
	if secondProvider.weatherCalls != 0 {
		t.Fatalf("second processor made %d provider requests during shared cooldown", secondProvider.weatherCalls)
	}
}
