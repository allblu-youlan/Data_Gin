package logger

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
)

func TestErrorCoreRecordsOnlySeriousErrorsWithContext(t *testing.T) {
	var records []ErrorRecord
	core := NewErrorCore(func(ctx context.Context, record ErrorRecord) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("write has no deadline")
		}
		records = append(records, record)
		return nil
	}).(*errorCore)
	defer close(core.worker.queue)
	log := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zap.ErrorLevel)).With(zap.String("job_id", "job-1"))
	log.Info("ok")
	log.Warn("业务校验失败")
	log.Error("订单入库失败", zap.Error(errors.New("ORA-00942: table missing")), zap.String("password", "secret-value"), zap.Any("nested", map[string]string{"access_token": "token-value", "store": "S01"}))
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	record := records[0]
	for _, want := range []string{"job-1", "ORA-00942", "S01", "[REDACTED]"} {
		if !strings.Contains(record.Details, want) {
			t.Errorf("details missing %q: %s", want, record.Details)
		}
	}
	if strings.Contains(record.Details, "secret-value") || strings.Contains(record.Details, "token-value") {
		t.Fatal("credentials were retained")
	}
	if !strings.Contains(record.Caller, "error_core_test.go") || record.Stack == "" {
		t.Fatalf("missing caller/stack: %+v", record)
	}
}

func TestErrorCoreDoesNotBlockBusinessAndContinuesAfterFailure(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls int
	core := NewErrorCore(func(ctx context.Context, record ErrorRecord) error {
		calls++
		if calls == 1 {
			close(started)
			<-release
			return errors.New("Oracle unavailable")
		}
		return nil
	}).(*errorCore)
	defer close(core.worker.queue)
	log := zap.New(core)
	log.Error("first")
	<-started
	logged := make(chan struct{})
	go func() { log.Error("second"); close(logged) }()
	select {
	case <-logged:
	case <-time.After(time.Second):
		t.Fatal("business logging waited for Oracle")
	}
	close(release)
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}

func TestErrorCoreConcurrentLoggingAndSync(t *testing.T) {
	var count int
	core := NewErrorCore(func(context.Context, ErrorRecord) error { count++; return nil }).(*errorCore)
	defer close(core.worker.queue)
	log := zap.New(core)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); log.With(zap.String("worker", "test")).Error("failure") }()
	}
	workers.Wait()
	if err := log.Sync(); err != nil {
		t.Fatal(err)
	}
	if count != 20 {
		t.Fatalf("count=%d", count)
	}
}

func TestErrorRecordFormatFitsOracleWithoutSplittingUTF8(t *testing.T) {
	record := ErrorRecord{Time: time.Now(), Level: "error", Message: "同步订单失败", Caller: "service/orders.go:20", Details: strings.Repeat("数据库异常", 1000), Stack: strings.Repeat("堆栈", 1000)}
	formatted := record.Format()
	if len(formatted) > 4000 || !utf8.ValidString(formatted) {
		t.Fatalf("invalid format length=%d", len(formatted))
	}
	for _, want := range []string{"同步订单失败", "service/orders.go:20", "堆栈=", "[截断]"} {
		if !strings.Contains(formatted, want) {
			t.Errorf("missing %s", want)
		}
	}
}
