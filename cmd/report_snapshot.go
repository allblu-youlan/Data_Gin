package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"

	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type reportSnapshotStore interface {
	ValidateJSONSnapshotStore(context.Context) error
	InitializeJSONSnapshotStore(context.Context) error
	Close() error
}

type reportSnapshotOpener func(context.Context, reportoracle.Config) (reportSnapshotStore, error)

var ReportSnapshotCmd = NewReportSnapshotCmd()

func NewReportSnapshotCmd() *cobra.Command {
	return newReportSnapshotCmd(loadSnapshotOracleConfig, func(ctx context.Context, config reportoracle.Config) (reportSnapshotStore, error) {
		return reportoracle.Open(ctx, config)
	})
}

func newReportSnapshotCmd(load func() (appConfig.ReportInputOracleConfig, error), open reportSnapshotOpener) *cobra.Command {
	var timeout time.Duration
	command := &cobra.Command{
		Use: "report-snapshot", Short: "检查或初始化平台共用的 Oracle 报表结果存储", Args: cobra.NoArgs,
		SilenceUsage: true, SilenceErrors: true,
		// This one-shot Oracle operation must not start the server, MySQL, or workers.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
	}
	command.PersistentFlags().DurationVar(&timeout, "timeout", 60*time.Second, "Oracle 操作总超时")
	for _, action := range []struct{ name, description string }{{"check", "检查系统 Oracle 中的报表快照表"}, {"init", "复用已有脚本补建缺少的共用快照表，已有表保留"}} {
		initialize := action.name == "init"
		command.AddCommand(&cobra.Command{
			Use: action.name, Short: action.description, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) (resultErr error) {
				if timeout <= 0 || timeout > 10*time.Minute {
					return fmt.Errorf("timeout 必须大于 0 且不超过 10 分钟")
				}
				configured, err := load()
				if err != nil {
					return err
				}
				if configured.Host == "" || configured.Username == "" || configured.Password == "" || (configured.ServiceName == "") == (configured.SID == "") {
					return fmt.Errorf("请配置现有 REPORT_INPUT_ORACLE_* 系统 Oracle 连接，并指定 SERVICE_NAME 或 SID 其中一个")
				}
				ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer stop()
				ctx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				store, err := open(ctx, snapshotOracleConfig(configured))
				if err != nil {
					return fmt.Errorf("连接系统 Oracle 失败: %w", err)
				}
				defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
				if initialize {
					err = store.InitializeJSONSnapshotStore(ctx)
				} else {
					err = store.ValidateJSONSnapshotStore(ctx)
				}
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "报表共用结果存储已就绪，可重新发布；无需逐报表配置结果表。")
				return err
			},
		})
	}
	return command
}

func loadSnapshotOracleConfig() (appConfig.ReportInputOracleConfig, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return appConfig.ReportInputOracleConfig{}, fmt.Errorf("读取环境配置失败: %w", err)
	}
	return appConfig.LoadReportInputOracleConfig()
}

func snapshotOracleConfig(config appConfig.ReportInputOracleConfig) reportoracle.Config {
	return reportoracle.Config{Host: config.Host, Port: config.Port, ServiceName: config.ServiceName, SID: config.SID, Username: config.Username, Password: config.Password, Timezone: config.Timezone, ConnectTimeout: config.ConnectTimeout, MaxOpenConnections: 1, MaxIdleConnections: 1, ConnectionLifetime: config.ConnectionLifetime, ConnectionIdleTime: config.ConnectionIdleTime, PrefetchRows: config.PrefetchRows, FetchArraySize: config.ArraySize}
}
