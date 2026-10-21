package reportrepo

import (
	"context"
	"errors"
	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportsecret"
	"gin-biz-web-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SystemOracleDatasourceCode = "system_environment_oracle"

func systemEnvironmentDatasource(d model.ReportDatasource) bool {
	return d.Code == SystemOracleDatasourceCode && d.CredentialKeyVersion == reportsecret.SystemOracleCredentialVersion
}

func environmentDatasource(configured appConfig.ReportInputOracleConfig) model.ReportDatasource {
	timezone := configured.Timezone
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	return model.ReportDatasource{
		Code: SystemOracleDatasourceCode, Name: "系统环境 Oracle（默认）", Driver: model.ReportDatasourceDriverOracle,
		Host: configured.Host, Port: configured.Port, ServiceName: configured.ServiceName, SID: configured.SID, Username: configured.Username,
		CredentialKeyVersion: reportsecret.SystemOracleCredentialVersion, PasswordCiphertext: reportsecret.SystemOracleCredentialReference,
		SessionTimezone: timezone, ConnectTimeoutSeconds: int(configured.ConnectTimeout.Seconds()), QueryTimeoutSeconds: int(configured.QueryTimeout.Seconds()),
		MaxOpenConnections: configured.MaxOpenConnections, MaxIdleConnections: configured.MaxIdleConnections, PrefetchRows: configured.PrefetchRows, ArraySize: configured.ArraySize, Enabled: true,
	}
}

func (r *Repository) ensureEnvironmentDatasource(ctx context.Context) error {
	configured, err := appConfig.LoadReportInputOracleConfig()
	if err != nil || configured.Host == "" || configured.Username == "" || configured.Password == "" || (configured.ServiceName == "") == (configured.SID == "") {
		// Manually registered connections remain usable when the optional system connection is absent.
		return nil
	}
	next := environmentDatasource(configured)
	var current model.ReportDatasource
	err = r.db.WithContext(ctx).Where("code = ?", SystemOracleDatasourceCode).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&next).Error
	}
	if err != nil {
		return err
	}
	if !systemEnvironmentDatasource(current) {
		return nil
	}
	comparison := next
	comparison.PasswordCiphertext = ""
	if !datasourceConnectionChanged(current, comparison) && current.Name == next.Name && current.Enabled == next.Enabled {
		return nil
	}
	return r.db.WithContext(ctx).Model(&current).Updates(map[string]any{
		"name": next.Name, "host": next.Host, "port": next.Port, "service_name": next.ServiceName, "sid": next.SID, "username": next.Username,
		"session_timezone": next.SessionTimezone, "connect_timeout_seconds": next.ConnectTimeoutSeconds, "query_timeout_seconds": next.QueryTimeoutSeconds,
		"max_open_connections": next.MaxOpenConnections, "max_idle_connections": next.MaxIdleConnections, "prefetch_rows": next.PrefetchRows, "array_size": next.ArraySize, "enabled": true,
	}).Error
}
