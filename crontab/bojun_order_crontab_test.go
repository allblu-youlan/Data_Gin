package crontab

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type bojunTestDeferredError struct{ delay time.Duration }

func (err bojunTestDeferredError) Error() string             { return "login cooling down" }
func (err bojunTestDeferredError) TaskDeferred() bool        { return true }
func (err bojunTestDeferredError) RetryDelay() time.Duration { return err.delay }

func TestBojunSyncErrorLoggingDistinguishesCooldownFromFailure(t *testing.T) {
	deferred := bojunTestDeferredError{delay: time.Minute}
	for _, test := range []struct {
		name      string
		err       error
		wantLevel zapcore.Level
		wantDelay float64
	}{
		{name: "wrapped cooldown", err: fmt.Errorf("order: %w", deferred), wantLevel: zap.InfoLevel, wantDelay: 60},
		{name: "multiple cooldowns", err: errors.Join(deferred, bojunTestDeferredError{delay: 30 * time.Second}), wantLevel: zap.InfoLevel, wantDelay: 30},
		{name: "actual gateway failure", err: errors.New("kerry login http_status=502"), wantLevel: zap.ErrorLevel},
		{name: "gateway failure and cooldown", err: errors.Join(deferred, errors.New("kerry login http_status=502")), wantLevel: zap.ErrorLevel},
		{name: "cooldown and lease failure", err: errors.Join(fmt.Errorf("order: %w", deferred), errors.New("lease lost")), wantLevel: zap.ErrorLevel},
	} {
		t.Run(test.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			previous := logger.Logger
			logger.Logger = zap.New(core)
			t.Cleanup(func() { logger.Logger = previous })
			result := &data_svc.BojunOrderSyncResult{WatermarkBefore: 10, WatermarkAfter: 10, SavedCount: 2}
			logBojunOrderSyncError(result, test.err)
			entries := logs.All()
			if len(entries) != 1 || entries[0].Level != test.wantLevel {
				t.Fatalf("logs=%+v, want level %s", entries, test.wantLevel)
			}
			fields := entries[0].ContextMap()
			if fields["watermark_after"] != uint64(10) || fields["saved_count"] != int64(2) {
				t.Errorf("missing partial progress: %v", fields)
			}
			if test.wantDelay > 0 && fields["retry_delay_seconds"] != test.wantDelay {
				t.Errorf("retry delay=%v, want %v", fields["retry_delay_seconds"], test.wantDelay)
			}
		})
	}
}
