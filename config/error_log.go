package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	EnvErrorLogEmail   = "ERROR_LOG_EMAIL"
	EnvErrorLogEnabled = "ERROR_LOG_ENABLED"
)

type ErrorLogConfig struct {
	Enabled bool
	Email   string
	Oracle  ReportInputOracleConfig
}

// LoadErrorLogConfig enables Oracle error logging when a recipient is configured.
func LoadErrorLogConfig() (ErrorLogConfig, error) {
	configured := ErrorLogConfig{Email: strings.TrimSpace(os.Getenv(EnvErrorLogEmail))}
	configured.Enabled = configured.Email != ""
	if raw := strings.TrimSpace(os.Getenv(EnvErrorLogEnabled)); raw != "" {
		enabled, err := strconv.ParseBool(raw)
		if err != nil {
			return ErrorLogConfig{}, fmt.Errorf("%s must be a boolean", EnvErrorLogEnabled)
		}
		configured.Enabled = enabled
	}
	if !configured.Enabled {
		return configured, nil
	}
	if configured.Email == "" || len(configured.Email) > 200 {
		return ErrorLogConfig{}, fmt.Errorf("%s must contain 1 to 200 bytes", EnvErrorLogEmail)
	}
	oracle, err := LoadReportInputOracleConfig()
	if err != nil {
		return ErrorLogConfig{}, fmt.Errorf("load error log Oracle configuration: %w", err)
	}
	if oracle.Host == "" || oracle.Username == "" || oracle.Password == "" || (oracle.ServiceName == "") == (oracle.SID == "") {
		return ErrorLogConfig{}, fmt.Errorf("error log Oracle host, username, password and exactly one service name or SID are required")
	}
	configured.Oracle = oracle
	return configured, nil
}
