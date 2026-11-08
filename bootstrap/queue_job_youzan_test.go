package bootstrap

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/youzan"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestYouzanDistributionHandlerReturnsWindowAndRetryHintWithoutDuplicateErrorLog(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path != "/token" {
			t.Errorf("token failure reached order API: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "text/html")
		writer.Header().Set("Retry-After", "120")
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(writer, "<html>private-gateway-body client_secret=test-secret</html>")
	}))
	defer server.Close()
	directory := t.TempDir()
	contents := "Youzan:\n  TokenURL: " + server.URL + "/token\n  ClientID: test-client\n  ClientSecret: test-secret\n  GrantID: test-grant\n"
	if err := os.WriteFile(filepath.Join(directory, "config.yaml"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	config.NewConfig("", directory+string(os.PathSeparator))
	core, logs := observer.New(zap.InfoLevel)
	previous := logger.Logger
	logger.Logger = zap.New(core)
	t.Cleanup(func() { logger.Logger = previous })
	task, err := job.NewYouzanDistributionOrderSyncTask(job.YouzanDistributionOrderSyncPayload{
		TimeFilter: youzan.OrderTimeFilterCreated, StartTime: "2026-11-07 00:00:00", EndTime: "2026-11-07 23:59:59",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = handleYouzanDistributionOrderSync(t.Context(), task)
	if err == nil {
		t.Fatal("token failure unexpectedly completed the task")
	}
	for _, expected := range []string{"time_filter=created", "start_time=2026-11-07 00:00:00", "end_time=2026-11-07 23:59:59", "http_status=503"} {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("returned error lost %q: %v", expected, err)
		}
	}
	var hint interface{ RetryDelay() time.Duration }
	if !errors.As(err, &hint) || hint.RetryDelay() != 2*time.Minute {
		t.Errorf("task wrapper lost token retry hint: %v", err)
	}
	if logs.FilterLevelExact(zap.ErrorLevel).Len() != 0 || requests.Load() != 1 {
		t.Errorf("handler duplicated retry alerts or requests: error logs=%d requests=%d", logs.FilterLevelExact(zap.ErrorLevel).Len(), requests.Load())
	}
	for _, secret := range []string{"private-gateway-body", "test-secret", "<html>"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("task error contains credential or response body %q", secret)
		}
	}
}
