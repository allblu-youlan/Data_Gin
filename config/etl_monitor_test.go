package config

import (
	"testing"
	"time"
)

func clearETLMonitorEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"ETL_MONITOR_BASE_URL", "ETL_MONITOR_BEARER_TOKEN", "ETL_MONITOR_USERNAME", "ETL_MONITOR_PASSWORD", "ETL_MONITOR_HEALTH_INTERVAL", "ETL_MONITOR_METRICS_INTERVAL", "ETL_MONITOR_HEALTH_TIMEOUT", "ETL_MONITOR_METRICS_TIMEOUT"} {
		t.Setenv(name, "")
	}
}

func TestLoadETLMonitorConfig(t *testing.T) {
	clearETLMonitorEnvironment(t)
	cfg, err := LoadETLMonitorConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "" || cfg.HealthInterval != 15*time.Second || cfg.MetricsInterval != 30*time.Second || cfg.HealthTimeout != 3*time.Second || cfg.MetricsTimeout != 5*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	t.Setenv("ETL_MONITOR_BASE_URL", " https://etl.example.com/proxy/ ")
	t.Setenv("ETL_MONITOR_METRICS_INTERVAL", "1m")
	t.Setenv("ETL_MONITOR_BEARER_TOKEN", "gateway-token")
	cfg, err = LoadETLMonitorConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://etl.example.com/proxy/" || cfg.MetricsInterval != time.Minute || cfg.BearerToken != "gateway-token" {
		t.Fatal("environment overrides were not loaded")
	}
}

func TestLoadETLMonitorConfigRejectsInvalidPolling(t *testing.T) {
	for _, value := range []string{"wrong", "0s", "-2s", "1ms"} {
		t.Run(value, func(t *testing.T) {
			clearETLMonitorEnvironment(t)
			t.Setenv("ETL_MONITOR_HEALTH_INTERVAL", value)
			if _, err := LoadETLMonitorConfig(); err == nil {
				t.Fatal("expected invalid polling duration to be rejected")
			}
		})
	}
}
