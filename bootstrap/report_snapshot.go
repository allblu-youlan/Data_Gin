package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"
)

type reportSnapshotInitializer interface {
	InitializeJSONSnapshotStore(context.Context) error
	Close() error
}

func setupReportSnapshotStore() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ready, err := initializeSystemReportSnapshots(ctx, global.ReportCenterEnabledAtStartup, appConfig.LoadReportInputOracleConfig,
		func(ctx context.Context, config reportoracle.Config) (reportSnapshotInitializer, error) {
			return reportoracle.Open(ctx, config)
		})
	if err != nil {
		// A report storage outage must not prevent unrelated modules from starting.
		logger.WarnString("ReportSnapshot", "initialize", providerhttp.RedactText(fmt.Sprintf("报表共用结果存储初始化未完成，可执行 report-snapshot init 重试: %v", err)))
	} else if ready {
		logger.InfoString("ReportSnapshot", "initialize", "系统 Oracle 报表共用结果存储已就绪")
	}
}

// This creates only the two platform snapshot tables, using the existing
// idempotent installer. Source tables and existing objects are never altered.
func initializeSystemReportSnapshots(
	ctx context.Context,
	enabled bool,
	load func() (appConfig.ReportInputOracleConfig, error),
	open func(context.Context, reportoracle.Config) (reportSnapshotInitializer, error),
) (ready bool, resultErr error) {
	if !enabled {
		return false, nil
	}
	configured, err := load()
	if err != nil {
		return false, fmt.Errorf("load system Oracle settings: %w", err)
	}
	if configured.Host == "" && configured.Username == "" && configured.Password == "" && configured.ServiceName == "" && configured.SID == "" {
		return false, nil
	}
	if configured.Host == "" || configured.Username == "" || configured.Password == "" || (configured.ServiceName == "") == (configured.SID == "") {
		return false, fmt.Errorf("系统 Oracle 连接不完整，请检查现有 REPORT_INPUT_ORACLE_* 环境配置")
	}
	store, err := open(ctx, reportoracle.Config{
		Host: configured.Host, Port: configured.Port, ServiceName: configured.ServiceName, SID: configured.SID,
		Username: configured.Username, Password: configured.Password, Timezone: configured.Timezone,
		ConnectTimeout: configured.ConnectTimeout, MaxOpenConnections: 1, MaxIdleConnections: 1,
		ConnectionLifetime: configured.ConnectionLifetime, ConnectionIdleTime: configured.ConnectionIdleTime,
		PrefetchRows: configured.PrefetchRows, FetchArraySize: configured.ArraySize,
	})
	if err != nil {
		return false, fmt.Errorf("open system Oracle snapshot connection: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close system Oracle snapshot connection: %w", err))
		}
	}()
	if err := store.InitializeJSONSnapshotStore(ctx); err != nil {
		return false, err
	}
	return true, nil
}
