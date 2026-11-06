package bootstrap

import (
	"context"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/controller/data_ctrl"
	"gin-biz-web-api/internal/etlmonitor"
	"gin-biz-web-api/internal/middleware"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/routers"

	"github.com/gin-gonic/gin"
)

// setupETLMonitor isolates monitoring failures from application startup and requests.
func setupETLMonitor(router *gin.Engine) func() {
	cfg, err := appConfig.LoadETLMonitorConfig()
	var monitor *etlmonitor.Monitor
	if err == nil {
		monitor, err = etlmonitor.New(cfg)
	}
	configError := ""
	if err != nil {
		configError = err.Error()
		logger.WarnString("ETLMonitor", "initialize", configError)
		monitor, _ = etlmonitor.New(etlmonitor.Config{})
	}
	snapshot := func() etlmonitor.Snapshot {
		result := monitor.Snapshot()
		result.ConfigError = configError
		return result
	}
	api := router.Group("/api", middleware.RequireDatabaseAvailability())
	routers.RegisterETLMonitorRoutes(api, data_ctrl.NewETLMonitorController(snapshot))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		monitor.Run(ctx)
	}()
	return func() { cancel(); <-done }
}
