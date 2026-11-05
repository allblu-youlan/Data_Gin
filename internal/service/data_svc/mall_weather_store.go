package data_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"gin-biz-web-api/connector/caiyun"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	weatherdomain "gin-biz-web-api/internal/weather"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/database"
	projectredis "gin-biz-web-api/pkg/redis"
	"gin-biz-web-api/pkg/storage"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormMallWeatherTaskStore struct {
	db                *gorm.DB
	alertMissingGrace time.Duration
}

type mallWeatherAlertRelationStore interface {
	FindAlertsByProviderIDs(context.Context, string, []string) ([]model.MallWeatherAlert, error)
	UpsertAlertRelations(context.Context, []model.MallWeatherAlertRelation) (data_dao.UpsertResult, error)
	DeactivateMissingAlertRelations(context.Context, uint, string, []uint, time.Time) (int64, error)
}

var _ mallWeatherTaskStore = (*gormMallWeatherTaskStore)(nil)
var _ mallWeatherAlertRelationStore = (*data_dao.MallWeatherDAO)(nil)

func NewMallWeatherProcessor() (*MallWeatherProcessor, error) {
	if database.DB == nil {
		return nil, fmt.Errorf("mall weather: database is unavailable")
	}
	requestBuilder, err := caiyun.NewRequestBuilder(
		config.GetString("cfg.caiyun.base_url"),
		global.Credentials.CaiyunAppKey(),
		global.Credentials.CaiyunAppSecret(),
	)
	if err != nil {
		return nil, fmt.Errorf("mall weather: create Caiyun request builder: %w", err)
	}
	fetchTimeout := time.Duration(config.GetInt("cfg.mall_weather.fetch_timeout_seconds")) * time.Second
	provider, err := caiyun.NewClientWithConfig(requestBuilder, nil, caiyun.ClientConfig{Timeout: fetchTimeout})
	if err != nil {
		return nil, fmt.Errorf("mall weather: create Caiyun client: %w", err)
	}
	objectStore, err := newWeatherSnapshotObjectStore()
	if err != nil {
		return nil, err
	}
	retentionDays := normalizeRawRetentionDays(config.GetInt("cfg.mall_weather.raw_retention_days"))
	weatherSnapshots, err := weatherdomain.NewRawSnapshotBuilder(weatherdomain.RawSnapshotConfig{
		RetentionDays: retentionDays, SchemaVersion: weatherParserVersionV26,
	}, objectStore)
	if err != nil {
		return nil, err
	}
	lifeSnapshots, err := weatherdomain.NewRawSnapshotBuilder(weatherdomain.RawSnapshotConfig{
		RetentionDays: retentionDays, SchemaVersion: weatherParserVersionLifeV3,
	}, objectStore)
	if err != nil {
		return nil, err
	}
	staleAfter := 3 * fetchTimeout
	if staleAfter < 2*time.Minute {
		staleAfter = 2 * time.Minute
	}
	redisInstance := projectredis.Instance()
	if redisInstance == nil || redisInstance.Client == nil {
		return nil, fmt.Errorf("mall weather: redis task lock is unavailable")
	}
	locker, err := weatherdomain.NewRedisTaskLocker(
		redisInstance.Client,
		projectredis.GenNamespace("lock:mall_weather:"),
		time.Duration(config.GetInt("cfg.mall_weather.lock_ttl_seconds"))*time.Second,
	)
	if err != nil {
		return nil, err
	}
	qps := config.GetFloat64("cfg.caiyun.qps")
	burst := int(math.Ceil(qps))
	if burst < 1 {
		burst = 1
	}
	limiter, err := weatherdomain.NewRedisTokenBucketLimiter(
		redisInstance.Client,
		projectredis.GenNamespace("rate:mall_weather:caiyun"),
		qps,
		burst,
	)
	if err != nil {
		return nil, err
	}
	circuitOpenTimeout := time.Duration(config.GetInt("cfg.mall_weather.circuit_open_seconds")) * time.Second
	breaker, err := weatherdomain.NewRedisCircuitBreaker(redisInstance.Client, weatherdomain.RedisCircuitBreakerConfig{
		Key:              projectredis.GenNamespace("circuit:mall_weather:caiyun"),
		FailureThreshold: config.GetInt("cfg.mall_weather.circuit_failure_threshold"),
		OpenTimeout:      circuitOpenTimeout,
		ProbeTTL:         time.Duration(config.GetInt("cfg.mall_weather.circuit_probe_ttl_seconds")) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	alertMissingGrace := time.Duration(config.GetInt("cfg.mall_weather.alert_missing_grace_seconds")) * time.Second
	return newMallWeatherProcessor(provider, &gormMallWeatherTaskStore{
		db: database.DB, alertMissingGrace: alertMissingGrace,
	}, weatherSnapshots, lifeSnapshots, locker, limiter, breaker, MallWeatherProcessorConfig{
		FastHourlySteps: 24, FastDailySteps: 1,
		FullHourlySteps: config.GetInt("cfg.mall_weather.hourly_steps"),
		FullDailySteps:  config.GetInt("cfg.mall_weather.daily_steps"),
		LifeIndexDays:   config.GetInt("cfg.mall_weather.daily_steps"),
		Unit:            config.GetString("cfg.mall_weather.unit"), AlertEnabled: config.GetBool("cfg.mall_weather.alert_enabled"),
		AttemptStaleAfter: staleAfter, FailureFinalizeTimeout: 3 * time.Second,
		LockReleaseTimeout: time.Duration(config.GetInt("cfg.mall_weather.lock_release_timeout_seconds")) * time.Second,
		CircuitOpenTimeout: circuitOpenTimeout,
	}, time.Now, mallWeatherRuntimeMetrics)
}

func newWeatherSnapshotObjectStore() (weatherdomain.SnapshotObjectStore, error) {
	if !storage.OSSStorageEnabled() {
		return nil, nil
	}
	client, err := storage.NewOSSClientFromConfig()
	if err != nil {
		return nil, fmt.Errorf("mall weather: create OSS snapshot client: %w", err)
	}
	ossConfig := storage.LoadOSSConfig()
	store, err := weatherdomain.NewOSSSnapshotStore(client, weatherdomain.OSSSnapshotStoreConfig{
		ObjectKeyPrefix: ossConfig.Prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("mall weather: create OSS snapshot store: %w", err)
	}
	return store, nil
}

func (store *gormMallWeatherTaskStore) Start(ctx context.Context, input mallWeatherTaskStart, startedAt time.Time) (*mallWeatherExecution, error) {
	if store == nil || store.db == nil || ctx == nil || input.Payload.MallID == 0 || startedAt.IsZero() {
		return nil, fmt.Errorf("mall weather: task store is not configured")
	}
	mall, err := data_dao.NewMallDAO(store.db).FindByID(ctx, input.Payload.MallID)
	if err != nil {
		if errors.Is(err, data_dao.ErrMallNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("mall weather: load mall for fetch: %w", err)
	}
	if mall == nil || !mall.WeatherEnabled || mall.Status != "active" {
		return nil, nil
	}
	run, _, err := data_dao.NewMallWeatherDAO(store.db).GetOrCreateFetchRun(ctx, &model.MallWeatherFetchRun{
		RunUUID: uuid.NewString(), MallID: mall.ID, TaskKind: input.TaskKind,
		TaskWindow: input.Payload.TaskWindow, EndpointKind: input.Payload.EndpointKind,
		Provider: weatherdomain.ProviderCaiyun, RequestedHourlySteps: input.RequestedHourlySteps,
		RequestedDailySteps: input.RequestedDailySteps, Status: "pending",
	})
	if err != nil {
		return nil, err
	}
	lease, err := data_dao.NewMallWeatherDAO(store.db).BeginFetchAttempt(ctx, run.ID, startedAt, input.AttemptStaleAfter)
	if err != nil {
		return nil, err
	}
	return &mallWeatherExecution{
		Mall: *mall, Run: lease.Run, Attempt: lease.Attempt, Disposition: lease.Disposition,
	}, nil
}

func (store *gormMallWeatherTaskStore) RecordResponse(ctx context.Context, execution *mallWeatherExecution, response *caiyun.ProviderResponse, snapshot *model.ProviderRawSnapshot) error {
	if err := validateWeatherStoreResponse(store, ctx, execution, response, snapshot); err != nil {
		return err
	}
	if err := data_dao.NewMallWeatherDAO(store.db).CreateRawSnapshot(ctx, snapshot); err != nil {
		return err
	}
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, _, err := lockMallWeatherExecution(tx, execution); err != nil {
			return err
		}
		httpStatus := response.HTTPStatus
		runUpdates := map[string]interface{}{
			"http_status": &httpStatus, "provider_status": response.ProviderStatus,
			"raw_snapshot_id": snapshot.ID, "response_checksum": snapshot.ResponseChecksum,
		}
		attemptUpdates := map[string]interface{}{
			"http_status": &httpStatus, "provider_status": response.ProviderStatus,
			"raw_snapshot_id": snapshot.ID, "response_checksum": snapshot.ResponseChecksum,
		}
		dao := data_dao.NewMallWeatherDAO(tx)
		if err := dao.UpdateFetchAttempt(ctx, execution.Attempt.ID, attemptUpdates); err != nil {
			return err
		}
		return dao.UpdateFetchRun(ctx, execution.Run.ID, runUpdates)
	})
	if err != nil {
		return err
	}
	execution.Run.RawSnapshotID = &snapshot.ID
	execution.Run.ResponseChecksum = snapshot.ResponseChecksum
	execution.Run.HTTPStatus = &response.HTTPStatus
	execution.Run.ProviderStatus = response.ProviderStatus
	execution.Attempt.RawSnapshotID = &snapshot.ID
	execution.Attempt.ResponseChecksum = snapshot.ResponseChecksum
	execution.Attempt.HTTPStatus = &response.HTTPStatus
	execution.Attempt.ProviderStatus = response.ProviderStatus
	return nil
}

func (store *gormMallWeatherTaskStore) Fail(ctx context.Context, execution *mallWeatherExecution, failure mallWeatherFailure) error {
	if store == nil || store.db == nil || ctx == nil || execution == nil || execution.Run.ID == 0 || execution.Attempt.ID == 0 ||
		failure.FinishedAt.IsZero() || failure.AttemptStatus == "" || failure.ErrorClass == "" || failure.ErrorCode == "" || failure.ErrorMessageSafe == "" {
		return fmt.Errorf("mall weather: invalid fetch failure")
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, attempt, err := lockMallWeatherExecution(tx, execution)
		if err != nil {
			return err
		}
		attemptDuration := nonNegativeMilliseconds(failure.FinishedAt.Sub(attempt.StartedAt))
		runDuration := int64(0)
		if run.StartedAt != nil {
			runDuration = nonNegativeMilliseconds(failure.FinishedAt.Sub(*run.StartedAt))
		}
		attemptUpdates := map[string]interface{}{
			"status": failure.AttemptStatus, "finished_at": &failure.FinishedAt, "duration_ms": attemptDuration,
			"error_class": failure.ErrorClass, "error_code": failure.ErrorCode, "error_message_safe": failure.ErrorMessageSafe,
		}
		runUpdates := map[string]interface{}{
			"status": "failed", "finished_at": &failure.FinishedAt, "duration_ms": runDuration,
			"error_class": failure.ErrorClass, "error_code": failure.ErrorCode, "error_message_safe": failure.ErrorMessageSafe,
		}
		if failure.ParserVersion != "" {
			runUpdates["parser_version"] = failure.ParserVersion
		}
		if failure.ParseWarningsJSON != "" {
			runUpdates["parse_warnings_json"] = failure.ParseWarningsJSON
		}
		dao := data_dao.NewMallWeatherDAO(tx)
		if err := dao.UpdateFetchAttempt(ctx, attempt.ID, attemptUpdates); err != nil {
			return err
		}
		if err := dao.UpdateFetchRun(ctx, run.ID, runUpdates); err != nil {
			return err
		}
		if _, err := dao.MarkLatestStaleForEndpoint(ctx, execution.Mall.ID, execution.Run.EndpointKind, attempt.StartedAt); err != nil {
			return err
		}
		return data_dao.NewMallDAO(tx).AdvanceLastWeatherErrorAt(ctx, execution.Mall.ID, failure.FinishedAt)
	})
}

func (store *gormMallWeatherTaskStore) Persist(ctx context.Context, execution *mallWeatherExecution, batch *mallWeatherModelBatch) error {
	if store == nil || store.db == nil || ctx == nil || execution == nil || batch == nil ||
		(batch.Status != weatherFetchStatusSuccess && batch.Status != weatherFetchStatusPartialSuccess) ||
		batch.EndpointKind != execution.Run.EndpointKind || batch.FinishedAt.IsZero() ||
		store.alertMissingGrace < time.Minute || store.alertMissingGrace > 24*time.Hour {
		return fmt.Errorf("mall weather: invalid persistence batch")
	}
	return store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, attempt, err := lockMallWeatherExecution(tx, execution)
		if err != nil {
			return err
		}
		dao := data_dao.NewMallWeatherDAO(tx)
		checksumConflicts, err := persistWeatherModelRows(
			ctx,
			dao,
			execution.Mall.ID,
			attempt.StartedAt,
			batch,
			store.alertMissingGrace,
		)
		if err != nil {
			return err
		}
		if checksumConflicts > 0 {
			if err := addChecksumConflictWarning(batch, checksumConflicts); err != nil {
				return err
			}
		}
		attemptDuration := nonNegativeMilliseconds(batch.FinishedAt.Sub(attempt.StartedAt))
		runDuration := int64(0)
		if run.StartedAt != nil {
			runDuration = nonNegativeMilliseconds(batch.FinishedAt.Sub(*run.StartedAt))
		}
		attemptUpdates := map[string]interface{}{
			"status": batch.Status, "finished_at": &batch.FinishedAt, "duration_ms": attemptDuration,
			"error_class": "", "error_code": "", "error_message_safe": "",
		}
		runUpdates := map[string]interface{}{
			"status": batch.Status, "finished_at": &batch.FinishedAt, "duration_ms": runDuration,
			"row_counts_json": batch.RowCountsJSON, "parse_warnings_json": batch.ParseWarningsJSON,
			"parser_version": batch.ParserVersion, "error_class": "", "error_code": "", "error_message_safe": "",
		}
		if batch.ProviderServerTime != nil {
			runUpdates["provider_server_time"] = batch.ProviderServerTime
		}
		if err := dao.UpdateFetchAttempt(ctx, attempt.ID, attemptUpdates); err != nil {
			return err
		}
		if err := dao.UpdateFetchRun(ctx, run.ID, runUpdates); err != nil {
			return err
		}
		return data_dao.NewMallDAO(tx).AdvanceLastWeatherSuccessAt(ctx, execution.Mall.ID, batch.FinishedAt)
	})
}

func persistWeatherModelRows(
	ctx context.Context,
	dao *data_dao.MallWeatherDAO,
	mallID uint,
	staleObservedBefore time.Time,
	batch *mallWeatherModelBatch,
	alertMissingGrace time.Duration,
) (int64, error) {
	var checksumConflicts int64
	if batch.Forecasts != nil {
		if batch.Forecasts.Realtime != nil {
			result, err := dao.UpsertRealtime(ctx, []model.MallWeatherRealtime{*batch.Forecasts.Realtime})
			if err != nil {
				return 0, err
			}
			checksumConflicts += result.ChecksumConflicts
		}
		result, err := dao.UpsertMinutely(ctx, batch.Forecasts.Minutely)
		if err != nil {
			return 0, err
		}
		checksumConflicts += result.ChecksumConflicts
		result, err = dao.UpsertHourly(ctx, batch.Forecasts.Hourly)
		if err != nil {
			return 0, err
		}
		checksumConflicts += result.ChecksumConflicts
	}
	if batch.Daily != nil {
		result, err := dao.UpsertDaily(ctx, batch.Daily.Daily)
		if err != nil {
			return 0, err
		}
		checksumConflicts += result.ChecksumConflicts
		result, err = dao.UpsertLifeIndices(ctx, batch.Daily.LifeIndices)
		if err != nil {
			return 0, err
		}
		checksumConflicts += result.ChecksumConflicts
	}
	if batch.Alerts != nil {
		result, err := dao.UpsertAlerts(ctx, batch.Alerts.Alerts)
		if err != nil {
			return 0, err
		}
		checksumConflicts += result.ChecksumConflicts
		if err := persistWeatherAlertRelations(
			ctx,
			dao,
			mallID,
			batch.Alerts.Alerts,
			batch.FinishedAt,
			alertMissingGrace,
		); err != nil {
			return 0, err
		}
	}
	if batch.LifeIndices != nil {
		result, err := dao.UpsertLifeIndices(ctx, batch.LifeIndices.LifeIndices)
		if err != nil {
			return 0, err
		}
		checksumConflicts += result.ChecksumConflicts
	}
	if _, err := dao.RefreshLatest(ctx, weatherLatestSources(batch)); err != nil {
		return 0, err
	}
	if len(batch.StaleLatest.DataKinds)+len(batch.StaleLatest.LifeSourceAPIs) > 0 {
		if _, err := dao.MarkLatestStale(ctx, mallID, batch.StaleLatest, staleObservedBefore); err != nil {
			return 0, err
		}
	}
	return checksumConflicts, nil
}

func weatherLatestSources(batch *mallWeatherModelBatch) data_dao.MallWeatherLatestSources {
	var sources data_dao.MallWeatherLatestSources
	if batch == nil {
		return sources
	}
	if batch.Forecasts != nil {
		if batch.Forecasts.Realtime != nil {
			sources.Realtime = []model.MallWeatherRealtime{*batch.Forecasts.Realtime}
		}
		sources.Minutely = batch.Forecasts.Minutely
		sources.Hourly = batch.Forecasts.Hourly
	}
	if batch.Daily != nil {
		sources.Daily = batch.Daily.Daily
		sources.LifeIndices = append(sources.LifeIndices, batch.Daily.LifeIndices...)
	}
	if batch.LifeIndices != nil {
		sources.LifeIndices = append(sources.LifeIndices, batch.LifeIndices.LifeIndices...)
	}
	return sources
}

func addChecksumConflictWarning(batch *mallWeatherModelBatch, conflicts int64) error {
	if batch == nil || conflicts <= 0 {
		return fmt.Errorf("mall weather: invalid checksum conflict warning")
	}
	var warnings []caiyun.ParseWarning
	if err := json.Unmarshal([]byte(batch.ParseWarningsJSON), &warnings); err != nil {
		return fmt.Errorf("mall weather: decode persistence warnings: %w", err)
	}
	warnings = deduplicateWarnings(append(warnings, caiyun.ParseWarning{
		Code: "CHECKSUM_CONFLICT", Path: "persistence.raw_checksum",
	}))
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		return fmt.Errorf("mall weather: encode persistence warnings: %w", err)
	}
	var counts map[string]int64
	if err := json.Unmarshal([]byte(batch.RowCountsJSON), &counts); err != nil {
		return fmt.Errorf("mall weather: decode persistence row counts: %w", err)
	}
	if counts == nil {
		counts = make(map[string]int64)
	}
	counts["checksum_conflicts"] += conflicts
	countsJSON, err := json.Marshal(counts)
	if err != nil {
		return fmt.Errorf("mall weather: encode persistence row counts: %w", err)
	}
	batch.ParseWarningsJSON = model.JSONText(warningsJSON)
	batch.RowCountsJSON = model.JSONText(countsJSON)
	return nil
}

func persistWeatherAlertRelations(
	ctx context.Context,
	dao mallWeatherAlertRelationStore,
	mallID uint,
	alerts []model.MallWeatherAlert,
	seenAt time.Time,
	missingGrace time.Duration,
) error {
	if ctx == nil || dao == nil || mallID == 0 || seenAt.IsZero() ||
		missingGrace < time.Minute || missingGrace > 24*time.Hour {
		return fmt.Errorf("mall weather: invalid alert relation reconciliation")
	}
	missingBefore := seenAt.Add(-missingGrace)
	if len(alerts) == 0 {
		_, err := dao.DeactivateMissingAlertRelations(
			ctx,
			mallID,
			weatherdomain.ProviderCaiyun,
			nil,
			missingBefore,
		)
		return err
	}
	alertIDs := make([]string, len(alerts))
	for index := range alerts {
		alertIDs[index] = alerts[index].AlertID
	}
	stored, err := dao.FindAlertsByProviderIDs(ctx, weatherdomain.ProviderCaiyun, alertIDs)
	if err != nil {
		return err
	}
	if len(stored) != len(alertIDs) {
		return fmt.Errorf("mall weather: alert upsert identity mismatch")
	}
	relations := make([]model.MallWeatherAlertRelation, len(stored))
	seenAlertPKs := make([]uint, len(stored))
	for index := range stored {
		seenAlertPKs[index] = stored[index].ID
		relations[index] = model.MallWeatherAlertRelation{
			MallID: mallID, AlertPK: stored[index].ID, RelationReason: "provider_location",
			FirstSeenAt: seenAt, LastSeenAt: seenAt, IsActive: true,
		}
	}
	if _, err = dao.UpsertAlertRelations(ctx, relations); err != nil {
		return err
	}
	_, err = dao.DeactivateMissingAlertRelations(
		ctx,
		mallID,
		weatherdomain.ProviderCaiyun,
		seenAlertPKs,
		missingBefore,
	)
	return err
}

func lockMallWeatherExecution(tx *gorm.DB, execution *mallWeatherExecution) (*model.MallWeatherFetchRun, *model.MallWeatherFetchAttempt, error) {
	if tx == nil || execution == nil || execution.Run.ID == 0 || execution.Attempt.ID == 0 {
		return nil, nil, fmt.Errorf("mall weather: invalid execution fence")
	}
	var run model.MallWeatherFetchRun
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, execution.Run.ID).Error; err != nil {
		return nil, nil, fmt.Errorf("mall weather: lock active fetch run: %w", err)
	}
	var attempt model.MallWeatherFetchAttempt
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&attempt, execution.Attempt.ID).Error; err != nil {
		return nil, nil, fmt.Errorf("mall weather: lock active fetch attempt: %w", err)
	}
	if run.Status != "running" || run.AttemptCount != attempt.AttemptNo || attempt.Status != "running" ||
		attempt.FetchRunID != run.ID || run.ID != execution.Run.ID || attempt.ID != execution.Attempt.ID {
		return nil, nil, ErrMallWeatherAttemptSuperseded
	}
	return &run, &attempt, nil
}

func validateWeatherStoreResponse(store *gormMallWeatherTaskStore, ctx context.Context, execution *mallWeatherExecution, response *caiyun.ProviderResponse, snapshot *model.ProviderRawSnapshot) error {
	if store == nil || store.db == nil || ctx == nil || execution == nil || response == nil || snapshot == nil ||
		execution.Run.ID == 0 || execution.Attempt.ID == 0 || response.EndpointKind != execution.Run.EndpointKind ||
		snapshot.Provider != weatherdomain.ProviderCaiyun || snapshot.EndpointKind != response.EndpointKind ||
		snapshot.MallID == nil || *snapshot.MallID != execution.Mall.ID || snapshot.ResponseChecksum == "" {
		return fmt.Errorf("mall weather: invalid response persistence input")
	}
	return nil
}

func nonNegativeMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}
