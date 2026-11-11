package crontab

import (
	"context"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"
	"go.uber.org/zap"
	"sync/atomic"
	"time"
)

type KaiyuRegionCrontab struct{}

var kaiyuRegionRunning atomic.Bool

func (job KaiyuRegionCrontab) Run() { job.RunContext(context.Background()) }
func (KaiyuRegionCrontab) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil || !kaiyuRegionRunning.CompareAndSwap(false, true) {
		return
	}
	defer kaiyuRegionRunning.Store(false)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	start, end := data_svc.KaiyuRegionRollingRange(time.Now())
	_, result, err := data_svc.RunKaiyuRegionJob(ctx, data_svc.KaiyuRegionQuery{StartDate: start, EndDate: end}, true, true)
	fields := []zap.Field{zap.String("start_date", start), zap.String("end_date", end)}
	if result != nil {
		fields = append(fields, zap.Int("saved_days", result.SavedDays), zap.Int("empty_days", result.EmptyDays), zap.Int("failed_days", result.FailedDays))
	}
	if err != nil {
		logger.Error("开域区域日数据滚动刷新未全部完成", append(fields, zap.String("error", providerhttp.RedactText(err.Error())))...)
		return
	}
	logger.Info("开域区域日数据滚动刷新完成", fields...)
}
