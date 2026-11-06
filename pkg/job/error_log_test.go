package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDeferredTaskLoggingPreservesCooldown(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	task := asynq.NewTask("mall:weather:fast", nil)
	err := fmt.Errorf("weather: %w", deferredTaskError{deferred: true})
	logTaskError(context.Background(), task, err)
	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zap.InfoLevel || entries[0].Message != "Asynq task deferred" {
		t.Fatalf("cooldown logs = %+v, want one info deferral", entries)
	}
	if got := entries[0].ContextMap()["retry_delay_seconds"]; got != (61 * time.Second).Seconds() {
		t.Fatalf("retry_delay_seconds = %v, want 61", got)
	}
	logTaskError(context.Background(), task, errors.Join(err, asynq.SkipRetry))
	entries = logs.All()
	if len(entries) != 2 || entries[1].Level != zap.ErrorLevel || entries[1].Message != "Asynq task failed" {
		t.Fatalf("terminal cooldown logs = %+v, want failure", entries)
	}
}

func TestTaskErrorLoggingPreservesCauseWithoutTaskPayload(t *testing.T) {
	core, logs := observer.New(zap.ErrorLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	task := asynq.NewTask("report:run", []byte(`{"token":"payload-secret"}`))
	err := errors.Join(
		fmt.Errorf("generate inventory report: %w", errors.New("ORA-00942 missing table password=database-secret")),
		asynq.SkipRetry,
	)

	logTaskError(context.Background(), task, err)

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("error logs = %d, want 1", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["task_type"] != "report:run" || fields["retry_skipped"] != true {
		t.Fatalf("missing task metadata: %v", fields)
	}
	if fields["retry_exhausted"] != false {
		t.Fatalf("missing retry metadata was treated as exhausted: %v", fields)
	}
	for _, field := range []string{"task_id", "queue", "retry_count", "max_retry", "panic"} {
		if _, ok := fields[field]; !ok {
			t.Fatalf("missing task field %s: %v", field, fields)
		}
	}
	logged := fmt.Sprint(fields)
	if !strings.Contains(logged, "generate inventory report: ORA-00942") {
		t.Fatalf("lost wrapped cause: %s", logged)
	}
	for _, secret := range []string{"payload-secret", "database-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("task log leaked %q: %s", secret, logged)
		}
	}

	logTaskError(context.Background(), task, nil)
	if logs.Len() != 1 {
		t.Fatal("successful task emitted an error")
	}
	logTaskError(context.Background(), task, errors.New("temporary upstream failure"))
	if logs.Len() != 1 {
		t.Fatal("retryable task emitted a severe error")
	}
}
