package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	businessjob "gin-biz-web-api/job"
	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRegisteredTaskInvalidPayloadKeepsFinalAlert(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	task := asynq.NewTask(businessjob.TypeSalesSync, []byte(`{"secret":"payload-secret"`))
	err := businessjob.HandleSalesSyncTask(t.Context(), task)
	if !errors.Is(err, asynq.SkipRetry) || logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 {
		t.Fatalf("handler classified or logged malformed payload incorrectly: %v", err)
	}
	logTaskError(t.Context(), task, err)
	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zap.ErrorLevel || entries[0].Message != "Asynq task failed" {
		t.Fatalf("terminal task lost its final alert: %+v", entries)
	}
	fields := entries[0].ContextMap()
	if fields["retry_skipped"] != true || fields["task_type"] != businessjob.TypeSalesSync || strings.Contains(fmt.Sprint(fields), "payload-secret") {
		t.Fatalf("terminal alert lost classification or exposed payload: %v", fields)
	}
}

func TestRetryableTaskLoggingKeepsBusinessContextAtWarning(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	cause := errors.New("temporary database read failure")
	err := fmt.Errorf("sync sales shop_code=%q status=%q: %w", "YXHZR001", "70", cause)
	logTaskError(t.Context(), asynq.NewTask(businessjob.TypeSalesSync, nil), err)
	entries := logs.All()
	if len(entries) != 1 || entries[0].Level != zap.WarnLevel || entries[0].Message != "Asynq task will retry" {
		t.Fatalf("retryable task severity changed: %+v", entries)
	}
	fields := entries[0].ContextMap()
	for _, want := range []string{"YXHZR001", "70", cause.Error()} {
		if !strings.Contains(fmt.Sprint(fields["error"]), want) {
			t.Fatalf("retry log lost business context %q: %v", want, fields)
		}
	}
	if fields["retry_exhausted"] != false || fields["retry_skipped"] != false {
		t.Fatalf("retryable task became terminal: %v", fields)
	}
}

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
