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
	end := data_svc.KaiyuPreviousHour(time.Now()).Add(time.Hour)
	missing, result, err := data_svc.RunKaiyuPassengerGapCheck(ctx, end.Add(-48*time.Hour), end, true)
	fields := []zap.Field{zap.Time("end_hour", end)}
	if missing != nil {
		fields = append(fields, zap.Int("missing_hours", missing.MissingHours), zap.Int("unbound", missing.UnboundMalls))
	}
	if result != nil {
		fields = append(fields, zap.Int("saved_hours", result.SavedHours), zap.Int("source_missing_hours", result.SourceMissingHours), zap.Int("failed_hours", result.FailedHours), zap.Int("remaining_hours", result.RemainingHours))
	}
	if err != nil {
		logger.Error("开域小时客流查缺补拉未全部完成", append(fields, zap.String("error", providerhttp.RedactText(err.Error())))...)
		return
	}
	logger.Info("开域最近48小时客流查缺补拉完成", fields...)
}
