package data_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/pkg/kaiyu"
	"gin-biz-web-api/pkg/redis"
	"go.uber.org/zap"
)

// NewKaiyuRuntimeClient shares the provider budget with all configured Kaiyu jobs.
func NewKaiyuRuntimeClient() (*kaiyu.Client, error) {
	cfg, err := appConfig.LoadKaiyuConfig()
	if err != nil {
		return nil, err
	}
	instance := redis.Instance()
	if instance != nil && instance.Client != nil {
		limiter, err := kaiyu.NewRedisRequestLimiter(instance.Client, cfg.Client.SecurityID)
		if err != nil {
			return nil, err
		}
		cfg.Client.RequestLimiter = limiter
	} else {
		zap.L().Warn("开域请求未配置 Redis，使用本进程共享限流")
	}
	return kaiyu.NewClient(cfg.Client)
}
func newKaiyuRegionSource() (kaiyuRegionSource, error) { return NewKaiyuRuntimeClient() }

// KaiyuRegionRollingRange includes today and the previous three Shanghai dates.
func KaiyuRegionRollingRange(now time.Time) (string, string) {
	today := now.In(kaiyuLocation)
	return today.AddDate(0, 0, -3).Format(time.DateOnly), today.Format(time.DateOnly)
}

// RunKaiyuRegionJob is for trusted cron and CLI callers; HTTP must use actor-scoped methods.
func RunKaiyuRegionJob(ctx context.Context, input KaiyuRegionQuery, backfill, force bool) (missing *KaiyuRegionMissingResult, result *KaiyuRegionBackfillResult, resultErr error) {
	start, end, err := parseKaiyuRegionRange(input)
	if err != nil {
		return nil, nil, err
	}
	if backfill {
		if !kaiyuRegionSyncMu.TryLock() {
			return nil, nil, ErrKaiyuRegionBusy
		}
		defer kaiyuRegionSyncMu.Unlock()
	}
	store, err := OpenKaiyuOracle(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	service := NewKaiyuRegionService()
	malls, err := service.access.listScopedMalls(ctx, 0)
	if err != nil {
		return nil, nil, err
	}
	malls = filterKaiyuRegionMalls(malls, input.MallID)
	var ids []uint
	if input.MallID > 0 {
		ids = []uint{input.MallID}
	}
	missing, err = queryKaiyuRegionStatus(ctx, store, input, start, end, ids, malls)
	if err != nil {
		return nil, nil, err
	}
	result = &KaiyuRegionBackfillResult{Done: true}
	if !backfill {
		return missing, result, nil
	}
	source, err := newKaiyuRegionSource()
	if err != nil {
		return missing, result, err
	}
	request := KaiyuRegionBackfillRequest{KaiyuRegionQuery: input, Force: force}
	for {
		batch, err := fillKaiyuRegionBatch(ctx, source, store, missing.Items, request, time.Now())
		if batch != nil {
			result.ProcessedDays += batch.ProcessedDays
			result.SavedDays += batch.SavedDays
			result.EmptyDays += batch.EmptyDays
			result.FailedDays += batch.FailedDays
			result.SourceMissingDays += batch.SourceMissingDays
			result.Done = batch.Done
			result.NextCursor = batch.NextCursor
		}
		if err != nil {
			return missing, result, err
		}
		if batch.Done {
			break
		}
		request.Cursor = batch.NextCursor
	}
	missing, err = queryKaiyuRegionStatus(ctx, store, input, start, end, ids, malls)
	if err != nil {
		return missing, result, err
	}
	if result.FailedDays > 0 {
		return missing, result, fmt.Errorf("区域同步未全部完成：失败 %d 店日", result.FailedDays)
	}
	return missing, result, nil
}
