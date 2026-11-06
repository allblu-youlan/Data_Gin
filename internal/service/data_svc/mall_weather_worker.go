package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/internal/dao/data_dao"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"

	"go.uber.org/zap"
)

const (
	weatherFetchStatusSuccess        = "success"
	weatherFetchStatusPartialSuccess = "partial_success"
	weatherParserVersionV26          = "caiyun-v26-v1"
	weatherParserVersionLifeV3       = "caiyun-v3-lifeindex-v1"
	mallWeatherCircuitWaitLimit      = 10 * time.Minute
)

var ErrMallWeatherAttemptSuperseded = errors.New("mall weather: fetch attempt superseded")

var partialWeatherWarningCodes = map[string]struct{}{
	"CORE_FIELD_COVERAGE_LOW": {}, "EMPTY_DAY": {}, "FORECAST_DATE_OUT_OF_RANGE": {},
	"FORECAST_TIME_OUT_OF_RANGE": {}, "INVALID_ALERT_ID": {}, "INVALID_DATE": {},
	"INVALID_DATETIME": {}, "INVALID_ITEM": {}, "MODULE_PARSE_FAILED": {},
	"MODULE_STATUS_NOT_OK": {}, "REQUEST_STATUS_NOT_OK": {},
}

type MallWeatherProcessError struct {
	Retryable  bool
	Code       string
	cause      error
	retryDelay time.Duration
}

func (processError *MallWeatherProcessError) Error() string {
	message := "mall weather: processing failed"
	if processError == nil {
		return message
	}
	if processError.Code != "" {
		message += " code=" + processError.Code
	}
	var providerError *caiyun.ProviderError
	if errors.As(processError.cause, &providerError) && providerError != nil {
		message += " (" + providerError.Error() + ")"
	}
	return message
}

func (processError *MallWeatherProcessError) RetryDelay() time.Duration {
	if processError == nil || !processError.Retryable {
		return 0
	}
	return processError.retryDelay
}

func (processError *MallWeatherProcessError) TaskDeferred() bool {
	return processError != nil && processError.Retryable && processError.Code == "CIRCUIT_OPEN"
}

func (processError *MallWeatherProcessError) Unwrap() error {
	if processError == nil {
		return nil
	}
	return processError.cause
}

type mallWeatherProvider interface {
	FetchWeather(ctx context.Context, input caiyun.WeatherRequest) (*caiyun.ProviderResponse, error)
}

type mallWeatherTaskStore interface {
	Start(ctx context.Context, input mallWeatherTaskStart, startedAt time.Time) (*mallWeatherExecution, error)
	RecordResponse(ctx context.Context, execution *mallWeatherExecution, response *caiyun.ProviderResponse, snapshot *model.ProviderRawSnapshot) error
	Fail(ctx context.Context, execution *mallWeatherExecution, failure mallWeatherFailure) error
	Persist(ctx context.Context, execution *mallWeatherExecution, batch *mallWeatherModelBatch) error
}

type mallWeatherTaskStart struct {
	TaskType             string
	TaskKind             string
	Payload              job.MallTaskPayload
	RequestedHourlySteps int
	RequestedDailySteps  int
	AttemptStaleAfter    time.Duration
}

type mallWeatherExecution struct {
	Mall        model.Mall
	Run         model.MallWeatherFetchRun
	Attempt     model.MallWeatherFetchAttempt
	Disposition data_dao.FetchAttemptDisposition
}

type mallWeatherFailure struct {
	AttemptStatus     string
	ErrorClass        string
	ErrorCode         string
	ErrorMessageSafe  string
	FinishedAt        time.Time
	ParserVersion     string
	ParseWarningsJSON model.JSONText
	RetryAfter        time.Duration
}

type mallWeatherModelBatch struct {
	EndpointKind       string
	Status             string
	ParserVersion      string
	ProviderServerTime *time.Time
	Forecasts          *weatherdomain.ForecastModelBatch
	Daily              *weatherdomain.DailyModelBatch
	Alerts             *weatherdomain.AlertModelBatch
	LifeIndices        *weatherdomain.LifeIndexModelBatch
	StaleLatest        data_dao.MallWeatherLatestStaleScope
	ParseWarningsJSON  model.JSONText
	RowCountsJSON      model.JSONText
	FinishedAt         time.Time
}

type MallWeatherProcessorConfig struct {
	FastHourlySteps        int
	FastDailySteps         int
	FullHourlySteps        int
	FullDailySteps         int
	LifeIndexDays          int
	Unit                   string
	AlertEnabled           bool
	AttemptStaleAfter      time.Duration
	FailureFinalizeTimeout time.Duration
	LockReleaseTimeout     time.Duration
	CircuitOpenTimeout     time.Duration
}

type MallWeatherProcessor struct {
	provider         mallWeatherProvider
	store            mallWeatherTaskStore
	weatherSnapshots *weatherdomain.RawSnapshotBuilder
	lifeSnapshots    *weatherdomain.RawSnapshotBuilder
	locker           weatherdomain.TaskLocker
	limiter          weatherdomain.ProviderRateLimiter
	breaker          weatherdomain.ProviderCircuitBreaker
	config           MallWeatherProcessorConfig
	now              func() time.Time
	metrics          mallWeatherMetricRecorder
}

func newMallWeatherProcessor(
	provider mallWeatherProvider,
	store mallWeatherTaskStore,
	weatherSnapshots *weatherdomain.RawSnapshotBuilder,
	lifeSnapshots *weatherdomain.RawSnapshotBuilder,
	locker weatherdomain.TaskLocker,
	limiter weatherdomain.ProviderRateLimiter,
	breaker weatherdomain.ProviderCircuitBreaker,
	config MallWeatherProcessorConfig,
	now func() time.Time,
	metrics mallWeatherMetricRecorder,
) (*MallWeatherProcessor, error) {
	if provider == nil || store == nil || weatherSnapshots == nil || lifeSnapshots == nil || locker == nil ||
		limiter == nil || breaker == nil || now == nil || metrics == nil ||
		config.FastHourlySteps < 1 || config.FastHourlySteps > 360 || config.FastDailySteps < 1 || config.FastDailySteps > 15 ||
		config.FullHourlySteps < 1 || config.FullHourlySteps > 360 || config.FullDailySteps < 1 || config.FullDailySteps > 15 ||
		config.LifeIndexDays < 1 || config.LifeIndexDays > 15 || config.Unit != "metric:v2" ||
		config.AttemptStaleAfter <= 0 || config.FailureFinalizeTimeout <= 0 || config.LockReleaseTimeout <= 0 ||
		config.CircuitOpenTimeout <= 0 {
		return nil, fmt.Errorf("mall weather: invalid processor configuration")
	}
	return &MallWeatherProcessor{
		provider: provider, store: store, weatherSnapshots: weatherSnapshots, lifeSnapshots: lifeSnapshots,
		locker: locker, limiter: limiter, breaker: breaker, config: config, now: now,
		metrics: metrics,
	}, nil
}

func (processor *MallWeatherProcessor) Process(ctx context.Context, taskType string, payload job.MallTaskPayload) (resultErr error) {
	if processor == nil || ctx == nil {
		return &MallWeatherProcessError{Code: "INVALID_PROCESSOR"}
	}
	start, err := processor.taskStart(taskType, payload)
	if err != nil {
		return &MallWeatherProcessError{Code: "INVALID_TASK", cause: err}
	}
	lock, acquired, err := processor.locker.Acquire(ctx, weatherTaskLockKey(start))
	if err != nil {
		return &MallWeatherProcessError{Retryable: true, Code: "LOCK_ACQUIRE_FAILED", cause: err}
	}
	if !acquired {
		return &MallWeatherProcessError{Retryable: true, Code: "LOCK_BUSY"}
	}
	if lock == nil {
		return &MallWeatherProcessError{Retryable: true, Code: "INVALID_TASK_LOCK"}
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), processor.config.LockReleaseTimeout)
		defer cancel()
		if err := lock.Release(releaseCtx); err != nil && resultErr == nil {
			resultErr = &MallWeatherProcessError{Retryable: true, Code: "LOCK_RELEASE_FAILED", cause: err}
		}
	}()
	startedAt := processor.now().UTC()
	execution, err := processor.store.Start(ctx, start, startedAt)
	if err != nil {
		return &MallWeatherProcessError{Retryable: true, Code: "START_FAILED", cause: err}
	}
	if execution == nil || execution.Disposition == data_dao.FetchAttemptDispositionBusy ||
		execution.Disposition == data_dao.FetchAttemptDispositionTerminal {
		return nil
	}
	if execution.Disposition != data_dao.FetchAttemptDispositionAcquired || execution.Run.ID == 0 || execution.Attempt.ID == 0 {
		return &MallWeatherProcessError{Retryable: true, Code: "INVALID_EXECUTION"}
	}
	if err := validateWeatherExecution(execution, start); err != nil {
		return processor.finishFailure(ctx, execution, mallWeatherFailure{
			AttemptStatus: "provider_failed", ErrorClass: "invalid_request", ErrorCode: "INVALID_MALL_WEATHER_POINT",
			ErrorMessageSafe: "mall weather request point is not eligible", FinishedAt: processor.now().UTC(),
		}, err, false)
	}
	if err := processor.limiter.Wait(ctx); err != nil {
		return processor.finishFailure(ctx, execution, mallWeatherFailure{
			AttemptStatus: "transport_failed", ErrorClass: "rate_limit", ErrorCode: "RATE_LIMIT_FAILED",
			ErrorMessageSafe: "weather provider rate limit could not be acquired", FinishedAt: processor.now().UTC(),
		}, err, true)
	}
	allowed, retryAfter, err := processor.breaker.Allow(ctx)
	if err != nil {
		return processor.finishFailure(ctx, execution, mallWeatherFailure{
			AttemptStatus: "transport_failed", ErrorClass: "circuit", ErrorCode: "CIRCUIT_CHECK_FAILED",
			ErrorMessageSafe: "weather provider circuit state could not be checked", FinishedAt: processor.now().UTC(),
		}, err, true)
	}
	if !allowed {
		recordMallWeatherProviderCircuitOpen(processor.metrics, true)
		finishedAt := processor.now().UTC()
		remaining := execution.Run.CreatedAt.Add(mallWeatherCircuitWaitLimit).Sub(finishedAt)
		deferred := !execution.Run.CreatedAt.IsZero() && remaining > 0
		failure := mallWeatherFailure{
			AttemptStatus: "provider_skipped", ErrorClass: "circuit_open", ErrorCode: "CIRCUIT_OPEN",
			ErrorMessageSafe: "weather provider circuit is open", FinishedAt: finishedAt,
			RetryAfter: retryAfter,
		}
		if !deferred {
			failure.ErrorCode = "CIRCUIT_WAIT_EXPIRED"
			failure.ErrorMessageSafe = "weather provider circuit wait exceeded the task lifetime"
		}
		err := processor.finishFailure(ctx, execution, failure, fmt.Errorf("weather provider circuit is open"), deferred)
		var processError *MallWeatherProcessError
		if errors.As(err, &processError) && processError.TaskDeferred() {
			// Keep the deadline fixed across attempts, including the queue rounding allowance.
			processError.retryDelay = min(processError.retryDelay, remaining)
		}
		return err
	}
	recordMallWeatherProviderCircuitOpen(processor.metrics, false)
	response, providerErr := processor.fetch(ctx, start, &execution.Mall)
	finishedAt := processor.now().UTC()
	if response != nil && response.EndpointKind != start.Payload.EndpointKind {
		providerErr = &caiyun.ProviderError{Class: "invalid_response"}
	}
	if providerErr == nil && (response == nil || len(response.RawBody) == 0) {
		providerErr = &caiyun.ProviderError{Class: "invalid_response"}
	}
	if providerErr != nil {
		processor.reportProviderCircuitFailure(ctx, providerErr)
	}
	recordMallWeatherProviderRequest(processor.metrics, start.Payload.EndpointKind, providerErr == nil)
	recordMallWeatherProviderRateLimited(processor.metrics, providerErr)
	recordMallWeatherProviderAuthFailure(processor.metrics, start.Payload.EndpointKind, providerErr)
	if response != nil && len(response.RawBody) > 0 {
		snapshot, snapshotErr := processor.snapshotBuilder(start.Payload.EndpointKind).Build(ctx, weatherdomain.RawSnapshotInput{
			Provider: weatherdomain.ProviderCaiyun, EndpointKind: start.Payload.EndpointKind,
			MallID: &execution.Mall.ID, ReceivedAtUTC: finishedAt, Body: response.RawBody,
		})
		if snapshotErr != nil {
			return processor.finishFailure(ctx, execution, mallWeatherFailure{
				AttemptStatus: "persist_failed", ErrorClass: "snapshot", ErrorCode: "SNAPSHOT_FAILED",
				ErrorMessageSafe: "weather response snapshot could not be stored", FinishedAt: finishedAt,
			}, errors.Join(snapshotErr, providerErr), true)
		}
		execution.Run.ResponseChecksum = snapshot.ResponseChecksum
		if err := processor.store.RecordResponse(ctx, execution, response, snapshot); err != nil {
			if errors.Is(err, ErrMallWeatherAttemptSuperseded) {
				return nil
			}
			return processor.finishFailure(ctx, execution, mallWeatherFailure{
				AttemptStatus: "persist_failed", ErrorClass: "database", ErrorCode: "SNAPSHOT_RECORD_FAILED",
				ErrorMessageSafe: "weather response snapshot could not be recorded", FinishedAt: finishedAt,
			}, errors.Join(err, providerErr), true)
		}
	}
	if providerErr != nil {
		failure, retryable := classifyWeatherProviderFailure(providerErr, response, finishedAt)
		return processor.finishFailure(ctx, execution, failure, providerErr, retryable)
	}

	metadata := weatherdomain.MappingMetadata{
		MallID: execution.Mall.ID, FetchRunID: execution.Run.ID,
		FetchedAtUTC: finishedAt, RawChecksum: execution.Run.ResponseChecksum,
	}
	batch, err := parseMallWeatherBatch(start.Payload.EndpointKind, response.RawBody, metadata, finishedAt)
	if err != nil {
		processor.reportProviderCircuitFailure(ctx, err)
		return processor.finishFailure(ctx, execution, mallWeatherFailure{
			AttemptStatus: "parse_failed", ErrorClass: "invalid_response", ErrorCode: "PARSE_FAILED",
			ErrorMessageSafe: "weather provider response could not be parsed", FinishedAt: finishedAt,
			ParserVersion: parserVersionForEndpoint(start.Payload.EndpointKind), ParseWarningsJSON: model.JSONText("[]"),
		}, err, false)
	}
	processor.reportProviderCircuitSuccess(ctx)
	if err := processor.store.Persist(ctx, execution, batch); err != nil {
		if errors.Is(err, ErrMallWeatherAttemptSuperseded) {
			return nil
		}
		return processor.finishFailure(ctx, execution, mallWeatherFailure{
			AttemptStatus: "persist_failed", ErrorClass: "database", ErrorCode: "PERSIST_FAILED",
			ErrorMessageSafe: "weather business data could not be stored", FinishedAt: finishedAt,
		}, err, true)
	}
	recordMallWeatherFetchDuration(processor.metrics, start.TaskKind, startedAt, finishedAt)
	recordMallWeatherDataAge(processor.metrics, start.TaskKind, batch.ProviderServerTime, finishedAt)
	recordMallWeatherParseWarnings(processor.metrics, batch.ParseWarningsJSON)
	recordMallWeatherFetch(processor.metrics, start.TaskKind, batch.Status)
	return nil
}

func (processor *MallWeatherProcessor) reportProviderCircuitSuccess(ctx context.Context) {
	if err := processor.breaker.ReportSuccess(ctx); err != nil {
		logger.Warn("Mall weather provider circuit success report failed", zap.Error(err))
		return
	}
}

func (processor *MallWeatherProcessor) reportProviderCircuitFailure(ctx context.Context, cause error) {
	var providerError *caiyun.ProviderError
	var err error
	if errors.As(cause, &providerError) && providerError != nil && providerError.Class == providerhttp.ErrorClassRateLimited {
		err = processor.breaker.ReportRateLimited(ctx, providerError.RetryDelay())
		if err == nil {
			recordMallWeatherProviderCircuitOpen(processor.metrics, true)
		}
	} else {
		err = processor.breaker.ReportFailure(ctx)
	}
	if err != nil {
		logger.Warn("Mall weather provider circuit failure report failed", zap.Error(err), zap.NamedError("cause", cause))
	}
}

func weatherTaskLockKey(start mallWeatherTaskStart) string {
	return strconv.FormatUint(uint64(start.Payload.MallID), 10) + ":" + start.TaskKind + ":" + start.Payload.TaskWindow
}

func (processor *MallWeatherProcessor) taskStart(taskType string, payload job.MallTaskPayload) (mallWeatherTaskStart, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return mallWeatherTaskStart{}, err
	}
	payload, err = job.DecodeMallWeatherTaskPayload(taskType, raw)
	if err != nil {
		return mallWeatherTaskStart{}, err
	}
	start := mallWeatherTaskStart{
		TaskType: taskType, Payload: payload, AttemptStaleAfter: processor.config.AttemptStaleAfter,
	}
	switch taskType {
	case job.TypeMallWeatherFast:
		start.TaskKind = "fast"
		start.RequestedHourlySteps = processor.config.FastHourlySteps
		start.RequestedDailySteps = processor.config.FastDailySteps
	case job.TypeMallWeatherFull:
		start.TaskKind = "full"
		start.RequestedHourlySteps = processor.config.FullHourlySteps
		start.RequestedDailySteps = processor.config.FullDailySteps
	case job.TypeMallWeatherLifeIndex:
		start.TaskKind = "lifeindex"
		start.RequestedHourlySteps = processor.config.FullHourlySteps
		start.RequestedDailySteps = processor.config.FullDailySteps
	case job.TypeMallWeatherRepair:
		start.TaskKind = "repair"
		processor.setEndpointSteps(&start)
	case job.TypeMallWeatherManual:
		start.TaskKind = "manual"
		processor.setEndpointSteps(&start)
	default:
		return mallWeatherTaskStart{}, fmt.Errorf("unsupported task type")
	}
	return start, nil
}

func (processor *MallWeatherProcessor) setEndpointSteps(start *mallWeatherTaskStart) {
	start.RequestedHourlySteps = processor.config.FullHourlySteps
	start.RequestedDailySteps = processor.config.FullDailySteps
}

func (processor *MallWeatherProcessor) fetch(ctx context.Context, start mallWeatherTaskStart, mall *model.Mall) (*caiyun.ProviderResponse, error) {
	return processor.provider.FetchWeather(ctx, caiyun.WeatherRequest{
		Longitude: *mall.WeatherLongitude, Latitude: *mall.WeatherLatitude,
		HourlySteps: start.RequestedHourlySteps, DailySteps: start.RequestedDailySteps,
		Alert: processor.config.AlertEnabled, Unit: processor.config.Unit,
	})
}

func (processor *MallWeatherProcessor) snapshotBuilder(endpointKind string) *weatherdomain.RawSnapshotBuilder {
	if endpointKind == caiyun.EndpointLifeIndexV3 {
		return processor.lifeSnapshots
	}
	return processor.weatherSnapshots
}

func (processor *MallWeatherProcessor) finishFailure(ctx context.Context, execution *mallWeatherExecution, failure mallWeatherFailure, cause error, retryable bool) error {
	failureParent := ctx
	if ctx.Err() != nil {
		failureParent = context.WithoutCancel(ctx)
	}
	failureCtx, cancel := context.WithTimeout(failureParent, processor.config.FailureFinalizeTimeout)
	defer cancel()
	if err := processor.store.Fail(failureCtx, execution, failure); err != nil {
		if errors.Is(err, ErrMallWeatherAttemptSuperseded) {
			return nil
		}
		return &MallWeatherProcessError{Retryable: true, Code: "FAILURE_AUDIT_FAILED", cause: err}
	}
	recordMallWeatherFetch(processor.metrics, execution.Run.TaskKind, mallWeatherMetricStatusFailed)
	if errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) {
		return cause
	}
	processError := &MallWeatherProcessError{
		Retryable: retryable, Code: failure.ErrorCode, cause: cause, retryDelay: failure.RetryAfter,
	}
	var providerError *caiyun.ProviderError
	rateLimited := failure.ErrorClass == string(providerhttp.ErrorClassRateLimited)
	if retryable && errors.As(cause, &providerError) && providerError != nil {
		processError.retryDelay = max(processError.retryDelay, providerError.RetryDelay())
		rateLimited = rateLimited || providerError.Class == providerhttp.ErrorClassRateLimited
	}
	if retryable && (rateLimited || failure.ErrorClass == "circuit_open") {
		processError.retryDelay = max(processError.retryDelay, processor.config.CircuitOpenTimeout)
	}
	if processError.retryDelay > 0 && processError.retryDelay <= time.Duration(math.MaxInt64)-time.Second {
		// Asynq stores retry timestamps in whole seconds, while the circuit uses milliseconds.
		processError.retryDelay += time.Second
	}
	return processError
}

func parserVersionForEndpoint(endpointKind string) string {
	if endpointKind == caiyun.EndpointLifeIndexV3 {
		return weatherParserVersionLifeV3
	}
	return weatherParserVersionV26
}

func validateWeatherExecution(execution *mallWeatherExecution, start mallWeatherTaskStart) error {
	mall := &execution.Mall
	if mall.ID == 0 || !mall.WeatherEnabled || mall.Status != "active" || mall.WeatherProvider != weatherdomain.ProviderCaiyun ||
		mall.GeocodeStatus != "confirmed" || mall.WeatherLongitude == nil || mall.WeatherLatitude == nil ||
		mall.WeatherCoordinateSystem == "" || *mall.WeatherLongitude < -180 || *mall.WeatherLongitude > 180 ||
		*mall.WeatherLatitude < -90 || *mall.WeatherLatitude > 90 || math.IsNaN(*mall.WeatherLongitude) ||
		math.IsInf(*mall.WeatherLongitude, 0) || math.IsNaN(*mall.WeatherLatitude) || math.IsInf(*mall.WeatherLatitude, 0) ||
		(start.Payload.EndpointKind != caiyun.EndpointWeatherV26 && start.Payload.EndpointKind != caiyun.EndpointLifeIndexV3) ||
		execution.Run.MallID != mall.ID || execution.Run.EndpointKind != start.Payload.EndpointKind ||
		execution.Run.TaskKind != start.TaskKind || execution.Run.TaskWindow != start.Payload.TaskWindow || execution.Run.Status != "running" ||
		execution.Attempt.FetchRunID != execution.Run.ID || execution.Attempt.AttemptNo != execution.Run.AttemptCount ||
		execution.Attempt.Status != "running" {
		return fmt.Errorf("invalid mall weather execution")
	}
	return nil
}

func classifyWeatherProviderFailure(err error, response *caiyun.ProviderResponse, finishedAt time.Time) (mallWeatherFailure, bool) {
	failure := mallWeatherFailure{
		AttemptStatus: "transport_failed", ErrorClass: "transport", ErrorCode: "PROVIDER_REQUEST_FAILED",
		ErrorMessageSafe: "weather provider request failed", FinishedAt: finishedAt,
	}
	if response != nil {
		failure.AttemptStatus = "provider_failed"
	}
	var providerError *caiyun.ProviderError
	if errors.As(err, &providerError) {
		failure.ErrorClass = string(providerError.Class)
		if providerError.Code != "" {
			failure.ErrorCode = providerError.Code
		}
		return failure, providerError.Retryable
	}
	if errors.Is(err, context.Canceled) {
		failure.ErrorClass = "canceled"
		failure.ErrorCode = "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		failure.ErrorClass = "timeout"
		failure.ErrorCode = "TIMEOUT"
		return failure, true
	}
	return failure, true
}

func parseMallWeatherBatch(endpointKind string, raw []byte, metadata weatherdomain.MappingMetadata, finishedAt time.Time) (*mallWeatherModelBatch, error) {
	if endpointKind == caiyun.EndpointLifeIndexV3 {
		bundle, err := caiyun.ParseLifeIndexV3(raw)
		if err != nil {
			return nil, err
		}
		mapped, err := weatherdomain.MapLifeIndices(weatherdomain.LifeIndexMappingInput{
			Metadata: metadata, IssuedAtUTC: finishedAt, LifeIndex: bundle,
		})
		if err != nil {
			return nil, err
		}
		warnings, counts, err := aggregateBatchMetadata(mapped.ParseWarningsJSON, mapped.RowCountsJSON)
		if err != nil {
			return nil, err
		}
		status := weatherFetchStatusSuccess
		if warningsRequirePartial(warnings) {
			status = weatherFetchStatusPartialSuccess
		}
		return newMallWeatherModelBatch(endpointKind, status, weatherParserVersionLifeV3, nil, nil, nil, mapped, warnings, counts, finishedAt)
	}

	weatherBundle, err := caiyun.ParseWeatherV26(raw)
	if err != nil {
		return nil, err
	}
	moduleWarnings := make([]caiyun.ParseWarning, 0, 4)
	staleLatest := data_dao.MallWeatherLatestStaleScope{}
	partial := false
	realtimeFailed := weatherBundle.Realtime.Status != "ok"
	if realtimeFailed {
		partial = true
		staleLatest.DataKinds = append(staleLatest.DataKinds, model.MallWeatherDataKindRealtime)
	}
	minutely, err := caiyun.ParseMinutelyV26(weatherBundle)
	if err != nil || minutely == nil || minutely.Status != "ok" {
		partial = true
		staleLatest.DataKinds = append(staleLatest.DataKinds, model.MallWeatherDataKindMinutely)
		if err != nil || minutely == nil {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_PARSE_FAILED", Path: "result.minutely"})
		} else {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_STATUS_NOT_OK", Path: "result.minutely.status"})
		}
		minutely = nil
	}
	hourly, err := caiyun.ParseHourlyV26(weatherBundle)
	if err != nil || hourly == nil || hourly.Status != "ok" {
		partial = true
		staleLatest.DataKinds = append(staleLatest.DataKinds, model.MallWeatherDataKindHourly)
		if err != nil || hourly == nil {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_PARSE_FAILED", Path: "result.hourly"})
		} else {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_STATUS_NOT_OK", Path: "result.hourly.status"})
		}
		hourly = nil
	}
	daily, err := caiyun.ParseDailyV26(weatherBundle)
	if err != nil || daily == nil || daily.Status != "ok" {
		partial = true
		staleLatest.DataKinds = append(staleLatest.DataKinds, model.MallWeatherDataKindDaily)
		staleLatest.LifeSourceAPIs = append(staleLatest.LifeSourceAPIs, weatherdomain.SourceAPIV26Daily)
		if err != nil || daily == nil {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_PARSE_FAILED", Path: "result.daily"})
		} else {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_STATUS_NOT_OK", Path: "result.daily.status"})
		}
		daily = nil
	}
	alerts, err := caiyun.ParseAlertsV26(weatherBundle)
	if err != nil || alerts == nil || alerts.Status != "ok" || (alerts.RequestStatus != "" && alerts.RequestStatus != "ok") {
		partial = true
		if err != nil || alerts == nil {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_PARSE_FAILED", Path: "result.alert"})
		} else if alerts.Status != "ok" {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "MODULE_STATUS_NOT_OK", Path: "result.alert.status"})
		} else {
			moduleWarnings = append(moduleWarnings, caiyun.ParseWarning{Code: "REQUEST_STATUS_NOT_OK", Path: "result.alert.request_status"})
		}
		alerts = nil
	}

	forecastBatch, err := weatherdomain.MapForecasts(weatherdomain.ForecastMappingInput{
		Metadata: metadata, Weather: weatherBundle, Minutely: minutely, Hourly: hourly,
	})
	if err != nil {
		return nil, err
	}
	if realtimeFailed {
		forecastBatch.Realtime = nil
	}
	var dailyBatch *weatherdomain.DailyModelBatch
	if daily != nil {
		dailyBatch, err = weatherdomain.MapDaily(weatherdomain.DailyMappingInput{Metadata: metadata, Weather: weatherBundle, Daily: daily})
		if err != nil {
			return nil, err
		}
	}
	var alertBatch *weatherdomain.AlertModelBatch
	if alerts != nil {
		alertBatch, err = weatherdomain.MapAlerts(weatherdomain.AlertMappingInput{Metadata: metadata, Weather: weatherBundle, Alerts: alerts})
		if err != nil {
			return nil, err
		}
	}
	jsonPairs := []model.JSONText{forecastBatch.ParseWarningsJSON, forecastBatch.RowCountsJSON}
	if dailyBatch != nil {
		jsonPairs = append(jsonPairs, dailyBatch.ParseWarningsJSON, dailyBatch.RowCountsJSON)
	}
	if alertBatch != nil {
		jsonPairs = append(jsonPairs, alertBatch.ParseWarningsJSON, alertBatch.RowCountsJSON)
	}
	warnings, counts, err := aggregateBatchMetadata(jsonPairs...)
	if err != nil {
		return nil, err
	}
	warnings = deduplicateWarnings(append(warnings, moduleWarnings...))
	if realtimeFailed {
		counts[model.MallWeatherDataKindRealtime] = 0
	}
	status := weatherFetchStatusSuccess
	if partial || warningsRequirePartial(warnings) {
		status = weatherFetchStatusPartialSuccess
	}
	serverTime := weatherBundle.Metadata.ServerTimeUTC.UTC()
	batch, err := newMallWeatherModelBatch(endpointKind, status, weatherParserVersionV26, forecastBatch, dailyBatch, alertBatch, nil, warnings, counts, finishedAt, &serverTime)
	if err != nil {
		return nil, err
	}
	batch.StaleLatest = staleLatest
	return batch, nil
}

func newMallWeatherModelBatch(
	endpointKind, status, parserVersion string,
	forecasts *weatherdomain.ForecastModelBatch,
	daily *weatherdomain.DailyModelBatch,
	alerts *weatherdomain.AlertModelBatch,
	lifeIndices *weatherdomain.LifeIndexModelBatch,
	warnings []caiyun.ParseWarning,
	counts map[string]int,
	finishedAt time.Time,
	serverTimes ...*time.Time,
) (*mallWeatherModelBatch, error) {
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return nil, err
	}
	countsJSON, err := json.Marshal(counts)
	if err != nil {
		return nil, err
	}
	var serverTime *time.Time
	if len(serverTimes) > 0 {
		serverTime = serverTimes[0]
	}
	return &mallWeatherModelBatch{
		EndpointKind: endpointKind, Status: status, ParserVersion: parserVersion,
		ProviderServerTime: serverTime, Forecasts: forecasts, Daily: daily, Alerts: alerts, LifeIndices: lifeIndices,
		ParseWarningsJSON: model.JSONText(warningsJSON), RowCountsJSON: model.JSONText(countsJSON), FinishedAt: finishedAt,
	}, nil
}

func aggregateBatchMetadata(values ...model.JSONText) ([]caiyun.ParseWarning, map[string]int, error) {
	warnings := make([]caiyun.ParseWarning, 0)
	counts := make(map[string]int)
	for index := 0; index < len(values); index += 2 {
		if index+1 >= len(values) {
			return nil, nil, fmt.Errorf("mall weather: incomplete batch metadata")
		}
		var batchWarnings []caiyun.ParseWarning
		if err := json.Unmarshal([]byte(values[index]), &batchWarnings); err != nil {
			return nil, nil, err
		}
		var batchCounts map[string]int
		if err := json.Unmarshal([]byte(values[index+1]), &batchCounts); err != nil {
			return nil, nil, err
		}
		warnings = append(warnings, batchWarnings...)
		for kind, count := range batchCounts {
			counts[kind] += count
		}
	}
	return deduplicateWarnings(warnings), counts, nil
}

func deduplicateWarnings(warnings []caiyun.ParseWarning) []caiyun.ParseWarning {
	unique := make(map[string]caiyun.ParseWarning, len(warnings))
	keys := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		key := warning.Code + "\x00" + warning.Path
		if _, exists := unique[key]; exists {
			continue
		}
		unique[key] = warning
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]caiyun.ParseWarning, 0, len(keys))
	for _, key := range keys {
		result = append(result, unique[key])
	}
	return result
}

func warningsRequirePartial(warnings []caiyun.ParseWarning) bool {
	for _, warning := range warnings {
		if _, ok := partialWeatherWarningCodes[warning.Code]; ok {
			return true
		}
	}
	return false
}
