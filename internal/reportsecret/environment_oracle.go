package reportsecret

import appConfig "gin-biz-web-api/config"

const SystemOracleCredentialVersion = "system-environment-oracle"
const SystemOracleCredentialReference = "environment:REPORT_INPUT_ORACLE_PASSWORD"

func systemOraclePassword(purpose, reference string) (string, error) {
	if purpose != "report-datasource" || reference != SystemOracleCredentialReference {
		return "", credentialError("invalid system Oracle credential reference")
	}
	configured, err := appConfig.LoadReportInputOracleConfig()
	if err != nil || configured.Host == "" || configured.Username == "" || configured.Password == "" || (configured.ServiceName == "") == (configured.SID == "") {
		return "", credentialError("system Oracle environment is unavailable")
	}
	return configured.Password, nil
}
