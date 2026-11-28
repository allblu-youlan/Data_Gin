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
	"gin-biz-web-api/pkg/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestMallWeatherLockDeferralWaitsForLeaseWithoutStartingAttempt(t *testing.T) {
	createdAt := time.Date(2026, 11, 28, 5, 34, 11, 734000000, time.UTC)
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	store.execution.Run.CreatedAt = createdAt
	provider := &fakeMallWeatherProvider{}
	locker := &fakeWeatherTaskLocker{}
	processor := newTestMallWeatherProcessorWithLocker(t, provider, store, locker)
	payload := job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611281330"}
	for _, elapsed := range []time.Duration{2 * time.Minute, 3 * time.Minute, 10 * time.Minute, 11*time.Minute + 45*time.Second} {
		processor.now = func() time.Time { return createdAt.Add(elapsed) }
		err := processor.Process(t.Context(), job.TypeMallWeatherFast, payload)
		var processError *MallWeatherProcessError
		wantDelay := min(30*time.Second, 12*time.Minute-elapsed)
		if !errors.As(err, &processError) || processError.Code != "LOCK_BUSY" || !weatherTaskDeferred(err) || processError.RetryDelay() != wantDelay {
			t.Fatalf("elapsed=%s error=%v must defer for %s", elapsed, err, wantDelay)
		}
	}
	if len(store.events) != 0 || provider.weatherCalls != 0 {
		t.Fatalf("lock wait started another attempt: events=%v calls=%d", store.events, provider.weatherCalls)
	}
	locker.acquired = true
	provider.weatherResponse = &caiyun.ProviderResponse{
		EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: http.StatusOK, ProviderStatus: "ok",
		RawBody: readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json"),
	}
	if err := processor.Process(t.Context(), job.TypeMallWeatherFast, payload); err != nil {
		t.Fatalf("released lease did not recover: %v", err)
	}
	if provider.weatherCalls != 1 || store.batch == nil {
		t.Fatal("released lease must fetch and persist once")
	}
}

func TestMallWeatherLockDeferralUsesFixedDeadlineAndTerminalState(t *testing.T) {
	createdAt := time.Date(2026, 11, 28, 5, 34, 11, 0, time.UTC)
	for _, test := range []struct {
		name, status, wantCode string
		createdAt              time.Time
		elapsed                time.Duration
	}{
		{name: "deadline reached", status: "running", createdAt: createdAt, elapsed: 12 * time.Minute, wantCode: "LOCK_WAIT_EXPIRED"},
		{name: "deadline cannot renew", status: "running", createdAt: createdAt, elapsed: 20 * time.Minute, wantCode: "LOCK_WAIT_EXPIRED"},
		{name: "no persistent anchor", status: "running", wantCode: "LOCK_WAIT_STATE_MISSING"},
		{name: "already succeeded", status: "success", createdAt: createdAt, elapsed: 20 * time.Minute},
		{name: "already partially succeeded", status: "partial_success", createdAt: createdAt, elapsed: 20 * time.Minute},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
			store.execution.Run.CreatedAt, store.execution.Run.Status = test.createdAt, test.status
			provider := &fakeMallWeatherProvider{}
			processor := newTestMallWeatherProcessorWithLocker(t, provider, store, &fakeWeatherTaskLocker{})
			processor.now = func() time.Time { return createdAt.Add(test.elapsed) }
			err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611281330"})
			if test.wantCode == "" {
				if err != nil {
					t.Fatalf("completed task was not idempotent: %v", err)
				}
			} else {
				var processError *MallWeatherProcessError
				if !errors.As(err, &processError) || processError.Code != test.wantCode || processError.Retryable || weatherTaskDeferred(err) {
					t.Fatalf("error=%v want terminal %s", err, test.wantCode)
				}
			}
			if len(store.events) != 0 || provider.weatherCalls != 0 {
				t.Fatal("busy lock changed persisted attempts or called provider")
			}
		})
	}
}

func TestMallWeatherLockAcquisitionFailureIsNotDeferred(t *testing.T) {
	cause := errors.New("Redis unavailable")
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	provider := &fakeMallWeatherProvider{}
	processor := newTestMallWeatherProcessorWithLocker(t, provider, store, &fakeWeatherTaskLocker{err: cause})
	err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611281330"})
	var processError *MallWeatherProcessError
	if !errors.Is(err, cause) || !errors.As(err, &processError) || processError.Code != "LOCK_ACQUIRE_FAILED" || weatherTaskDeferred(err) || len(store.events) != 0 {
		t.Fatalf("real lock failure was hidden as a wait: %v", err)
	}
}

func TestMallWeatherLockWaitUsesConfiguredLeaseTTL(t *testing.T) {
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	processor := newTestMallWeatherProcessorWithLocker(t, &fakeMallWeatherProvider{}, store, &fakeWeatherTaskLocker{})
	processor.config.TaskLockTTL = 3 * time.Minute
	processor.now = func() time.Time { return store.execution.Run.CreatedAt.Add(4*time.Minute + 45*time.Second) }
	err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611281330"})
	var processError *MallWeatherProcessError
	if !errors.As(err, &processError) || !processError.TaskDeferred() || processError.RetryDelay() != 15*time.Second {
		t.Fatalf("wait did not use configured TTL plus queue allowance: %v", err)
	}
}

func TestMallWeatherLockWaitReadsStateOnlyWhenBusy(t *testing.T) {
	for _, acquired := range []bool{false, true} {
		cause := errors.New("database read unavailable")
		store := &weatherLockWaitErrorStore{fakeMallWeatherTaskStore: newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired), err: cause}
		provider := &fakeMallWeatherProvider{weatherResponse: &caiyun.ProviderResponse{
			EndpointKind: caiyun.EndpointWeatherV26, HTTPStatus: http.StatusOK, ProviderStatus: "ok",
			RawBody: readMallWeatherFixture(t, "../../../connector/caiyun/testdata/weather_v26_realtime.json"),
		}}
		processor := newTestMallWeatherProcessorWithLocker(t, provider, store, &fakeWeatherTaskLocker{acquired: acquired})
		err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611281330"})
		if acquired {
			if err != nil || store.reads != 0 || provider.weatherCalls != 1 {
				t.Fatalf("normal path queried lock state: error=%v reads=%d", err, store.reads)
			}
		} else if !errors.Is(err, cause) || weatherTaskDeferred(err) || store.reads != 1 || len(store.events) != 0 || provider.weatherCalls != 0 {
			t.Fatalf("failed state read was hidden as deferral: error=%v reads=%d", err, store.reads)
		}
	}
}

func TestMallWeatherReleaseFailureKeepsOriginalFailureAndWarns(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	cause := errors.New("start unavailable")
	store := newFakeMallWeatherTaskStore(data_dao.FetchAttemptDispositionAcquired)
	store.startErr = cause
	processor := newTestMallWeatherProcessorWithLocker(t, &fakeMallWeatherProvider{}, store, weatherReleaseErrorLocker{})
	err := processor.Process(t.Context(), job.TypeMallWeatherFast, job.MallTaskPayload{MallID: 7, TaskWindow: "fast:7:202611281330"})
	var processError *MallWeatherProcessError
	entries := logs.All()
	if !errors.Is(err, cause) || !errors.As(err, &processError) || processError.Code != "START_FAILED" || len(entries) != 1 || entries[0].Level != zap.WarnLevel || entries[0].Message != "Mall weather task lock release failed after processing failure" {
		t.Fatalf("release failure hid original error or was lost: error=%v logs=%v", err, entries)
	}
}

type weatherLockWaitErrorStore struct {
	*fakeMallWeatherTaskStore
	err   error
	reads int
}

func (s *weatherLockWaitErrorStore) LockWaitState(context.Context, mallWeatherTaskStart) (mallWeatherLockWaitState, error) {
	s.reads++
	return mallWeatherLockWaitState{}, s.err
}

type weatherReleaseErrorLocker struct{}

func (weatherReleaseErrorLocker) Acquire(context.Context, string) (weatherdomain.TaskLock, bool, error) {
	return weatherReleaseErrorLocker{}, true, nil
}
func (weatherReleaseErrorLocker) Release(context.Context) error {
	return errors.New("Redis release unavailable")
}
