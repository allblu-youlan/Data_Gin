package config

import (
	"strings"
	"testing"
)

func TestLoadErrorLogConfigEnablesEmailAndUsesSystemOracle(t *testing.T) {
	setReportInputOracleEnvironment(t)
	t.Setenv("ERROR_LOG_EMAIL", " alerts@example.com ")
	t.Setenv("ERROR_LOG_ENABLED", "")
	t.Setenv(EnvReportInputQueriesJSON, "invalid legacy query configuration")

	configured, err := LoadErrorLogConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !configured.Enabled || configured.Email != "alerts@example.com" || configured.Oracle.Host != "oracle.internal" || configured.Oracle.Username != "report_input" {
		t.Fatalf("unexpected error log configuration: enabled=%t email=%q host=%q", configured.Enabled, configured.Email, configured.Oracle.Host)
	}
}

func TestLoadErrorLogConfigDisabledDoesNotRequireOracle(t *testing.T) {
	for _, test := range []struct {
		name    string
		email   string
		enabled string
	}{
		{name: "unset"},
		{name: "explicitly disabled", email: "alerts@example.com", enabled: "false"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("ERROR_LOG_EMAIL", test.email)
			t.Setenv("ERROR_LOG_ENABLED", test.enabled)
			t.Setenv(EnvReportInputOraclePort, "invalid")
			configured, err := LoadErrorLogConfig()
			if err != nil || configured.Enabled {
				t.Fatalf("disabled error logging: enabled=%t error=%v", configured.Enabled, err)
			}
		})
	}
}

func TestLoadErrorLogConfigRejectsInvalidEnabledConfiguration(t *testing.T) {
	for _, test := range []struct {
		name    string
		email   string
		enabled string
		host    string
	}{
		{name: "missing email", enabled: "true", host: "oracle.internal"},
		{name: "long email", email: strings.Repeat("a", 201), host: "oracle.internal"},
		{name: "multibyte email", email: strings.Repeat("邮", 67), host: "oracle.internal"},
		{name: "invalid switch", email: "alerts@example.com", enabled: "invalid", host: "oracle.internal"},
		{name: "missing Oracle", email: "alerts@example.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setReportInputOracleEnvironment(t)
			t.Setenv("ERROR_LOG_EMAIL", test.email)
			t.Setenv("ERROR_LOG_ENABLED", test.enabled)
			t.Setenv(EnvReportInputOracleHost, test.host)
			if _, err := LoadErrorLogConfig(); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}
