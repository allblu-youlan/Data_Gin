package data_svc

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/providerhttp"
)

func TestMallWeatherRetryDelayMatchesProviderFailure(t *testing.T) {
	for _, test := range []struct {
		name          string
		providerError *caiyun.ProviderError
		circuitOpen   bool
		cooldown      time.Duration
		wantDelay     time.Duration
		wantRetryable bool
		wantClass     string
	}{
		{
			name: "rate limited waits past the circuit cooldown",
			providerError: &caiyun.ProviderError{
				Class: providerhttp.ErrorClassRateLimited, HTTPStatus: http.StatusTooManyRequests, Retryable: true,
			},
			wantDelay: time.Minute + time.Second, wantRetryable: true, wantClass: "rate_limited",
		},
		{
			name: "open circuit waits past the circuit cooldown", circuitOpen: true,
			wantDelay: time.Minute + time.Second, wantRetryable: true, wantClass: "circuit_open",
		},
		{
			name: "rate limited uses the configured circuit cooldown",
			providerError: &caiyun.ProviderError{
				Class: providerhttp.ErrorClassRateLimited, HTTPStatus: http.StatusTooManyRequests, Retryable: true,
			},
			cooldown: 90 * time.Second, wantDelay: 91 * time.Second, wantRetryable: true, wantClass: "rate_limited",
		},
		{
			name: "open circuit uses the configured circuit cooldown", circuitOpen: true,
			cooldown: 90 * time.Second, wantDelay: 91 * time.Second, wantRetryable: true, wantClass: "circuit_open",
		},
		{
			name: "transport keeps the queue retry schedule",
			providerError: &caiyun.ProviderError{
				Class: providerhttp.ErrorClassTransport, Retryable: true,
			},
			wantRetryable: true, wantClass: "transport",
		},
		{
			name: "service unavailable keeps the queue retry schedule",
			providerError: &caiyun.ProviderError{
				Class: providerhttp.ErrorClassProvider, HTTPStatus: http.StatusServiceUnavailable, Retryable: true,
			},
			wantRetryable: true, wantClass: "provider_unavailable",
		},
		{
			name: "authentication failure stays permanent",
			providerError: &caiyun.ProviderError{
				Class: providerhttp.ErrorClassAuth, HTTPStatus: http.StatusUnauthorized,
			},
			wantClass: "authentication",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeMallWeatherProvider{}
			if test.providerError != nil {
				provider.weatherErr = test.providerError
				if test.providerError.HTTPStatus != 0 {
					provider.weatherResponse = &caiyun.ProviderResponse{
						EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: test.providerError.HTTPStatus,
					}
				}
			}
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			breaker := &fakeWeatherCircuitBreaker{allowed: !test.circuitOpen}
			processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
				t, provider, store, &fakeWeatherTaskLocker{acquired: true},
				&fakeWeatherRateLimiter{}, noopMallWeatherMetricRecorder{}, breaker,
			)
			if test.cooldown > 0 {
				processor.config.CircuitOpenTimeout = test.cooldown
			}
			err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{
				MallID: 7, TaskWindow: "fast:7:202611052040",
			})
			var processError *MallWeatherProcessError
			if !errors.As(err, &processError) || processError.Retryable != test.wantRetryable {
				t.Fatalf("Process() error=%v, want retryable=%t", err, test.wantRetryable)
			}
			if store.failure.ErrorClass != test.wantClass {
				t.Fatalf("persisted failure=%+v, want class=%s", store.failure, test.wantClass)
			}
			if test.circuitOpen && provider.weatherCalls != 0 {
				t.Fatalf("open circuit made %d provider calls", provider.weatherCalls)
			}
			var hint interface{ RetryDelay() time.Duration }
			var delay time.Duration
			if errors.As(err, &hint) {
				delay = hint.RetryDelay()
			}
			if delay != test.wantDelay {
				t.Fatalf("Process() retry delay=%s, want %s", delay, test.wantDelay)
			}
		})
	}
}

func TestMallWeatherRetryRejectsNonPositiveCircuitCooldown(t *testing.T) {
	processor := newTestMallWeatherProcessor(t, &fakeMallWeatherProvider{},
		newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired))
	for _, cooldown := range []time.Duration{0, -time.Second} {
		t.Run(cooldown.String(), func(t *testing.T) {
			config := processor.config
			config.CircuitOpenTimeout = cooldown
			created, err := newMallWeatherProcessor(
				processor.provider, processor.store, processor.weatherSnapshots, processor.lifeSnapshots,
				processor.locker, processor.limiter, processor.breaker, config, processor.now, processor.metrics,
			)
			if err == nil || created != nil {
				t.Fatalf("constructor accepted circuit cooldown=%s: processor=%v error=%v", cooldown, created, err)
			}
		})
	}
}

func TestMallWeatherRetryErrorNilReceiver(t *testing.T) {
	var processError *MallWeatherProcessError
	if processError.Error() == "" || processError.RetryDelay() != 0 || processError.Unwrap() != nil {
		t.Fatal("nil error receiver returned unexpected message, delay, or cause")
	}
}

func TestMallWeatherRetryErrorReportsOnlySafeDetails(t *testing.T) {
	providerError := &caiyun.ProviderError{
		Class: providerhttp.ErrorClassRateLimited, HTTPStatus: http.StatusTooManyRequests, Retryable: true,
	}
	rawCause := errors.New("request https://example.invalid?token=private-test-secret failed")
	for _, test := range []struct {
		name  string
		code  string
		cause error
		want  []string
	}{
		{name: "safe provider error", code: "PROVIDER_REQUEST_FAILED", cause: providerError, want: []string{"class=rate_limited", "http_status=429"}},
		{name: "safe provider behind raw wrapper", code: "PROVIDER_REQUEST_FAILED", cause: fmt.Errorf("private-test-secret: %w", providerError), want: []string{"class=rate_limited", "http_status=429"}},
		{name: "raw cause stays private", code: "START_FAILED", cause: rawCause},
		{name: "code without cause", code: "CIRCUIT_OPEN"},
	} {
		t.Run(test.name, func(t *testing.T) {
			processError := &MallWeatherProcessError{Code: test.code, cause: test.cause}
			message := processError.Error()
			for _, want := range append([]string{test.code}, test.want...) {
				if !strings.Contains(message, want) {
					t.Errorf("Error()=%q, missing %q", message, want)
				}
			}
			if strings.Contains(message, "private-test-secret") || strings.Contains(message, "example.invalid") {
				t.Errorf("Error() exposed raw cause: %q", message)
			}
			if test.cause != nil && !errors.Is(processError, test.cause) {
				t.Error("errors.Is no longer reaches the original cause")
			}
			var originalProvider, wrappedProvider *caiyun.ProviderError
			if errors.As(test.cause, &originalProvider) &&
				(!errors.As(processError, &wrappedProvider) || wrappedProvider != originalProvider) {
				t.Error("errors.As no longer reaches the original provider error")
			}
		})
	}
}
