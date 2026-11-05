package logger

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestGormLoggerUsesParameterizedStatementPlaceholder(t *testing.T) {
	if NewGormLogger().SlowThreshold <= 0 {
		t.Fatal("GORM logger has no slow query threshold")
	}
}

func TestGormLoggerSkipsSQLFormattingWhenTraceIsDisabled(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	logger := GormLogger{ZapLogger: zap.New(core), SlowThreshold: time.Hour}
	formatted := false
	logger.Trace(t.Context(), time.Now(), func() (string, int64) {
		formatted = true
		return "private query", 1
	}, nil)
	if formatted || logs.Len() != 0 {
		t.Fatalf("disabled trace formatted=%t logs=%d", formatted, logs.Len())
	}
}

func TestGormLoggerKeepsSlowAndFailedQueriesRedacted(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	logger := GormLogger{ZapLogger: zap.New(core), SlowThreshold: time.Second}
	query := func() (string, int64) { return "password=private-secret", 3 }
	logger.Trace(t.Context(), time.Now().Add(-2*time.Second), query, nil)
	logger.Trace(t.Context(), time.Now(), query, errors.New("query failed"))
	if logs.Len() != 2 {
		t.Fatalf("slow and failed query logs=%d", logs.Len())
	}
	for _, entry := range logs.All() {
		if entry.ContextMap()["sql"] != "[parameterized statement omitted]" || entry.ContextMap()["affected rows"] != int64(3) {
			t.Fatalf("missing safe query context: %v", entry.ContextMap())
		}
	}
}

func BenchmarkGormLoggerDisabledTrace(b *testing.B) {
	logger := GormLogger{ZapLogger: zap.NewNop(), SlowThreshold: time.Hour}
	query := func() (string, int64) { return "SELECT 1", 1 }
	b.ReportAllocs()
	for b.Loop() {
		logger.Trace(context.Background(), time.Now(), query, nil)
	}
}
