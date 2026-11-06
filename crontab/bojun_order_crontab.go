package crontab

import (
	"context"
	"sync/atomic"
	"time"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
)

type BojunOrderCrontab struct{}

var bojunOrderCronRunning atomic.Bool

func (b BojunOrderCrontab) Run() {
	b.RunContext(context.Background())
}

func (b BojunOrderCrontab) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil {
		return
	}
	if !bojunOrderCronRunning.CompareAndSwap(false, true) {
		logger.Warn("伯俊订单拉取仍在运行，跳过本次调度")
		return
	}
	defer bojunOrderCronRunning.Store(false)

	result, err := data_svc.NewBojunOrderSourceRouter().SyncRecentOrders(ctx)
	if err != nil {
		logBojunOrderSyncError(result, err)
		return
	}

	logger.Info(
		"伯俊订单拉取完成",
		zap.String("source_mode", result.SourceMode),
		zap.String("start_time", result.StartTime),
		zap.String("end_time", result.EndTime),
		zap.Int("fetch_pages", result.FetchPages),
		zap.Int("saved_count", result.SavedCount),
		zap.Int("retail_count", result.RetailCount),
		zap.Int("failed_count", result.FailedCount),
		zap.Uint64("watermark_before", result.WatermarkBefore),
		zap.Uint64("watermark_after", result.WatermarkAfter),
		zap.Bool("watermark_initialized", result.WatermarkInitialized),
		zap.Bool("lease_acquired", result.LeaseAcquired),
	)
}

func logBojunOrderSyncError(result *data_svc.BojunOrderSyncResult, err error) {
	fields := []zap.Field{zap.Error(err)}
	if result != nil {
		fields = append(fields,
			zap.String("source_mode", result.SourceMode),
			zap.Int("saved_count", result.SavedCount),
			zap.Int("failed_count", result.FailedCount),
			zap.Uint64("watermark_before", result.WatermarkBefore),
			zap.Uint64("watermark_after", result.WatermarkAfter),
		)
	}
	if delay, deferred := bojunSyncDeferredDelay(err); deferred {
		fields = append(fields, zap.Float64("retry_delay_seconds", delay.Seconds()))
		logger.Info("伯俊订单等待商场登录恢复", fields...)
		return
	}
	logger.Error("伯俊订单同步失败", fields...)
}

// A joined real failure must retain its severity even when another order is deferred.
func bojunSyncDeferredDelay(err error) (time.Duration, bool) {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var earliest time.Duration
		for _, child := range joined.Unwrap() {
			delay, deferred := bojunSyncDeferredDelay(child)
			if !deferred {
				return 0, false
			}
			if earliest == 0 || delay < earliest {
				earliest = delay
			}
		}
		return earliest, earliest > 0
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return bojunSyncDeferredDelay(wrapped.Unwrap())
	}
	deferred, ok := err.(interface {
		TaskDeferred() bool
		RetryDelay() time.Duration
	})
	if !ok || !deferred.TaskDeferred() {
		return 0, false
	}
	delay := deferred.RetryDelay()
	return delay, delay > 0
}
