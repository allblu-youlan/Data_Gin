package reportsecret

import (
	"errors"
	appConfig "gin-biz-web-api/config"
	"strings"
	"testing"
)

func TestSystemOracleUsesExistingEnvironmentWithoutCopyingCredentials(t *testing.T) {
	t.Setenv(appConfig.EnvReportInputOracleHost, "oracle.example.test")
	t.Setenv(appConfig.EnvReportInputOracleUsername, "example_user")
	t.Setenv(appConfig.EnvReportInputOracleServiceName, "example_service")
	t.Setenv(appConfig.EnvReportInputOracleSID, "")
	t.Setenv(appConfig.EnvReportInputOraclePassword, "environment-only-secret")
	t.Setenv(appConfig.EnvReportInputQueriesJSON, "unrelated legacy query configuration")
	t.Setenv("REPORT_CREDENTIAL_KEYS_JSON", "")
	keyring := EnvironmentKeyring{}
	password, err := keyring.Decrypt(SystemOracleCredentialVersion, SystemOracleCredentialReference)
	if err != nil || password != "environment-only-secret" {
		t.Fatal("environment reference did not resolve the existing Oracle credential")
	}
	for _, request := range [][2]string{{"report-parameter", SystemOracleCredentialReference}, {"report-datasource", "other-environment-variable"}} {
		_, err := keyring.DecryptScoped(request[0], SystemOracleCredentialVersion, request[1])
		if !errors.Is(err, ErrInvalidCredential) || strings.Contains(err.Error(), password) {
			t.Fatalf("invalid credential reference was not safely rejected: %v", err)
		}
	}
}
