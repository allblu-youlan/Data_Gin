package config

import (
	"os"
	"strconv"
	"strings"

	"gin-biz-web-api/pkg/config"
)

const (
	EnvMallWeatherEnabled       = "MALL_WEATHER_ENABLED"
	EnvMallWeatherRepairEnabled = "MALL_WEATHER_REPAIR_ENABLED"
)

func init() {
	config.Add("cfg.mall_weather", func() map[string]interface{} {
		return map[string]interface{}{
			"enabled":                         mallWeatherEnabled(),
			"repair_enabled":                  mallWeatherRepairEnabled(),
			"feishu_enabled":                  config.Get("MallWeather.FeishuEnabled", false),
			"provider":                        config.Get("MallWeather.Provider", "caiyun"),
			"coverage_radius_m":               config.Get("MallWeather.CoverageRadiusM", 1000),
			"sampling_mode":                   config.Get("MallWeather.SamplingMode", "center"),
			"default_detail_profile":          config.Get("MallWeather.DefaultDetailProfile", "full"),
			"fast_cron":                       config.Get("MallWeather.FastCron", "*/10 * * * *"),
			"full_cron":                       config.Get("MallWeather.FullCron", "7 * * * *"),
			"hourly_steps":                    config.GetInt("MallWeather.HourlySteps", 360),
			"daily_steps":                     config.GetInt("MallWeather.DailySteps", 15),
			"unit":                            config.Get("MallWeather.Unit", "metric:v2"),
			"alert_enabled":                   config.Get("MallWeather.AlertEnabled", true),
			"alert_missing_grace_seconds":     config.Get("MallWeather.AlertMissingGraceSeconds", 1800),
			"fetch_timeout_seconds":           config.Get("MallWeather.FetchTimeoutSeconds", 10),
			"max_attempts":                    config.Get("MallWeather.MaxAttempts", 3),
			"worker_concurrency":              config.Get("MallWeather.WorkerConcurrency", 20),
			"task_timeout_seconds":            config.Get("MallWeather.TaskTimeoutSeconds", 300),
			"lock_ttl_seconds":                config.Get("MallWeather.LockTTLSeconds", 600),
			"lock_release_timeout_seconds":    config.Get("MallWeather.LockReleaseTimeoutSeconds", 3),
			"circuit_failure_threshold":       config.Get("MallWeather.CircuitFailureThreshold", 5),
			"circuit_open_seconds":            config.Get("MallWeather.CircuitOpenSeconds", 60),
			"circuit_probe_ttl_seconds":       config.Get("MallWeather.CircuitProbeTTLSeconds", 10),
			"outbox_poll_interval_ms":         config.Get("MallWeather.OutboxPollIntervalMS", 1000),
			"outbox_lock_timeout_seconds":     config.Get("MallWeather.OutboxLockTimeoutSeconds", 60),
			"outbox_batch_size":               config.Get("MallWeather.OutboxBatchSize", 100),
			"outbox_retry_base_seconds":       config.Get("MallWeather.OutboxRetryBaseSeconds", 5),
			"outbox_retry_max_seconds":        config.Get("MallWeather.OutboxRetryMaxSeconds", 300),
			"repair_max_rounds":               config.Get("MallWeather.RepairMaxRounds", 3),
			"repair_spread_seconds":           config.Get("MallWeather.RepairSpreadSeconds", 900),
			"schedule_db_timeout_seconds":     config.Get("MallWeather.ScheduleDBTimeoutSeconds", config.Get("MallWeather.RepairQueryTimeoutSeconds", 8)),
			"raw_retention_days":              config.Get("MallWeather.RawRetentionDays", 30),
			"forecast_version_retention_days": config.Get("MallWeather.ForecastVersionRetentionDays", 30),
		}
	})
}

func mallWeatherEnabled() bool {
	fallback := config.GetBool("MallWeather.Enabled", false)
	raw, exists := os.LookupEnv(EnvMallWeatherEnabled)
	if !exists || strings.TrimSpace(raw) == "" {
		return fallback
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return enabled
}

func mallWeatherRepairEnabled() bool {
	fallback := config.GetBool("MallWeather.RepairEnabled", false)
	raw, exists := os.LookupEnv(EnvMallWeatherRepairEnabled)
	if !exists || strings.TrimSpace(raw) == "" {
		return fallback
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return enabled
}
