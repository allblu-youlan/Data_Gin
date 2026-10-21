package reportrepo

import (
	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/internal/reportsecret"
	"testing"
	"time"
)

func TestSystemOracleDatasourceStoresAReferenceAndUsesSharedSettings(t *testing.T) {
	configured := appConfig.ReportInputOracleConfig{Host: "oracle.example.test", Port: 1521, ServiceName: "service", Username: "example_user", Password: "never-persist-this", ConnectTimeout: 5 * time.Second, QueryTimeout: 30 * time.Second, MaxOpenConnections: 20, MaxIdleConnections: 10, PrefetchRows: 1000, ArraySize: 1000}
	datasource := environmentDatasource(configured)
	if !systemEnvironmentDatasource(datasource) || !datasource.Enabled || datasource.PasswordCiphertext != reportsecret.SystemOracleCredentialReference || datasource.PasswordCiphertext == configured.Password {
		t.Fatal("system Oracle datasource must persist only an environment reference")
	}
	if datasource.Host != configured.Host || datasource.Username != configured.Username || datasource.SessionTimezone != "Asia/Shanghai" || datasource.QueryTimeoutSeconds != 30 || datasource.MaxOpenConnections != 20 {
		t.Fatal("system datasource did not reuse the existing Oracle configuration")
	}
	next := datasource
	next.PasswordCiphertext = ""
	if datasourceConnectionChanged(datasource, next) {
		t.Fatal("unchanged system configuration would invalidate the connection pool")
	}
}
