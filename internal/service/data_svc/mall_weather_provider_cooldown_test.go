package data_svc

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/providerhttp"
)

type providerCooldownBreaker struct {
	fakeWeatherCircuitBreaker
	rateLimitedCalls int
	retryAfter       time.Duration
}

func (breaker *providerCooldownBreaker) ReportRateLimited(_ context.Context, delay time.Duration) error {
	breaker.rateLimitedCalls++
	breaker.retryAfter = delay
	return nil
}

func TestMallWeatherProviderCooldownHonorsRetryAfter(t *testing.T) {
	for _, test := range []struct {
		name       string
		class      providerhttp.ErrorClass
		httpStatus int
		retryAfter time.Duration
		wantDelay  time.Duration
	}{
		{"rate limit longer than circuit", providerhttp.ErrorClassRateLimited, http.StatusTooManyRequests, 2 * time.Minute, 2*time.Minute + time.Second},
		{"rate limit shorter than circuit", providerhttp.ErrorClassRateLimited, http.StatusTooManyRequests, 5 * time.Second, time.Minute + time.Second},
		{"provider unavailable", providerhttp.ErrorClassProvider, http.StatusServiceUnavailable, 2 * time.Minute, 2*time.Minute + time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeMallWeatherProvider{weatherErr: &caiyun.ProviderError{
				Class: test.class, HTTPStatus: test.httpStatus, Retryable: true, RetryAfter: test.retryAfter,
			}}
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			breaker := &providerCooldownBreaker{fakeWeatherCircuitBreaker: fakeWeatherCircuitBreaker{allowed: true}}
			processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, provider, store,
				&fakeWeatherTaskLocker{acquired: true}, &fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, breaker)
			err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611052340"})
			var hint interface{ RetryDelay() time.Duration }
			if !errors.As(err, &hint) || hint.RetryDelay() != test.wantDelay {
				t.Fatalf("Process() error=%v retry hint=%v, want %s", err, hint, test.wantDelay)
			}
			if test.class == providerhttp.ErrorClassRateLimited {
				if breaker.rateLimitedCalls != 1 || breaker.retryAfter != test.retryAfter || breaker.failureCalls != 0 {
					t.Fatalf("rate limit did not pause the shared circuit: %+v", breaker)
				}
			} else if breaker.rateLimitedCalls != 0 || breaker.failureCalls != 1 {
				t.Fatalf("ordinary provider failure used rate-limit feedback: %+v", breaker)
			}
		})
	}
}

func TestMallWeatherProviderCooldownLateSuccessPreservesOpenMetric(t *testing.T) {
	metrics := newInMemoryMallWeatherMetricRecorder()
	processor := newTestMallWeatherProcessorWithMetrics(t, &fakeMallWeatherProvider{},
		newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired), metrics)
	recordMallWeatherProviderCircuitOpen(metrics, true)
	processor.reportProviderCircuitSuccess(t.Context())
	if !mallWeatherMetricGaugeExists(metrics.GaugeSnapshot(), MallWeatherMetricProviderCircuitOpen, nil, 1) {
		t.Fatal("a success response cleared the circuit gauge without a new admission decision")
	}
}

func TestMallWeatherProviderCooldownUsesSharedRemainingDelay(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	breaker := &fakeWeatherCircuitBreaker{allowRetryAfter: 3 * time.Minute}
	processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, provider,
		newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired), &fakeWeatherTaskLocker{acquired: true},
		&fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, breaker)
	err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{
		MallID: 7, TaskWindow: "fast:7:202611052340",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || processError.Code != "CIRCUIT_OPEN" ||
		processError.RetryDelay() != 3*time.Minute+time.Second {
		t.Fatalf("Process() error=%v, want open circuit with 181s retry delay", err)
	}
	if provider.weatherCalls != 0 {
		t.Fatalf("shared cooldown made %d provider requests", provider.weatherCalls)
	}
}

func TestMallWeatherProviderCooldownSurvivesSnapshotFailure(t *testing.T) {
	for _, test := range []struct {
		name         string
		buildFailure bool
		wantCode     string
		retryAfter   time.Duration
		wantDelay    time.Duration
	}{
		{name: "build failure with long cooldown", buildFailure: true, wantCode: "SNAPSHOT_FAILED", retryAfter: 2 * time.Minute, wantDelay: 121 * time.Second},
		{name: "database failure with long cooldown", wantCode: "SNAPSHOT_RECORD_FAILED", retryAfter: 2 * time.Minute, wantDelay: 121 * time.Second},
		{name: "build failure with short cooldown", buildFailure: true, wantCode: "SNAPSHOT_FAILED", retryAfter: 5 * time.Second, wantDelay: 61 * time.Second},
		{name: "database failure with short cooldown", wantCode: "SNAPSHOT_RECORD_FAILED", retryAfter: 5 * time.Second, wantDelay: 61 * time.Second},
		{name: "build failure without cooldown", buildFailure: true, wantCode: "SNAPSHOT_FAILED", wantDelay: 61 * time.Second},
		{name: "database failure without cooldown", wantCode: "SNAPSHOT_RECORD_FAILED", wantDelay: 61 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeMallWeatherProvider{
				weatherResponse: &caiyun.ProviderResponse{
					EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: http.StatusTooManyRequests,
					RawBody: []byte("Rate limit exceeded"),
				},
				weatherErr: &caiyun.ProviderError{
					Class: providerhttp.ErrorClassRateLimited, HTTPStatus: http.StatusTooManyRequests,
					Retryable: true, RetryAfter: test.retryAfter,
				},
			}
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			store.recordErr = errors.New("snapshot database unavailable")
			breaker := &providerCooldownBreaker{fakeWeatherCircuitBreaker: fakeWeatherCircuitBreaker{allowed: true}}
			processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, provider, store,
				&fakeWeatherTaskLocker{acquired: true}, &fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, breaker)
			if test.buildFailure {
				builder, err := weatherdomain.NewRawSnapshotBuilder(weatherdomain.RawSnapshotConfig{
					SchemaVersion: weatherParserVersionV26, InlineCompressedBytes: 1,
				}, nil)
				if err != nil {
					t.Fatal(err)
				}
				processor.weatherSnapshots = builder
			}
			err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{
				MallID: 7, TaskWindow: "fast:7:202611052340",
			})
			var processError *MallWeatherProcessError
			if !errors.As(err, &processError) || !processError.Retryable || processError.Code != test.wantCode ||
				store.failure.ErrorCode != test.wantCode {
				t.Fatalf("Process() error=%v persisted failure=%+v, want %s", err, store.failure, test.wantCode)
			}
			if breaker.rateLimitedCalls != 1 || breaker.retryAfter != test.retryAfter || breaker.failureCalls != 0 {
				t.Fatalf("snapshot failure lost or duplicated provider cooldown: %+v", breaker)
			}
			if processError.RetryDelay() != test.wantDelay {
				t.Fatalf("snapshot failure retry delay=%v, want %v", processError.RetryDelay(), test.wantDelay)
			}
		})
	}
}
