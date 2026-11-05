package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/providerhttp"
)

func TestMallWeatherProcessorRoutesLegacyLifeIndexTaskThroughComprehensiveWeather(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	processor := newTestMallWeatherProcessor(t, provider, store)
	payload := job.MallTaskPayload{MallID: 7, TaskWindow: "life:7:2026072203"}

	if err := processor.Process(context.Background(), job.TypeMallWeatherLifeIndex, payload); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	if !reflect.DeepEqual(store.events, []string{"start", "response", "persist"}) {
		t.Fatalf("events=%v", store.events)
	}
	if store.snapshot == nil || store.snapshot.ResponseChecksum == "" || store.batch == nil ||
		store.batch.Status != weatherFetchStatusPartialSuccess || store.batch.EndpointKind != caiyun.EndpointWeatherV26 {
		t.Fatalf("snapshot=%+v batch=%+v", store.snapshot, store.batch)
	}
	if provider.weatherRequest.HourlySteps != 360 || provider.weatherRequest.DailySteps != 15 ||
		provider.weatherCalls != 1 {
		t.Fatalf("provider=%+v", provider)
	}
}

func TestMallWeatherProcessorPersistsEveryComprehensiveWeatherModule(t *testing.T) {
	raw := fullComprehensiveWeatherResponse(t)
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	processor := newTestMallWeatherProcessor(t, provider, store)

	if err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	}); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	batch := store.batch
	request := provider.weatherRequest
	if provider.weatherCalls != 1 || request.HourlySteps != 360 || request.DailySteps != 15 ||
		!request.Alert || request.Unit != "metric:v2" || batch == nil || batch.EndpointKind != caiyun.EndpointWeatherV26 ||
		batch.Status != weatherFetchStatusSuccess || batch.Forecasts == nil || batch.Forecasts.Realtime == nil ||
		len(batch.Forecasts.Minutely) != 120 || len(batch.Forecasts.Hourly) != 360 || batch.Daily == nil ||
		len(batch.Daily.Daily) != 15 || len(batch.Daily.LifeIndices) != 15 || batch.Alerts == nil ||
		len(batch.StaleLatest.DataKinds) != 0 || len(batch.StaleLatest.LifeSourceAPIs) != 0 {
		t.Fatalf("provider calls=%d batch=%+v", provider.weatherCalls, batch)
	}
}

func TestMallWeatherProcessorRecordsSuccessfulFetchMetrics(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := newInMemoryMallWeatherMetricRecorder()
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	if err := processor.Process(context.Background(), job.TypeMallWeatherLifeIndex, job.MallTaskPayload{
		MallID: 7, TaskWindow: "life:7:2026072203",
	}); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	counters := metrics.CounterSnapshot()
	if !mallWeatherMetricCounterExists(counters, MallWeatherMetricProviderRequestsTotal, map[string]string{
		"endpoint": caiyun.EndpointWeatherV26,
		"status":   mallWeatherMetricStatusSuccess,
	}, 1) || !mallWeatherMetricCounterExists(counters, MallWeatherMetricFetchTotal, map[string]string{
		"kind":   "lifeindex",
		"status": weatherFetchStatusPartialSuccess,
	}, 1) {
		t.Fatalf("CounterSnapshot()=%+v missing success fetch counters", counters)
	}
}

func TestMallWeatherProcessorRecordsSuccessfulFetchGaugeMetrics(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := newInMemoryMallWeatherMetricRecorder()
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	if err := processor.Process(context.Background(), job.TypeMallWeatherFast, job.MallTaskPayload{
		MallID: 7, TaskWindow: "fast:7:202607220310",
	}); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	finishedAt := time.Date(2026, 7, 22, 3, 0, 1, 0, time.UTC)
	serverTime := time.Unix(1784688000, 0).UTC()
	want := []MallWeatherMetricGaugeSample{
		{
			Name:   MallWeatherMetricDataAgeSeconds,
			Labels: map[string]string{"kind": "fast"},
			Value:  finishedAt.Sub(serverTime).Seconds(),
		},
		{
			Name:   MallWeatherMetricFetchDurationSeconds,
			Labels: map[string]string{"kind": "fast"},
			Value:  1,
		},
		{
			Name:  MallWeatherMetricProviderCircuitOpen,
			Value: 0,
		},
	}
	if got := metrics.GaugeSnapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GaugeSnapshot()=%+v want %+v", got, want)
	}
}

func TestMallWeatherProcessorRecordsParseWarningMetricsAfterPersist(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	payload["api_status"] = "inactive"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := newInMemoryMallWeatherMetricRecorder()
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	if err := processor.Process(context.Background(), job.TypeMallWeatherFast, job.MallTaskPayload{
		MallID: 7, TaskWindow: "fast:7:202607220310",
	}); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	if !mallWeatherMetricCounterExists(metrics.CounterSnapshot(), MallWeatherMetricParseWarningsTotal, map[string]string{"field": "api_status"}, 1) {
		t.Fatalf("CounterSnapshot()=%+v missing api_status parse warning", metrics.CounterSnapshot())
	}
	if !mallWeatherMetricCounterExists(metrics.CounterSnapshot(), MallWeatherMetricParseWarningReasonsTotal, map[string]string{
		"field":  "api_status",
		"reason": "provider_status_not_ok",
	}, 1) {
		t.Fatalf("CounterSnapshot()=%+v missing api_status parse warning reason", metrics.CounterSnapshot())
	}
}

func TestMallWeatherProcessorRecordsFailedFetchMetrics(t *testing.T) {
	provider := &fakeMallWeatherProvider{weatherErr: &caiyun.ProviderError{
		Class: providerhttp.ErrorClassTransport, Retryable: true,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := &fakeMallWeatherMetricRecorder{}
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable {
		t.Fatalf("Process() error=%v", err)
	}
	assertFakeMallWeatherMetricCounters(t, metrics.counters, []fakeMallWeatherMetricCounter{
		{
			name: MallWeatherMetricProviderRequestsTotal,
			labels: map[string]string{
				"endpoint": caiyun.EndpointWeatherV26,
				"status":   mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
		{
			name: MallWeatherMetricFetchTotal,
			labels: map[string]string{
				"kind":   "full",
				"status": mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
	})
}

func TestMallWeatherProcessorRecordsProviderRateLimitedMetric(t *testing.T) {
	provider := &fakeMallWeatherProvider{weatherErr: &caiyun.ProviderError{
		Class: providerhttp.ErrorClassRateLimited, Retryable: true,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := &fakeMallWeatherMetricRecorder{}
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable {
		t.Fatalf("Process() error=%v", err)
	}
	assertFakeMallWeatherMetricCounters(t, metrics.counters, []fakeMallWeatherMetricCounter{
		{
			name: MallWeatherMetricProviderRequestsTotal,
			labels: map[string]string{
				"endpoint": caiyun.EndpointWeatherV26,
				"status":   mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
		{
			name:  MallWeatherMetricProviderRateLimitedTotal,
			value: 1,
		},
		{
			name: MallWeatherMetricFetchTotal,
			labels: map[string]string{
				"kind":   "full",
				"status": mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
	})
}

func TestMallWeatherProcessorRecordsProviderAuthFailureMetric(t *testing.T) {
	provider := &fakeMallWeatherProvider{weatherErr: &caiyun.ProviderError{
		Class: providerhttp.ErrorClassAuth,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := &fakeMallWeatherMetricRecorder{}
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || processError.Retryable {
		t.Fatalf("Process() error=%v", err)
	}
	assertFakeMallWeatherMetricCounters(t, metrics.counters, []fakeMallWeatherMetricCounter{
		{
			name: MallWeatherMetricProviderRequestsTotal,
			labels: map[string]string{
				"endpoint": caiyun.EndpointWeatherV26,
				"status":   mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
		{
			name: MallWeatherMetricProviderAuthFailuresTotal,
			labels: map[string]string{
				"endpoint": caiyun.EndpointWeatherV26,
			},
			value: 1,
		},
		{
			name: MallWeatherMetricFetchTotal,
			labels: map[string]string{
				"kind":   "full",
				"status": mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
	})
}

func TestMallWeatherProcessorRecordsEmptyProviderResponseAsFailedMetric(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := &fakeMallWeatherMetricRecorder{}
	processor := newTestMallWeatherProcessorWithMetrics(t, provider, store, metrics)

	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || processError.Retryable {
		t.Fatalf("Process() error=%v", err)
	}
	assertFakeMallWeatherMetricCounters(t, metrics.counters, []fakeMallWeatherMetricCounter{
		{
			name: MallWeatherMetricProviderRequestsTotal,
			labels: map[string]string{
				"endpoint": caiyun.EndpointWeatherV26,
				"status":   mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
		{
			name: MallWeatherMetricFetchTotal,
			labels: map[string]string{
				"kind":   "full",
				"status": mallWeatherMetricStatusFailed,
			},
			value: 1,
		},
	})
}

func TestMallWeatherProcessorUsesTaskLock(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	locker := &fakeWeatherTaskLocker{acquired: false}
	processor := newTestMallWeatherProcessorWithLocker(t, provider, store, locker)
	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable || processError.Code != "LOCK_BUSY" {
		t.Fatalf("Process() error=%v", err)
	}
	if locker.key != "7:full:full:7:2026072203" || len(store.events) != 0 || provider.weatherCalls != 0 {
		t.Fatalf("lock=%+v events=%v calls=%d", locker, store.events, provider.weatherCalls)
	}
}

func TestMallWeatherProcessorReleasesTaskLockAfterFailure(t *testing.T) {
	provider := &fakeMallWeatherProvider{weatherErr: &caiyun.ProviderError{
		Class: providerhttp.ErrorClassTransport, Retryable: true,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	locker := &fakeWeatherTaskLocker{acquired: true}
	processor := newTestMallWeatherProcessorWithLocker(t, provider, store, locker)
	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable || !locker.released {
		t.Fatalf("Process() error=%v locker=%+v", err, locker)
	}
}

func TestMallWeatherProcessorDoesNotBypassRateLimiterFailure(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	limiter := &fakeWeatherRateLimiter{err: errors.New("redis unavailable")}
	processor := newTestMallWeatherProcessorWithGuard(t, provider, store, &fakeWeatherTaskLocker{acquired: true}, limiter)
	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable || processError.Code != "RATE_LIMIT_FAILED" ||
		provider.weatherCalls != 0 || store.failure.ErrorClass != "rate_limit" {
		t.Fatalf("Process() error=%v calls=%d failure=%+v", err, provider.weatherCalls, store.failure)
	}
}

func TestMallWeatherProcessorDoesNotRecordProviderMetricBeforeProviderCall(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := &fakeMallWeatherMetricRecorder{}
	processor := newTestMallWeatherProcessorWithGuardAndMetrics(
		t,
		provider,
		store,
		&fakeWeatherTaskLocker{acquired: true},
		&fakeWeatherRateLimiter{err: errors.New("redis unavailable")},
		metrics,
	)

	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable {
		t.Fatalf("Process() error=%v", err)
	}
	assertFakeMallWeatherMetricCounters(t, metrics.counters, []fakeMallWeatherMetricCounter{{
		name: MallWeatherMetricFetchTotal,
		labels: map[string]string{
			"kind":   "full",
			"status": mallWeatherMetricStatusFailed,
		},
		value: 1,
	}})
}

func TestMallWeatherProcessorSkipsProviderWhenCircuitIsOpen(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := newInMemoryMallWeatherMetricRecorder()
	breaker := &fakeWeatherCircuitBreaker{allowed: false}
	processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
		t,
		provider,
		store,
		&fakeWeatherTaskLocker{acquired: true},
		&fakeWeatherRateLimiter{},
		metrics,
		breaker,
	)

	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable || processError.Code != "CIRCUIT_OPEN" ||
		provider.weatherCalls != 0 || breaker.allowCalls != 1 || breaker.failureCalls != 0 {
		t.Fatalf("Process() error=%v providerCalls=%d breaker=%+v", err, provider.weatherCalls, breaker)
	}
	if !mallWeatherMetricGaugeExists(metrics.GaugeSnapshot(), MallWeatherMetricProviderCircuitOpen, nil, 1) {
		t.Fatalf("GaugeSnapshot()=%+v missing circuit open gauge", metrics.GaugeSnapshot())
	}
	if !mallWeatherMetricCounterExists(
		metrics.CounterSnapshot(),
		MallWeatherMetricFetchTotal,
		map[string]string{"kind": "full", "status": mallWeatherMetricStatusFailed},
		1,
	) {
		t.Fatalf("CounterSnapshot()=%+v missing failed fetch counter", metrics.CounterSnapshot())
	}
}

func TestMallWeatherProcessorChecksCircuitAfterWaitingForRateLimit(t *testing.T) {
	provider := &fakeMallWeatherProvider{}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	breaker := &fakeWeatherCircuitBreaker{allowed: true}
	limiter := &fakeWeatherRateLimiter{onWait: func() { breaker.allowed = false }}
	processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
		t, provider, store, &fakeWeatherTaskLocker{acquired: true}, limiter,
		noopMallWeatherMetricRecorder{}, breaker,
	)

	err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{
		MallID: 7, TaskWindow: "fast:7:202611052340",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable || processError.Code != "CIRCUIT_OPEN" ||
		provider.weatherCalls != 0 || limiter.calls != 1 || breaker.allowCalls != 1 || breaker.failureCalls != 0 {
		t.Fatalf("Process() error=%v providerCalls=%d limiterCalls=%d breaker=%+v", err, provider.weatherCalls, limiter.calls, breaker)
	}
	if store.failure.ErrorClass != "circuit_open" || !reflect.DeepEqual(store.events, []string{"start", "fail"}) {
		t.Fatalf("failure=%+v events=%v", store.failure, store.events)
	}
}

func TestMallWeatherProcessorReportsProviderCircuitSuccess(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	metrics := newInMemoryMallWeatherMetricRecorder()
	breaker := &fakeWeatherCircuitBreaker{allowed: true}
	processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
		t,
		provider,
		store,
		&fakeWeatherTaskLocker{acquired: true},
		&fakeWeatherRateLimiter{},
		metrics,
		breaker,
	)

	if err := processor.Process(context.Background(), job.TypeMallWeatherFast, job.MallTaskPayload{
		MallID: 7, TaskWindow: "fast:7:202607220310",
	}); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	if breaker.allowCalls != 1 || breaker.successCalls != 1 || breaker.failureCalls != 0 {
		t.Fatalf("breaker=%+v", breaker)
	}
	if !mallWeatherMetricGaugeExists(metrics.GaugeSnapshot(), MallWeatherMetricProviderCircuitOpen, nil, 0) {
		t.Fatalf("GaugeSnapshot()=%+v missing circuit closed gauge", metrics.GaugeSnapshot())
	}
}

func TestMallWeatherProcessorPersistsAvailableV26ModulesAsPartialSuccess(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	processor := newTestMallWeatherProcessor(t, provider, store)
	payload := job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202607220310"}

	if err := processor.Process(context.Background(), job.TypeMallWeatherFast, payload); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	if store.batch == nil || store.batch.Status != weatherFetchStatusPartialSuccess ||
		store.batch.Forecasts == nil || store.batch.Forecasts.Realtime == nil {
		t.Fatalf("batch=%+v", store.batch)
	}
	if !reflect.DeepEqual(store.batch.StaleLatest.DataKinds, []string{
		model.MallWeatherDataKindMinutely,
		model.MallWeatherDataKindHourly,
		model.MallWeatherDataKindDaily,
	}) || !reflect.DeepEqual(store.batch.StaleLatest.LifeSourceAPIs, []string{weatherdomain.SourceAPIV26Daily}) {
		t.Fatalf("stale latest=%+v", store.batch.StaleLatest)
	}
	if provider.weatherRequest.HourlySteps != 24 || provider.weatherRequest.DailySteps != 1 ||
		provider.weatherRequest.Unit != "metric:v2" || !provider.weatherRequest.Alert {
		t.Fatalf("request=%+v", provider.weatherRequest)
	}
}

func TestParseMallWeatherBatchDoesNotPersistFailedRealtimeModule(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	result, ok := payload["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("fixture result=%T", payload["result"])
	}
	realtime, ok := result["realtime"].(map[string]interface{})
	if !ok {
		t.Fatalf("fixture realtime=%T", result["realtime"])
	}
	realtime["status"] = "failed"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	finishedAt := time.Date(2026, 7, 22, 3, 5, 0, 0, time.UTC)
	batch, err := parseMallWeatherBatch(caiyun.EndpointWeatherV26, raw, weatherdomain.MappingMetadata{
		MallID: 7, FetchRunID: 11, FetchedAtUTC: finishedAt, RawChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}, finishedAt)
	if err != nil {
		t.Fatalf("parseMallWeatherBatch() error=%v", err)
	}
	if batch.Forecasts == nil || batch.Forecasts.Realtime != nil || batch.Status != weatherFetchStatusPartialSuccess ||
		len(batch.StaleLatest.DataKinds) != 4 || batch.StaleLatest.DataKinds[0] != model.MallWeatherDataKindRealtime {
		t.Fatalf("batch=%+v", batch)
	}
	var counts map[string]int
	if err := json.Unmarshal([]byte(batch.RowCountsJSON), &counts); err != nil || counts[model.MallWeatherDataKindRealtime] != 0 {
		t.Fatalf("counts=%s error=%v", batch.RowCountsJSON, err)
	}
}

func TestMallWeatherProcessorRecordsProviderBodyBeforeRetryableFailure(t *testing.T) {
	providerFailure := &caiyun.ProviderError{
		Class: providerhttp.ErrorClassProvider, HTTPStatus: 503, Retryable: true,
	}
	provider := &fakeMallWeatherProvider{
		weatherResponse: &caiyun.ProviderResponse{
			EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 503,
			ProviderStatus: "unavailable", RawBody: []byte(`{"status":"unavailable"}`),
		},
		weatherErr: providerFailure,
	}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	processor := newTestMallWeatherProcessor(t, provider, store)
	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.Retryable {
		t.Fatalf("Process() error=%v", err)
	}
	if !reflect.DeepEqual(store.events, []string{"start", "response", "fail"}) || store.snapshot == nil ||
		store.failure.AttemptStatus != "provider_failed" || store.failure.ErrorClass != string(providerhttp.ErrorClassProvider) {
		t.Fatalf("events=%v snapshot=%+v failure=%+v", store.events, store.snapshot, store.failure)
	}
}

func TestMallWeatherProcessorClassifiesTransportAndParseFailures(t *testing.T) {
	tests := []struct {
		name          string
		provider      *fakeMallWeatherProvider
		wantEvents    []string
		wantStatus    string
		wantRetryable bool
	}{
		{
			name: "transport without body",
			provider: &fakeMallWeatherProvider{weatherErr: &caiyun.ProviderError{
				Class: providerhttp.ErrorClassTransport, Retryable: true,
			}},
			wantEvents: []string{"start", "fail"}, wantStatus: "transport_failed", wantRetryable: true,
		},
		{
			name: "invalid response after snapshot",
			provider: &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
				EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: []byte(`{}`),
			}},
			wantEvents: []string{"start", "response", "fail"}, wantStatus: "parse_failed", wantRetryable: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			breaker := &fakeWeatherCircuitBreaker{allowed: true}
			processor := newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
				t,
				test.provider,
				store,
				&fakeWeatherTaskLocker{acquired: true},
				&fakeWeatherRateLimiter{},
				noopMallWeatherMetricRecorder{},
				breaker,
			)
			err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
				MallID: 7, TaskWindow: "full:7:2026072203",
			})
			var processError *MallWeatherProcessError
			if !errors.As(err, &processError) || processError.Retryable != test.wantRetryable {
				t.Fatalf("Process() error=%v", err)
			}
			if !reflect.DeepEqual(store.events, test.wantEvents) || store.failure.AttemptStatus != test.wantStatus {
				t.Fatalf("events=%v failure=%+v", store.events, store.failure)
			}
			if breaker.failureCalls != 1 {
				t.Fatalf("breaker=%+v, want one failure report", breaker)
			}
		})
	}
}

func TestMallWeatherProcessorSkipsTerminalOrBusyRun(t *testing.T) {
	for _, disposition := range []data_dao.FetchAttemptDisposition{
		data_dao.FetchAttemptDispositionTerminal,
		data_dao.FetchAttemptDispositionBusy,
	} {
		store := newFakeMallWeatherTaskStore(disposition)
		provider := &fakeMallWeatherProvider{}
		processor := newTestMallWeatherProcessor(t, provider, store)
		if err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
			MallID: 7, TaskWindow: "full:7:2026072203",
		}); err != nil {
			t.Fatalf("Process(%v) error=%v", disposition, err)
		}
		if provider.weatherCalls != 0 || !reflect.DeepEqual(store.events, []string{"start"}) {
			t.Fatalf("disposition=%v provider calls=%d events=%v", disposition, provider.weatherCalls, store.events)
		}
	}
}

func TestMallWeatherProcessorFinalizesCanceledAttempt(t *testing.T) {
	provider := &fakeMallWeatherProvider{weatherErr: context.Canceled}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	processor := newTestMallWeatherProcessor(t, provider, store)
	err := processor.Process(context.Background(), job.TypeMallWeatherFull, job.MallTaskPayload{
		MallID: 7, TaskWindow: "full:7:2026072203",
	})
	if !errors.Is(err, context.Canceled) || store.failure.ErrorCode != "CANCELED" || store.failure.AttemptStatus != "transport_failed" {
		t.Fatalf("Process() error=%v failure=%+v", err, store.failure)
	}
}

func TestMallWeatherProcessorDiscardsSupersededAttemptResults(t *testing.T) {
	raw := readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json")
	provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: 200, ProviderStatus: "ok", RawBody: raw,
	}}
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	store.recordErr = ErrMallWeatherAttemptSuperseded
	processor := newTestMallWeatherProcessor(t, provider, store)
	if err := processor.Process(context.Background(), job.TypeMallWeatherLifeIndex, job.MallTaskPayload{
		MallID: 7, TaskWindow: "life:7:2026072203",
	}); err != nil {
		t.Fatalf("Process() error=%v", err)
	}
	if !reflect.DeepEqual(store.events, []string{"start", "response"}) {
		t.Fatalf("events=%v", store.events)
	}
}

type fakeMallWeatherProvider struct {
	weatherResponse *caiyun.ProviderResponse
	weatherErr      error
	weatherRequest  caiyun.WeatherRequest
	weatherCalls    int
}

func (provider *fakeMallWeatherProvider) FetchWeather(_ context.Context, input caiyun.WeatherRequest) (*caiyun.ProviderResponse, error) {
	provider.weatherCalls++
	provider.weatherRequest = input
	return provider.weatherResponse, provider.weatherErr
}

type fakeMallWeatherTaskStore struct {
	execution  *mallWeatherExecution
	start      mallWeatherTaskStart
	events     []string
	response   *caiyun.ProviderResponse
	snapshot   *model.ProviderRawSnapshot
	failure    mallWeatherFailure
	batch      *mallWeatherModelBatch
	startErr   error
	recordErr  error
	failErr    error
	persistErr error
}

func newFakeMallWeatherTaskStore(disposition data_dao.FetchAttemptDisposition) *fakeMallWeatherTaskStore {
	longitude := 121.4551234
	latitude := 31.2285678
	return &fakeMallWeatherTaskStore{execution: &mallWeatherExecution{
		Mall: model.Mall{
			BaseModel: model.BaseModel{ID: 7}, Status: "active", GeocodeStatus: "confirmed",
			WeatherEnabled: true, WeatherProvider: weatherdomain.ProviderCaiyun,
			WeatherLongitude: &longitude, WeatherLatitude: &latitude, WeatherCoordinateSystem: "GCJ02",
		},
		Run: model.MallWeatherFetchRun{
			BaseModel: model.BaseModel{ID: 17}, MallID: 7, AttemptCount: 1, Status: "running",
		},
		Attempt: model.MallWeatherFetchAttempt{
			BaseModel: model.BaseModel{ID: 23}, FetchRunID: 17, AttemptNo: 1, Status: "running",
		},
		Disposition: disposition,
	}}
}

func (store *fakeMallWeatherTaskStore) Start(_ context.Context, input mallWeatherTaskStart, _ time.Time) (*mallWeatherExecution, error) {
	store.events = append(store.events, "start")
	store.start = input
	store.execution.Run.EndpointKind = input.Payload.EndpointKind
	store.execution.Run.TaskKind = input.TaskKind
	store.execution.Run.TaskWindow = input.Payload.TaskWindow
	return store.execution, store.startErr
}

func (store *fakeMallWeatherTaskStore) RecordResponse(_ context.Context, _ *mallWeatherExecution, response *caiyun.ProviderResponse, snapshot *model.ProviderRawSnapshot) error {
	store.events = append(store.events, "response")
	store.response = response
	store.snapshot = snapshot
	return store.recordErr
}

func (store *fakeMallWeatherTaskStore) Fail(_ context.Context, _ *mallWeatherExecution, failure mallWeatherFailure) error {
	store.events = append(store.events, "fail")
	store.failure = failure
	return store.failErr
}

func (store *fakeMallWeatherTaskStore) Persist(_ context.Context, _ *mallWeatherExecution, batch *mallWeatherModelBatch) error {
	store.events = append(store.events, "persist")
	store.batch = batch
	return store.persistErr
}

func newTestMallWeatherProcessor(t *testing.T, provider mallWeatherProvider, store mallWeatherTaskStore) *MallWeatherProcessor {
	return newTestMallWeatherProcessorWithLocker(t, provider, store, &fakeWeatherTaskLocker{acquired: true})
}

func newTestMallWeatherProcessorWithMetrics(
	t *testing.T,
	provider mallWeatherProvider,
	store mallWeatherTaskStore,
	metrics mallWeatherMetricRecorder,
) *MallWeatherProcessor {
	return newTestMallWeatherProcessorWithLockerAndMetrics(t, provider, store, &fakeWeatherTaskLocker{acquired: true}, metrics)
}

func newTestMallWeatherProcessorWithLocker(t *testing.T, provider mallWeatherProvider, store mallWeatherTaskStore, locker weatherdomain.TaskLocker) *MallWeatherProcessor {
	return newTestMallWeatherProcessorWithLockerAndMetrics(t, provider, store, locker, noopMallWeatherMetricRecorder{})
}

func newTestMallWeatherProcessorWithLockerAndMetrics(
	t *testing.T,
	provider mallWeatherProvider,
	store mallWeatherTaskStore,
	locker weatherdomain.TaskLocker,
	metrics mallWeatherMetricRecorder,
) *MallWeatherProcessor {
	return newTestMallWeatherProcessorWithGuardAndMetrics(t, provider, store, locker, &fakeWeatherRateLimiter{}, metrics)
}

func newTestMallWeatherProcessorWithGuard(t *testing.T, provider mallWeatherProvider, store mallWeatherTaskStore, locker weatherdomain.TaskLocker, limiter weatherdomain.ProviderRateLimiter) *MallWeatherProcessor {
	return newTestMallWeatherProcessorWithGuardAndMetrics(t, provider, store, locker, limiter, noopMallWeatherMetricRecorder{})
}

func newTestMallWeatherProcessorWithGuardAndMetrics(
	t *testing.T,
	provider mallWeatherProvider,
	store mallWeatherTaskStore,
	locker weatherdomain.TaskLocker,
	limiter weatherdomain.ProviderRateLimiter,
	metrics mallWeatherMetricRecorder,
) *MallWeatherProcessor {
	return newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
		t,
		provider,
		store,
		locker,
		limiter,
		metrics,
		&fakeWeatherCircuitBreaker{allowed: true},
	)
}

func newTestMallWeatherProcessorWithGuardMetricsAndBreaker(
	t *testing.T,
	provider mallWeatherProvider,
	store mallWeatherTaskStore,
	locker weatherdomain.TaskLocker,
	limiter weatherdomain.ProviderRateLimiter,
	metrics mallWeatherMetricRecorder,
	breaker weatherdomain.ProviderCircuitBreaker,
) *MallWeatherProcessor {
	t.Helper()
	weatherSnapshots, err := weatherdomain.NewRawSnapshotBuilder(weatherdomain.RawSnapshotConfig{
		SchemaVersion: weatherParserVersionV26,
	}, nil)
	if err != nil {
		t.Fatalf("NewRawSnapshotBuilder(weather) error=%v", err)
	}
	lifeSnapshots, err := weatherdomain.NewRawSnapshotBuilder(weatherdomain.RawSnapshotConfig{
		SchemaVersion: weatherParserVersionLifeV3,
	}, nil)
	if err != nil {
		t.Fatalf("NewRawSnapshotBuilder(life) error=%v", err)
	}
	current := time.Date(2026, 7, 22, 3, 0, 0, 0, time.UTC)
	processor, err := newMallWeatherProcessor(provider, store, weatherSnapshots, lifeSnapshots, locker, limiter, breaker, MallWeatherProcessorConfig{
		FastHourlySteps: 24, FastDailySteps: 1, FullHourlySteps: 360, FullDailySteps: 15,
		LifeIndexDays: 15, Unit: "metric:v2", AlertEnabled: true,
		AttemptStaleAfter: 2 * time.Minute, FailureFinalizeTimeout: time.Second, LockReleaseTimeout: time.Second,
		CircuitOpenTimeout: time.Minute,
	}, func() time.Time {
		result := current
		current = current.Add(time.Second)
		return result
	}, metrics)
	if err != nil {
		t.Fatalf("newMallWeatherProcessor() error=%v", err)
	}
	return processor
}

type fakeWeatherRateLimiter struct {
	calls  int
	err    error
	onWait func()
}

func (limiter *fakeWeatherRateLimiter) Wait(context.Context) error {
	limiter.calls++
	if limiter.onWait != nil {
		limiter.onWait()
	}
	return limiter.err
}

type fakeWeatherCircuitBreaker struct {
	allowed      bool
	allowErr     error
	successErr   error
	failureErr   error
	allowCalls   int
	successCalls int
	failureCalls int
}

func (breaker *fakeWeatherCircuitBreaker) Allow(context.Context) (bool, error) {
	breaker.allowCalls++
	if breaker.allowErr != nil {
		return false, breaker.allowErr
	}
	return breaker.allowed, nil
}

func (breaker *fakeWeatherCircuitBreaker) ReportSuccess(context.Context) error {
	breaker.successCalls++
	return breaker.successErr
}

func (breaker *fakeWeatherCircuitBreaker) ReportFailure(context.Context) error {
	breaker.failureCalls++
	return breaker.failureErr
}

type fakeWeatherTaskLocker struct {
	key      string
	acquired bool
	err      error
	released bool
}

func (locker *fakeWeatherTaskLocker) Acquire(_ context.Context, key string) (weatherdomain.TaskLock, bool, error) {
	locker.key = key
	if locker.err != nil || !locker.acquired {
		return nil, false, locker.err
	}
	return &fakeWeatherTaskLock{locker: locker}, true, nil
}

type fakeWeatherTaskLock struct {
	locker *fakeWeatherTaskLocker
}

func (lock *fakeWeatherTaskLock) Release(context.Context) error {
	lock.locker.released = true
	return nil
}

func readMallWeatherFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile(%q) error=%v", path, err)
	}
	return raw
}

func fullComprehensiveWeatherResponse(t *testing.T) []byte {
	t.Helper()
	var envelope map[string]interface{}
	if err := json.Unmarshal(readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json"), &envelope); err != nil {
		t.Fatalf("decode comprehensive weather fixture: %v", err)
	}
	result, ok := envelope["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("comprehensive weather result=%T", envelope["result"])
	}
	precipitation2H := make([]float64, 120)
	precipitation := make([]float64, 60)
	result["minutely"] = map[string]interface{}{
		"status": "ok", "datasource": "radar", "description": "未来两小时无明显降水",
		"precipitation_2h": precipitation2H, "precipitation": precipitation,
		"probability": []float64{0, 0, 0, 0},
	}
	hourlyTemperature := make([]interface{}, 0, 360)
	hourlySkycon := make([]interface{}, 0, 360)
	hourlyStart := time.Date(2026, 7, 22, 11, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	for hour := 0; hour < 360; hour++ {
		forecastTime := hourlyStart.Add(time.Duration(hour) * time.Hour).Format("2006-01-02T15:04Z07:00")
		hourlyTemperature = append(hourlyTemperature, map[string]interface{}{"datetime": forecastTime, "value": 33.2})
		hourlySkycon = append(hourlySkycon, map[string]interface{}{"datetime": forecastTime, "value": "PARTLY_CLOUDY_DAY"})
	}
	result["hourly"] = map[string]interface{}{
		"status": "ok", "description": "未来二十四小时天气稳定",
		"temperature": hourlyTemperature,
		"skycon":      hourlySkycon,
	}
	dailyTemperature := make([]interface{}, 0, 15)
	dailySkycon := make([]interface{}, 0, 15)
	dailyComfort := make([]interface{}, 0, 15)
	for day := 0; day < 15; day++ {
		forecastDate := hourlyStart.AddDate(0, 0, day).Format("2006-01-02T00:00Z07:00")
		dailyTemperature = append(dailyTemperature, map[string]interface{}{
			"date": forecastDate, "max": 35.0, "min": 27.0, "avg": 31.0,
		})
		dailySkycon = append(dailySkycon, map[string]interface{}{
			"date": forecastDate, "value": "PARTLY_CLOUDY_DAY",
		})
		dailyComfort = append(dailyComfort, map[string]interface{}{
			"date": forecastDate, "index": "5", "desc": "闷热",
		})
	}
	result["daily"] = map[string]interface{}{
		"status":      "ok",
		"temperature": dailyTemperature,
		"skycon":      dailySkycon,
		"life_index": map[string]interface{}{
			"comfort": dailyComfort,
		},
	}
	result["alert"] = map[string]interface{}{
		"status": "ok", "request_status": "ok", "content": []interface{}{},
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode comprehensive weather fixture: %v", err)
	}
	return raw
}

func mallWeatherMetricCounterExists(
	samples []MallWeatherMetricCounterSample,
	name string,
	labels map[string]string,
	value int64,
) bool {
	for _, sample := range samples {
		if sample.Name != name || sample.Value != value || len(sample.Labels) != len(labels) {
			continue
		}
		matches := true
		for key, labelValue := range labels {
			if sample.Labels[key] != labelValue {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func mallWeatherMetricGaugeExists(
	samples []MallWeatherMetricGaugeSample,
	name string,
	labels map[string]string,
	value float64,
) bool {
	for _, sample := range samples {
		if sample.Name != name || sample.Value != value || len(sample.Labels) != len(labels) {
			continue
		}
		matches := true
		for key, labelValue := range labels {
			if sample.Labels[key] != labelValue {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
