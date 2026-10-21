package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	EnvReportInputOracleHost                  = "REPORT_INPUT_ORACLE_HOST"
	EnvReportInputOraclePort                  = "REPORT_INPUT_ORACLE_PORT"
	EnvReportInputOracleServiceName           = "REPORT_INPUT_ORACLE_SERVICE_NAME"
	EnvReportInputOracleSID                   = "REPORT_INPUT_ORACLE_SID"
	EnvReportInputOracleUsername              = "REPORT_INPUT_ORACLE_USERNAME"
	EnvReportInputOraclePassword              = "REPORT_INPUT_ORACLE_PASSWORD"
	EnvReportInputOracleTimezone              = "REPORT_INPUT_ORACLE_TIMEZONE"
	EnvReportInputOracleConnectTimeoutSeconds = "REPORT_INPUT_ORACLE_CONNECT_TIMEOUT_SECONDS"
	EnvReportInputOracleQueryTimeoutSeconds   = "REPORT_INPUT_ORACLE_QUERY_TIMEOUT_SECONDS"
	EnvReportInputOracleMaxOpenConnections    = "REPORT_INPUT_ORACLE_MAX_OPEN_CONNECTIONS"
	EnvReportInputOracleMaxIdleConnections    = "REPORT_INPUT_ORACLE_MAX_IDLE_CONNECTIONS"
	EnvReportInputOracleConnectionLifetime    = "REPORT_INPUT_ORACLE_CONNECTION_LIFETIME_SECONDS"
	EnvReportInputOracleConnectionIdleTime    = "REPORT_INPUT_ORACLE_CONNECTION_IDLE_SECONDS"
	EnvReportInputOraclePrefetchRows          = "REPORT_INPUT_ORACLE_PREFETCH_ROWS"
	EnvReportInputOracleArraySize             = "REPORT_INPUT_ORACLE_ARRAY_SIZE"
	EnvReportInputQueriesJSON                 = "REPORT_INPUT_QUERIES_JSON"
)

var reportInputQueryNamePattern = regexp.MustCompile(`^\p{L}[\p{L}\p{N}_-]{0,63}$`)

type ReportInputOracleConfig struct {
	Host               string
	Port               int
	ServiceName        string
	SID                string
	Username           string
	Password           string
	Timezone           string
	ConnectTimeout     time.Duration
	QueryTimeout       time.Duration
	MaxOpenConnections int
	MaxIdleConnections int
	ConnectionLifetime time.Duration
	ConnectionIdleTime time.Duration
	PrefetchRows       int
	ArraySize          int
}

type ReportInputQuery struct {
	Name   string
	Select string
}

type ReportInputQueryConfig struct {
	Oracle  ReportInputOracleConfig
	Queries map[string]ReportInputQuery
}

// LoadReportInputQueryConfig reads the default Oracle datasource and legacy
// named queries from environment variables. New definitions are persisted in
// MySQL through authenticated report-management APIs.
func LoadReportInputQueryConfig() (ReportInputQueryConfig, error) {
	oracle, err := loadReportInputOracleConfig()
	if err != nil {
		return ReportInputQueryConfig{}, err
	}
	queries, err := loadReportInputQueries()
	if err != nil {
		return ReportInputQueryConfig{}, err
	}
	if len(queries) > 0 {
		if oracle.Host == "" || oracle.Username == "" || oracle.Password == "" || (oracle.ServiceName == "" && oracle.SID == "") {
			return ReportInputQueryConfig{}, fmt.Errorf("report input Oracle host, username, password and service name or SID are required")
		}
		if oracle.ServiceName != "" && oracle.SID != "" {
			return ReportInputQueryConfig{}, fmt.Errorf("report input Oracle service name and SID are mutually exclusive")
		}
	}
	return ReportInputQueryConfig{Oracle: oracle, Queries: queries}, nil
}

func loadReportInputOracleConfig() (ReportInputOracleConfig, error) {
	port, err := reportInputPositiveInt(EnvReportInputOraclePort, 1521)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	connectTimeout, err := reportInputPositiveInt(EnvReportInputOracleConnectTimeoutSeconds, 5)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	queryTimeout, err := reportInputPositiveInt(EnvReportInputOracleQueryTimeoutSeconds, 30)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	maxOpen, err := reportInputPositiveInt(EnvReportInputOracleMaxOpenConnections, 20)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	maxIdle, err := reportInputNonNegativeInt(EnvReportInputOracleMaxIdleConnections, 10)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	if maxIdle > maxOpen {
		return ReportInputOracleConfig{}, fmt.Errorf("%s must not exceed %s", EnvReportInputOracleMaxIdleConnections, EnvReportInputOracleMaxOpenConnections)
	}
	connectionLifetime, err := reportInputPositiveInt(EnvReportInputOracleConnectionLifetime, 1800)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	connectionIdleTime, err := reportInputPositiveInt(EnvReportInputOracleConnectionIdleTime, 300)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	prefetchRows, err := reportInputPositiveInt(EnvReportInputOraclePrefetchRows, 1000)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	arraySize, err := reportInputPositiveInt(EnvReportInputOracleArraySize, 1000)
	if err != nil {
		return ReportInputOracleConfig{}, err
	}
	return ReportInputOracleConfig{
		Host: strings.TrimSpace(os.Getenv(EnvReportInputOracleHost)), Port: port,
		ServiceName: strings.TrimSpace(os.Getenv(EnvReportInputOracleServiceName)), SID: strings.TrimSpace(os.Getenv(EnvReportInputOracleSID)),
		Username: strings.TrimSpace(os.Getenv(EnvReportInputOracleUsername)), Password: os.Getenv(EnvReportInputOraclePassword),
		Timezone:       strings.TrimSpace(os.Getenv(EnvReportInputOracleTimezone)),
		ConnectTimeout: time.Duration(connectTimeout) * time.Second, QueryTimeout: time.Duration(queryTimeout) * time.Second,
		MaxOpenConnections: maxOpen, MaxIdleConnections: maxIdle,
		ConnectionLifetime: time.Duration(connectionLifetime) * time.Second, ConnectionIdleTime: time.Duration(connectionIdleTime) * time.Second,
		PrefetchRows: prefetchRows, ArraySize: arraySize,
	}, nil
}

// LoadReportInputOracleConfig shares the system Oracle connection settings
// without requiring any named input queries to be configured.
func LoadReportInputOracleConfig() (ReportInputOracleConfig, error) {
	return loadReportInputOracleConfig()
}

func loadReportInputQueries() (map[string]ReportInputQuery, error) {
	raw := strings.TrimSpace(os.Getenv(EnvReportInputQueriesJSON))
	if raw == "" {
		return map[string]ReportInputQuery{}, nil
	}
	var configured map[string]string
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&configured); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object of query name to SELECT statement", EnvReportInputQueriesJSON)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("%s must contain exactly one JSON object", EnvReportInputQueriesJSON)
	}
	if len(configured) == 0 {
		return nil, fmt.Errorf("%s must not be empty when configured", EnvReportInputQueriesJSON)
	}
	queries := make(map[string]ReportInputQuery, len(configured))
	seen := make(map[string]struct{}, len(configured))
	for rawName, rawSelect := range configured {
		name := strings.TrimSpace(rawName)
		statement := strings.TrimSpace(rawSelect)
		lowerName := strings.ToLower(name)
		if !ValidateReportInputQueryName(name) {
			return nil, fmt.Errorf("%s contains an invalid query name", EnvReportInputQueriesJSON)
		}
		if _, exists := seen[lowerName]; exists {
			return nil, fmt.Errorf("%s contains duplicate query names", EnvReportInputQueriesJSON)
		}
		if !ValidateReportInputSelect(statement) {
			return nil, fmt.Errorf("%s query %q must contain one SELECT statement without comments or semicolons", EnvReportInputQueriesJSON, name)
		}
		seen[lowerName] = struct{}{}
		queries[name] = ReportInputQuery{Name: name, Select: statement}
	}
	return queries, nil
}

// ValidateReportInputQueryName applies the shared query-name rule used by
// environment configuration, managed definitions, and report input schemas.
func ValidateReportInputQueryName(name string) bool {
	return reportInputQueryNamePattern.MatchString(name)
}

// ValidateReportInputSelect applies the common safety boundary used by both
// legacy environment configuration and MySQL-managed query definitions.
func ValidateReportInputSelect(statement string) bool {
	lower := strings.ToLower(strings.TrimSpace(statement))
	return strings.HasPrefix(lower, "select ") && !strings.Contains(lower, ";") &&
		!strings.Contains(lower, "--") && !strings.Contains(lower, "/*") && !strings.Contains(lower, "*/")
}

func reportInputPositiveInt(name string, fallback int) (int, error) {
	value, err := reportInputNonNegativeInt(name, fallback)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}

func reportInputNonNegativeInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return value, nil
}
