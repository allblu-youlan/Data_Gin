// Package bootstrap 处理程序初始化逻辑
package bootstrap

import (
	"fmt"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/storage"
)

func Initialize() {

	fmt.Println(console.Cyan(string(global.LogoContent)))
	console.Info("Initializing ...")

	// 初始化配置文件信息
	setupConfig()
	if err := validateReportCenterRuntime(); err != nil {
		console.Exit("invalid report center runtime configuration: %v", err)
	}

	// 初始化日志
	setupLogger()

	// 初始化数据库
	setupDB()

	// 初始化 redis
	setupRedis()

	// 初始化缓存 cache
	setupCache()

	// 启动 crontab
	setupCrontab()

	// 开启异步任务
	setupQueueJob()

	// 启动定时任务调度器
	setupScheduler()

}

func validateReportCenterRuntime() error {
	if !global.ReportCenterEnabledAtStartup {
		return nil
	}
	if !config.GetBool("cfg.queue_job.report_worker.enabled") {
		return fmt.Errorf("report worker must be enabled")
	}
	if config.GetInt("cfg.queue_job.report_worker.queue_weight") <= 0 {
		return fmt.Errorf("report worker queue weight must be positive")
	}
	if config.GetInt("cfg.queue_job.report_worker.run_concurrency") <= 0 {
		return fmt.Errorf("report run worker concurrency must be positive")
	}
	if config.GetInt("cfg.queue_job.report_worker.export_concurrency") <= 0 {
		return fmt.Errorf("report export worker concurrency must be positive")
	}
	if config.GetInt("cfg.queue_job.report_worker.cleanup_concurrency") <= 0 {
		return fmt.Errorf("report cleanup worker concurrency must be positive")
	}
	if err := (reportsecret.EnvironmentKeyring{}).Validate(); err != nil {
		return fmt.Errorf("oracle datasource credential keyring: %w", err)
	}
	if err := (reportsecret.EnvironmentParameterCipher{}).Validate(); err != nil {
		return fmt.Errorf("sensitive parameter keyring: %w", err)
	}
	if err := storage.ValidateOSSConfig(); err != nil {
		return fmt.Errorf("report export storage: %w", err)
	}
	if _, err := appConfig.LoadReportInputQueryConfig(); err != nil {
		return fmt.Errorf("report input query configuration: %w", err)
	}
	return nil
}

// InitializeMigration initializes only the dependencies needed by one-shot
// database migration commands. It deliberately avoids starting Redis-backed
// workers, cron jobs, queues, schedulers, and the service AutoMigrate path.
func InitializeMigration() {
	fmt.Println(console.Cyan(string(global.LogoContent)))
	console.Info("Initializing migration database ...")

	setupConfig()
	setupLogger()
	migrationIOTimeout, err := migrationDatabaseIOTimeout()
	if err != nil {
		console.Exit("invalid migration database configuration: %v", err)
	}
	setupDBConnectionWithIOTimeout(migrationIOTimeout)
}

func migrationDatabaseIOTimeout() (time.Duration, error) {
	seconds := config.GetInt("cfg.database.migration_io_timeout_seconds")
	if seconds < 60 || seconds > 24*60*60 {
		return 0, fmt.Errorf("migration I/O timeout seconds must be between 60 and 86400")
	}
	return time.Duration(seconds) * time.Second, nil
}
