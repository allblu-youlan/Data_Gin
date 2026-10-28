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

type KaiyuPassengerCrontab struct{}

var kaiyuPassengerRunning atomic.Bool

func (j KaiyuPassengerCrontab) Run() { j.RunContext(context.Background()) }
func (KaiyuPassengerCrontab) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil || !kaiyuPassengerRunning.CompareAndSwap(false, true) {
		return
	}
	defer kaiyuPassengerRunning.Store(false)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	result, err := data_svc.RunKaiyuPassengerSync(ctx, data_svc.KaiyuPreviousHour(time.Now()))
	fields := []zap.Field{zap.Time("hour_start", result.HourStart), zap.Int("saved", result.Saved), zap.Int("failed", result.Failed), zap.Int("unbound", result.Skipped)}
	if err != nil {
		logger.Error("开域小时客流同步未全部完成", append(fields, zap.String("error", providerhttp.RedactText(err.Error())))...)
		return
	}
	logger.Info("开域小时客流同步完成", fields...)
}
