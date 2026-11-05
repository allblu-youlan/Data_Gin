package job

import (
	"strings"
	"testing"

	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestAsynqLoggerPreservesSeverityAndRedactsDetails(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core, zap.OnFatal(zapcore.WriteThenPanic))
	t.Cleanup(func() { logger.Logger = previous })
	adapter := asynqLogger{}
	adapter.Debug("polling queue")
	adapter.Info("worker started")
	adapter.Warn("temporary contention")
	adapter.Error("Redis connection failed password=redis-secret")
	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Error("fatal log did not invoke its configured termination hook")
			}
		}()
		adapter.Fatal("worker stopped token=fatal-secret")
	}()

	entries := logs.All()
	wantLevels := []zapcore.Level{zap.DebugLevel, zap.InfoLevel, zap.WarnLevel, zap.ErrorLevel, zap.FatalLevel}
	if len(entries) != len(wantLevels) {
		t.Fatalf("logs = %d, want %d", len(entries), len(wantLevels))
	}
	for index, entry := range entries {
		if entry.Level != wantLevels[index] {
			t.Fatalf("entry %d level = %s, want %s", index, entry.Level, wantLevels[index])
		}
		detail, _ := entry.ContextMap()["detail"].(string)
		if strings.Contains(detail, "redis-secret") || strings.Contains(detail, "fatal-secret") {
			t.Fatalf("Asynq logger leaked credentials: %s", detail)
		}
	}
	if !strings.Contains(entries[3].ContextMap()["detail"].(string), "Redis connection failed") {
		t.Fatal("Asynq logger lost the internal failure description")
	}
}
