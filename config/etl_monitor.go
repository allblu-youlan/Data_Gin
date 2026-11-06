package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gin-biz-web-api/internal/etlmonitor"
)

// LoadETLMonitorConfig reads the monitoring target independently of ETL's deployment.
// An empty base URL disables polling. No network access is performed here.
func LoadETLMonitorConfig() (etlmonitor.Config, error) {
	cfg := etlmonitor.Config{
		BaseURL:     strings.TrimSpace(os.Getenv("ETL_MONITOR_BASE_URL")),
		BearerToken: strings.TrimSpace(os.Getenv("ETL_MONITOR_BEARER_TOKEN")),
		Username:    os.Getenv("ETL_MONITOR_USERNAME"),
		Password:    os.Getenv("ETL_MONITOR_PASSWORD"),
	}
	for _, setting := range []struct {
		name     string
		fallback time.Duration
		target   *time.Duration
	}{
		{"ETL_MONITOR_HEALTH_INTERVAL", 15 * time.Second, &cfg.HealthInterval},
		{"ETL_MONITOR_METRICS_INTERVAL", 30 * time.Second, &cfg.MetricsInterval},
		{"ETL_MONITOR_HEALTH_TIMEOUT", 3 * time.Second, &cfg.HealthTimeout},
		{"ETL_MONITOR_METRICS_TIMEOUT", 5 * time.Second, &cfg.MetricsTimeout},
	} {
		*setting.target = setting.fallback
		if raw := strings.TrimSpace(os.Getenv(setting.name)); raw != "" {
			value, err := time.ParseDuration(raw)
			if err != nil || value < time.Second || value > time.Hour {
				return etlmonitor.Config{}, fmt.Errorf("%s must be a duration between 1s and 1h", setting.name)
			}
			*setting.target = value
		}
	}
	return cfg, nil
}
