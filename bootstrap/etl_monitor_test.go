package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	pkgConfig "gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestETLMonitorConfigFailureDoesNotPreventServerStartup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("App:\n  Env: local\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))
	t.Setenv("ETL_MONITOR_HEALTH_INTERVAL", "invalid")
	previousLogger := logger.Logger
	logger.Logger = zap.NewNop()
	t.Cleanup(func() { logger.Logger = previousLogger })
	router := gin.New()
	stop := setupETLMonitor(router)
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unconfigured monitor did not stop")
	}
	found := false
	for _, route := range router.Routes() {
		if route.Method == http.MethodGet && route.Path == "/api/v1/etl-monitor" {
			found = true
		}
	}
	if !found {
		t.Fatal("configuration failure hid the monitoring route")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/etl-monitor", nil))
	if recorder.Code == http.StatusOK {
		t.Fatal("monitoring endpoint allowed anonymous access")
	}
}
