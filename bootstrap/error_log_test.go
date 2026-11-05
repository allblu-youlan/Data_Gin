package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/pkg/logger"
)

type errorLogTestOracle struct {
	entries []reportoracle.ErrorLogEntry
	err     error
	closed  bool
}

func (connection *errorLogTestOracle) WriteErrorLog(_ context.Context, entry reportoracle.ErrorLogEntry) error {
	connection.entries = append(connection.entries, entry)
	return connection.err
}

func (connection *errorLogTestOracle) Close() error {
	connection.closed = true
	return nil
}

func TestOracleErrorLogWriterLazilyReusesSmallPoolAndMapsRecord(t *testing.T) {
	connection := &errorLogTestOracle{}
	opens := 0
	writer := &oracleErrorLogWriter{
		config: appConfig.ErrorLogConfig{Email: "alerts@example.com", Oracle: appConfig.ReportInputOracleConfig{
			Host: "oracle.internal", Port: 1521, ServiceName: "REPORT", Username: "reporter", Password: "secret",
		}},
		open: func(_ context.Context, configured reportoracle.Config) (oracleErrorLogConnection, error) {
			opens++
			if configured.Host != "oracle.internal" || configured.ServiceName != "REPORT" || configured.MaxOpenConnections != 1 || configured.MaxIdleConnections != 1 {
				t.Fatalf("incorrect Oracle error-log pool host=%s service=%s open=%d idle=%d", configured.Host, configured.ServiceName, configured.MaxOpenConnections, configured.MaxIdleConnections)
			}
			return connection, nil
		},
	}
	record := logger.ErrorRecord{Message: "订单推送失败", Caller: "service/orders.go:51", Details: `{"error":"远程接口超时"}`}
	for range 2 {
		if err := writer.write(t.Context(), record); err != nil {
			t.Fatal(err)
		}
	}
	if opens != 1 || len(connection.entries) != 2 || connection.closed {
		t.Fatalf("pool lifecycle opens=%d entries=%d closed=%t", opens, len(connection.entries), connection.closed)
	}
	entry := connection.entries[0]
	if entry.InterfaceDirect != "Data_Gin" || entry.KeyInfoNo != "go" || entry.ProInfo != "service/orders.go:51" || entry.Email != "alerts@example.com" || !strings.Contains(entry.ErrorInfo, "订单推送失败") || !strings.Contains(entry.ErrorInfo, "远程接口超时") {
		t.Fatalf("incorrect error entry: %+v", entry)
	}
}

func TestOracleErrorLogWriterReconnectsAfterFailure(t *testing.T) {
	want := errors.New("Oracle unavailable")
	broken := &errorLogTestOracle{err: want}
	healthy := &errorLogTestOracle{}
	opens := 0
	writer := &oracleErrorLogWriter{
		open: func(context.Context, reportoracle.Config) (oracleErrorLogConnection, error) {
			opens++
			switch opens {
			case 1:
				return nil, want
			case 2:
				return broken, nil
			default:
				return healthy, nil
			}
		},
	}
	for range 2 {
		if err := writer.write(t.Context(), logger.ErrorRecord{}); !errors.Is(err, want) {
			t.Fatalf("connection failure lost: %v", err)
		}
	}
	if !broken.closed {
		t.Fatal("failed Oracle connection remained open")
	}
	if err := writer.write(t.Context(), logger.ErrorRecord{}); err != nil || len(healthy.entries) != 1 || opens != 3 {
		t.Fatalf("recovery failed: error=%v entries=%d opens=%d", err, len(healthy.entries), opens)
	}
}

func TestOracleErrorLogSummariesFitByteLimitedColumns(t *testing.T) {
	connection := &errorLogTestOracle{}
	writer := &oracleErrorLogWriter{connection: connection}
	record := logger.ErrorRecord{Message: strings.Repeat("接口", 40), Caller: strings.Repeat("目录/", 30)}
	if err := writer.write(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	entry := connection.entries[0]
	if entry.KeyInfoNo != "go" || len(entry.ProInfo) > 80 || !utf8.ValidString(entry.ProInfo) {
		t.Fatalf("invalid column limits: key=%d pro=%d", len(entry.KeyInfoNo), len(entry.ProInfo))
	}
	if err := writer.write(t.Context(), logger.ErrorRecord{Message: "后台订单任务"}); err != nil {
		t.Fatal(err)
	}
	if connection.entries[1].ProInfo != "后台订单任务" {
		t.Fatalf("missing caller fallback: %q", connection.entries[1].ProInfo)
	}
}
