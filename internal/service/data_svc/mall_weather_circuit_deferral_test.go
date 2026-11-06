package data_svc

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/providerhttp"
)

func TestMallWeatherCircuitDeferralSurvivesRepeatedSharedCooldown(t *testing.T) {
	createdAt := time.Date(2026, 11, 6, 5, 30, 0, 0, time.UTC)
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	store.execution.Run.CreatedAt = createdAt
	provider := &fakeMallWeatherProvider{}
	breaker := &fakeWeatherCircuitBreaker{allowRetryAfter: time.Minute}
	processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, provider, store,
		&fakeWeatherTaskLocker{acquired: true}, &fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, breaker)
	payload := job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611061330"}
	for _, elapsed := range []time.Duration{0, 62 * time.Second, 128 * time.Second, 3 * time.Minute} {
		processor.now = func() time.Time { return createdAt.Add(elapsed) }
		err := processor.Process(t.Context(), job.TypeMallWeatherFast, payload)
		var processError *MallWeatherProcessError
		if !errors.As(err, &processError) || !processError.Retryable || processError.Code != "CIRCUIT_OPEN" ||
			!weatherTaskDeferred(err) || processError.RetryDelay() != 61*time.Second {
			t.Fatalf("elapsed=%s: error=%v must defer without consuming retry budget", elapsed, err)
		}
		if store.failure.AttemptStatus != "provider_skipped" || store.failure.ErrorCode != "CIRCUIT_OPEN" {
			t.Fatalf("cooldown audit=%+v", store.failure)
		}
	}
	if provider.weatherCalls != 0 || breaker.failureCalls != 0 {
		t.Fatalf("shared cooldown made %d requests and %d failure reports", provider.weatherCalls, breaker.failureCalls)
	}
	provider.weatherResponse = &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: http.StatusOK, ProviderStatus: "ok",
		RawBody: readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json"),
	}
	breaker.allowed = true
	processor.now = func() time.Time { return createdAt.Add(4 * time.Minute) }
	if err := processor.Process(t.Context(), job.TypeMallWeatherFast, payload); err != nil {
		t.Fatalf("recovered circuit Process() error=%v", err)
	}
	if provider.weatherCalls != 1 || store.batch == nil {
		t.Fatal("recovered circuit did not fetch and persist weather")
	}
}

func TestMallWeatherCircuitDeferralEndsAtFixedRunDeadline(t *testing.T) {
	createdAt := time.Date(2026, 11, 6, 5, 30, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		createdAt time.Time
		elapsed   time.Duration
		wantCode  string
		wantDelay time.Duration
		wantDefer bool
	}{
		{name: "cooldown capped by remaining lifetime", createdAt: createdAt, elapsed: 9*time.Minute + 30*time.Second,
			wantCode: "CIRCUIT_OPEN", wantDelay: 30 * time.Second, wantDefer: true},
		{name: "deadline reached", createdAt: createdAt, elapsed: 10 * time.Minute, wantCode: "CIRCUIT_WAIT_EXPIRED"},
		{name: "deadline passed", createdAt: createdAt, elapsed: 12 * time.Minute, wantCode: "CIRCUIT_WAIT_EXPIRED"},
		{name: "missing creation time cannot renew wait", wantCode: "CIRCUIT_WAIT_EXPIRED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			store.execution.Run.CreatedAt = test.createdAt
			provider := &fakeMallWeatherProvider{}
			processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, provider, store,
				&fakeWeatherTaskLocker{acquired: true}, &fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{},
				&fakeWeatherCircuitBreaker{allowRetryAfter: 3 * time.Minute})
			processor.now = func() time.Time { return createdAt.Add(test.elapsed) }
			err := processor.Process(t.Context(), job.TypeMallWeatherFast,
				job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611061330"})
			var processError *MallWeatherProcessError
			if !errors.As(err, &processError) || processError.Code != test.wantCode ||
				processError.Retryable != test.wantDefer || weatherTaskDeferred(err) != test.wantDefer ||
				processError.RetryDelay() != test.wantDelay {
				t.Fatalf("Process() error=%+v, want code=%s deferred=%t delay=%s", err, test.wantCode, test.wantDefer, test.wantDelay)
			}
			if store.failure.ErrorCode != test.wantCode || store.failure.AttemptStatus != "provider_skipped" || provider.weatherCalls != 0 {
				t.Fatalf("failure=%+v provider calls=%d", store.failure, provider.weatherCalls)
			}
		})
	}
}

func TestMallWeatherCircuitDeferralDoesNotHideActualFailures(t *testing.T) {
	for _, test := range []struct {
		name        string
		allowed     bool
		providerErr error
		storeErr    error
		wantCode    string
	}{
		{name: "provider rate limit", allowed: true, providerErr: &caiyun.ProviderError{
			Class: providerhttp.ErrorClassRateLimited, HTTPStatus: http.StatusTooManyRequests, Retryable: true,
		}, wantCode: "PROVIDER_REQUEST_FAILED"},
		{name: "audit storage failure", storeErr: errors.New("storage unavailable"), wantCode: "FAILURE_AUDIT_FAILED"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			store.failErr = test.storeErr
			provider := &fakeMallWeatherProvider{weatherErr: test.providerErr}
			processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(t, provider, store,
				&fakeWeatherTaskLocker{acquired: true}, &fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{},
				&fakeWeatherCircuitBreaker{allowed: test.allowed})
			err := processor.Process(t.Context(), job.TypeMallWeatherFast,
				job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611061330"})
			var processError *MallWeatherProcessError
			if !errors.As(err, &processError) || processError.Code != test.wantCode || weatherTaskDeferred(err) {
				t.Fatalf("actual failure was hidden as deferral: %v", err)
			}
		})
	}
}

func weatherTaskDeferred(err error) bool {
	var deferred interface{ TaskDeferred() bool }
	return errors.As(err, &deferred) && deferred.TaskDeferred()
}
