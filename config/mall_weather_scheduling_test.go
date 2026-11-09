package config

import (
	"testing"

	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestMallWeatherSchedulingConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, yaml, concurrencyEnv, spreadEnv string
		concurrency, spread                   int
	}{
		{name: "sequential staggered defaults", concurrency: 1, spread: 300},
		{name: "explicit yaml", yaml: "MallWeather:\n  WorkerConcurrency: 3\n  ScheduleSpreadSeconds: 120\n", concurrency: 3, spread: 120},
		{name: "environment overrides old concurrency", yaml: "MallWeather:\n  WorkerConcurrency: 20\n  ScheduleSpreadSeconds: 120\n", concurrencyEnv: "1", spreadEnv: "300", concurrency: 1, spread: 300},
		{name: "disable staggering explicitly", spreadEnv: "0", concurrency: 1, spread: 0},
		{name: "disable staggering in yaml", yaml: "MallWeather:\n  ScheduleSpreadSeconds: 0\n", concurrency: 1, spread: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("MALL_WEATHER_WORKER_CONCURRENCY", tt.concurrencyEnv)
			t.Setenv("MALL_WEATHER_SCHEDULE_SPREAD_SECONDS", tt.spreadEnv)
			loadReportCenterTestConfig(t, tt.yaml)
			if got := pkgConfig.GetInt("cfg.mall_weather.worker_concurrency"); got != tt.concurrency {
				t.Fatalf("worker concurrency=%d, want %d", got, tt.concurrency)
			}
			if got := pkgConfig.GetInt("cfg.mall_weather.schedule_spread_seconds"); got != tt.spread {
				t.Fatalf("schedule spread=%d, want %d", got, tt.spread)
			}
		})
	}
}
