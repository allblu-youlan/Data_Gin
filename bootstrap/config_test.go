package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appConfig "gin-biz-web-api/config"
	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestValidateMallWeatherConfig(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		enabledEnv   string
		repairEnv    string
		qpsEnv       string
		checkEnabled bool
		wantEnabled  bool
		checkQPS     bool
		wantQPS      float64
		checkSteps   bool
		wantHourly   int
		wantDaily    int
		wantError    string
	}{
		{
			name: "disabled by default",
			yaml: "App:\n  Env: local\n", checkSteps: true, wantHourly: 360, wantDaily: 15,
		},
		{
			name: "full forecast windows are preserved",
			yaml: "MallWeather:\n  HourlySteps: 360\n  DailySteps: 15\n", checkSteps: true, wantHourly: 360, wantDaily: 15,
		},
		{
			name:      "hourly forecast window above provider maximum is rejected",
			yaml:      "MallWeather:\n  Enabled: true\n  HourlySteps: 361\n  DailySteps: 15\nCaiyun:\n  QPS: 2\n",
			wantError: "hourly steps",
		},
		{
			name:      "daily forecast window above provider maximum is rejected",
			yaml:      "MallWeather:\n  Enabled: true\n  HourlySteps: 360\n  DailySteps: 16\nCaiyun:\n  QPS: 2\n",
			wantError: "daily steps",
		},
		{
			name:      "alert missing grace below minimum",
			yaml:      "MallWeather:\n  Enabled: true\n  AlertMissingGraceSeconds: 59\nCaiyun:\n  QPS: 2\n",
			wantError: "alert missing grace",
		},
		{
			name:      "alert missing grace above maximum",
			yaml:      "MallWeather:\n  Enabled: true\n  AlertMissingGraceSeconds: 86401\nCaiyun:\n  QPS: 2\n",
			wantError: "alert missing grace",
		},
		{
			name:      "enabled requires explicit qps",
			yaml:      "MallWeather:\n  Enabled: true\n",
			wantError: "qps",
		},
		{
			name: "enabled configuration is valid",
			yaml: "MallWeather:\n  Enabled: true\nCaiyun:\n  QPS: 2\n",
		},
		{
			name:       "environment enables workers",
			yaml:       "MallWeather:\n  Enabled: false\nCaiyun:\n  QPS: 2\n",
			enabledEnv: "true", checkEnabled: true, wantEnabled: true,
		},
		{
			name:   "environment configures caiyun qps",
			yaml:   "MallWeather:\n  Enabled: true\nCaiyun:\n  QPS: 0\n",
			qpsEnv: "2.5", checkQPS: true, wantQPS: 2.5,
		},
		{
			name:   "invalid caiyun qps environment fails closed",
			yaml:   "MallWeather:\n  Enabled: true\nCaiyun:\n  QPS: 2\n",
			qpsEnv: "fast", wantError: "CAIYUN_QPS must be a finite number",
		},
		{
			name:   "non-finite caiyun qps environment fails closed",
			yaml:   "MallWeather:\n  Enabled: true\nCaiyun:\n  QPS: 2\n",
			qpsEnv: "NaN", wantError: "CAIYUN_QPS must be a finite number",
		},
		{
			name:       "environment disables workers",
			yaml:       "MallWeather:\n  Enabled: true\n",
			enabledEnv: "false", checkEnabled: true, wantEnabled: false,
		},
		{
			name:       "invalid environment flag fails closed",
			yaml:       "App:\n  Env: local\n",
			enabledEnv: "sometimes", wantError: "MALL_WEATHER_ENABLED",
		},
		{
			name:      "invalid repair environment flag fails closed",
			yaml:      "App:\n  Env: local\n",
			repairEnv: "sometimes", wantError: "MALL_WEATHER_REPAIR_ENABLED",
		},
		{
			name:      "task timeout must exceed fetch timeout",
			yaml:      "MallWeather:\n  Enabled: true\n  FetchTimeoutSeconds: 30\n  TaskTimeoutSeconds: 30\nCaiyun:\n  QPS: 2\n",
			wantError: "task timeout",
		},
		{
			name:      "lock ttl must exceed task timeout",
			yaml:      "MallWeather:\n  Enabled: true\n  TaskTimeoutSeconds: 600\n  LockTTLSeconds: 600\nCaiyun:\n  QPS: 2\n",
			wantError: "lock ttl",
		},
		{
			name:      "repair rounds are bounded",
			yaml:      "MallWeather:\n  Enabled: true\n  RepairMaxRounds: 11\nCaiyun:\n  QPS: 2\n",
			wantError: "repair max rounds",
		},
		{
			name:      "repair spread is bounded",
			yaml:      "MallWeather:\n  Enabled: true\n  RepairSpreadSeconds: 3601\nCaiyun:\n  QPS: 2\n",
			wantError: "repair spread",
		},
		{
			name:      "repair query timeout is bounded",
			yaml:      "MallWeather:\n  Enabled: true\n  RepairQueryTimeoutSeconds: 301\nCaiyun:\n  QPS: 2\n",
			wantError: "repair query timeout",
		},
		{
			name:      "enabled requires weather queue consumer",
			yaml:      "MallWeather:\n  Enabled: true\nCaiyun:\n  QPS: 2\nQueueJob:\n  ConfigOpt:\n    Queues:\n      default: 1\n",
			wantError: "weather queue",
		},
		{
			name:      "enabled requires export queue consumer",
			yaml:      "MallWeather:\n  Enabled: true\nCaiyun:\n  QPS: 2\nQueueJob:\n  ConfigOpt:\n    Queues:\n      weather: 1\n",
			wantError: "export queue",
		},
		{
			name:      "feishu cannot run independently",
			yaml:      "MallWeather:\n  FeishuEnabled: true\n",
			wantError: "requires mall weather",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(appConfig.EnvMallWeatherEnabled, tt.enabledEnv)
			t.Setenv(appConfig.EnvMallWeatherRepairEnabled, tt.repairEnv)
			t.Setenv(appConfig.EnvCaiyunQPS, tt.qpsEnv)
			configDir := t.TempDir()
			configFile := filepath.Join(configDir, "config.yaml")
			if err := os.WriteFile(configFile, []byte(tt.yaml), 0o600); err != nil {
				t.Fatalf("write test config: %v", err)
			}
			pkgConfig.NewConfig("", configDir+string(os.PathSeparator))
			if tt.checkEnabled && pkgConfig.GetBool("cfg.mall_weather.enabled") != tt.wantEnabled {
				t.Fatalf("cfg.mall_weather.enabled = %t, want %t", pkgConfig.GetBool("cfg.mall_weather.enabled"), tt.wantEnabled)
			}
			if tt.checkQPS && pkgConfig.GetFloat64("cfg.caiyun.qps") != tt.wantQPS {
				t.Fatalf("cfg.caiyun.qps = %v, want %v", pkgConfig.GetFloat64("cfg.caiyun.qps"), tt.wantQPS)
			}
			if tt.checkSteps && (pkgConfig.GetInt("cfg.mall_weather.hourly_steps") != tt.wantHourly ||
				pkgConfig.GetInt("cfg.mall_weather.daily_steps") != tt.wantDaily) {
				t.Fatalf("weather steps = %d/%d, want %d/%d",
					pkgConfig.GetInt("cfg.mall_weather.hourly_steps"), pkgConfig.GetInt("cfg.mall_weather.daily_steps"),
					tt.wantHourly, tt.wantDaily)
			}

			err := validateMallWeatherConfig()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validateMallWeatherConfig() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("validateMallWeatherConfig() error = %v, want %q", err, tt.wantError)
			}
		})
	}
}
