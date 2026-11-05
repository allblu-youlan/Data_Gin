package bootstrap

import (
	"context"
	"fmt"
	"sync"
	"unicode/utf8"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportoracle"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
)

type oracleErrorLogConnection interface {
	WriteErrorLog(context.Context, reportoracle.ErrorLogEntry) error
	Close() error
}

type oracleErrorLogWriter struct {
	mu         sync.Mutex
	config     appConfig.ErrorLogConfig
	open       func(context.Context, reportoracle.Config) (oracleErrorLogConnection, error)
	connection oracleErrorLogConnection
}

func setupErrorLog() {
	configured, err := appConfig.LoadErrorLogConfig()
	if err != nil {
		logger.Warn("Oracle error logging configuration", zap.Error(err))
		return
	}
	if !configured.Enabled {
		return
	}
	logger.AddCore(logger.NewErrorCore(newOracleErrorLogWriter(configured)))
}

func newOracleErrorLogWriter(configured appConfig.ErrorLogConfig) func(context.Context, logger.ErrorRecord) error {
	writer := &oracleErrorLogWriter{
		config: configured,
		open: func(ctx context.Context, configured reportoracle.Config) (oracleErrorLogConnection, error) {
			return reportoracle.Open(ctx, configured)
		},
	}
	return writer.write
}

func (writer *oracleErrorLogWriter) write(ctx context.Context, record logger.ErrorRecord) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if writer.connection == nil {
		configured := writer.config.Oracle
		connection, err := writer.open(ctx, reportoracle.Config{
			Host: configured.Host, Port: configured.Port, ServiceName: configured.ServiceName, SID: configured.SID,
			Username: configured.Username, Password: configured.Password, Timezone: configured.Timezone,
			ConnectTimeout: configured.ConnectTimeout, MaxOpenConnections: 1, MaxIdleConnections: 1,
			ConnectionLifetime: configured.ConnectionLifetime, ConnectionIdleTime: configured.ConnectionIdleTime,
		})
		if err != nil {
			return fmt.Errorf("open error log Oracle connection: %w", err)
		}
		writer.connection = connection
	}
	proInfo := record.Caller
	if proInfo == "" {
		proInfo = record.Message
	}
	entry := reportoracle.ErrorLogEntry{
		InterfaceDirect: "Data_Gin", KeyInfoNo: "go",
		ProInfo: errorLogSummary(proInfo), ErrorInfo: record.Format(), Email: writer.config.Email,
	}
	if err := writer.connection.WriteErrorLog(ctx, entry); err != nil {
		_ = writer.connection.Close()
		writer.connection = nil
		return err
	}
	return nil
}

func errorLogSummary(value string) string {
	if len(value) <= 80 {
		return value
	}
	end := 80
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end]
}
