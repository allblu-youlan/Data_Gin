package bootstrap

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	appConfig "gin-biz-web-api/config"
	"gin-biz-web-api/global"
	pkgConfig "gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/credential"

	"github.com/joho/godotenv"
)

// setupConfig 初始化配置文件信息
func setupConfig() {

	console.Info("init config ...")

	// 加载 .env 文件（如果存在）
	if err := godotenv.Load(); err != nil {
		console.Warning(".env file not found, using system environment variables")
	}

	// 通过匿名加载的方式自动加载了 config 包中所有的 init 函数

	// 加载配置文件
	pkgConfig.NewConfig(global.Env, strings.Split(global.ConfigPath, ",")...)

	if err := validateQueueRuntimeFlags(); err != nil {
		console.Exit("invalid queue runtime configuration: %v", err)
	}
	if err := validateMallWeatherConfig(); err != nil {
		console.Exit("invalid mall weather configuration: %v", err)
	}
	global.MallWeatherEnabledAtStartup = pkgConfig.GetBool("cfg.mall_weather.enabled")
	if err := validateReportCenterFlags(); err != nil {
		console.Exit("invalid report center configuration: %v", err)
	}
	global.ReportCenterEnabledAtStartup = pkgConfig.GetBool("cfg.report_center.enabled")

	credentials, err := credential.Load(credential.Requirements{
		Production:         pkgConfig.GetString("cfg.app.env") == "prod",
		RequireMallWeather: global.MallWeatherEnabledAtStartup,
		RequireFeishu:      pkgConfig.GetBool("cfg.mall_weather.feishu_enabled"),
	})
	if err != nil {
		console.Exit("invalid credential configuration: %v", err)
	}
	global.Credentials = credentials
	logCredentialStatus(credentials)

}

func validateQueueRuntimeFlags() error {
	for _, name := range []string{
		appConfig.EnvQueueWorkerEnabled,
		appConfig.EnvQueueSchedulerEnabled,
		appConfig.EnvCrontabEnabled,
	} {
		raw, exists := os.LookupEnv(name)
		if !exists || strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := strconv.ParseBool(strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("%s must be a boolean", name)
		}
	}
	return nil
}

func validateReportCenterFlags() error {
	for _, name := range []string{appConfig.EnvReportCenterEnabled, appConfig.EnvReportWorkerEnabled} {
		raw, exists := os.LookupEnv(name)
		if !exists || strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := strconv.ParseBool(strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("%s must be a boolean", name)
		}
	}
	return nil
}

func validateMallWeatherConfig() error {
	if raw, exists := os.LookupEnv(appConfig.EnvMallWeatherEnabled); exists && strings.TrimSpace(raw) != "" {
		if _, err := strconv.ParseBool(strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("%s must be a boolean", appConfig.EnvMallWeatherEnabled)
		}
	}
	if raw, exists := os.LookupEnv(appConfig.EnvMallWeatherRepairEnabled); exists && strings.TrimSpace(raw) != "" {
		if _, err := strconv.ParseBool(strings.TrimSpace(raw)); err != nil {
			return fmt.Errorf("%s must be a boolean", appConfig.EnvMallWeatherRepairEnabled)
		}
	}
	if raw, exists := os.LookupEnv(appConfig.EnvCaiyunQPS); exists && strings.TrimSpace(raw) != "" {
		qps, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil || math.IsNaN(qps) || math.IsInf(qps, 0) {
			return fmt.Errorf("%s must be a finite number", appConfig.EnvCaiyunQPS)
		}
	}
	if !pkgConfig.GetBool("cfg.mall_weather.enabled") {
		if pkgConfig.GetBool("cfg.mall_weather.feishu_enabled") {
			return fmt.Errorf("feishu integration requires mall weather to be enabled")
		}
		return nil
	}
	if pkgConfig.GetString("cfg.mall_weather.provider") != "caiyun" {
		return fmt.Errorf("provider must be caiyun")
	}
	if pkgConfig.GetString("cfg.mall_weather.unit") != "metric:v2" {
		return fmt.Errorf("unit must be metric:v2")
	}
	if qps := pkgConfig.GetFloat64("cfg.caiyun.qps"); qps <= 0 || qps > 1000 || math.IsNaN(qps) || math.IsInf(qps, 0) {
		return fmt.Errorf("caiyun qps must be greater than zero and at most 1000")
	}
	if hourlySteps := pkgConfig.GetInt("cfg.mall_weather.hourly_steps"); hourlySteps < 1 || hourlySteps > 360 {
		return fmt.Errorf("hourly steps must be between 1 and 360")
	}
	if dailySteps := pkgConfig.GetInt("cfg.mall_weather.daily_steps"); dailySteps < 1 || dailySteps > 15 {
		return fmt.Errorf("daily steps must be between 1 and 15")
	}
	if graceSeconds := pkgConfig.GetInt("cfg.mall_weather.alert_missing_grace_seconds"); graceSeconds < 60 || graceSeconds > 86400 {
		return fmt.Errorf("alert missing grace must be between 60 and 86400 seconds")
	}
	fetchTimeout := pkgConfig.GetInt("cfg.mall_weather.fetch_timeout_seconds")
	if fetchTimeout < 1 || fetchTimeout > 120 {
		return fmt.Errorf("fetch timeout must be between 1 and 120 seconds")
	}
	lockTTL := pkgConfig.GetInt("cfg.mall_weather.lock_ttl_seconds")
	taskTimeout := pkgConfig.GetInt("cfg.mall_weather.task_timeout_seconds")
	if taskTimeout <= fetchTimeout || taskTimeout > 1800 {
		return fmt.Errorf("weather task timeout must exceed fetch timeout and be at most 1800 seconds")
	}
	if lockTTL <= taskTimeout || lockTTL > 3600 {
		return fmt.Errorf("weather lock ttl must exceed task timeout and be at most 3600 seconds")
	}
	if releaseTimeout := pkgConfig.GetInt("cfg.mall_weather.lock_release_timeout_seconds"); releaseTimeout < 1 || releaseTimeout > 10 {
		return fmt.Errorf("weather lock release timeout must be between 1 and 10 seconds")
	}
	if threshold := pkgConfig.GetInt("cfg.mall_weather.circuit_failure_threshold"); threshold < 1 || threshold > 100 {
		return fmt.Errorf("weather circuit failure threshold must be between 1 and 100")
	}
	if openSeconds := pkgConfig.GetInt("cfg.mall_weather.circuit_open_seconds"); openSeconds < 1 || openSeconds > 3600 {
		return fmt.Errorf("weather circuit open seconds must be between 1 and 3600")
	}
	if probeSeconds := pkgConfig.GetInt("cfg.mall_weather.circuit_probe_ttl_seconds"); probeSeconds < 1 || probeSeconds > 120 {
		return fmt.Errorf("weather circuit probe ttl seconds must be between 1 and 120")
	}
	if repairRounds := pkgConfig.GetInt("cfg.mall_weather.repair_max_rounds"); repairRounds < 1 || repairRounds > 10 {
		return fmt.Errorf("weather repair max rounds must be between 1 and 10")
	}
	if repairSpread := pkgConfig.GetInt("cfg.mall_weather.repair_spread_seconds"); repairSpread < 0 || repairSpread > 3600 {
		return fmt.Errorf("weather repair spread must be between 0 and 3600 seconds")
	}
	if scheduleDBTimeout := pkgConfig.GetInt("cfg.mall_weather.schedule_db_timeout_seconds"); scheduleDBTimeout < 1 || scheduleDBTimeout > 60 {
		return fmt.Errorf("weather repair query timeout must be between 1 and 60 seconds")
	}
	queues, ok := pkgConfig.Get("cfg.queue_job.config_opt.queues").(map[string]int)
	if !ok || queues["weather"] <= 0 {
		return fmt.Errorf("weather queue must be configured with a positive weight")
	}
	if queues["export"] <= 0 {
		return fmt.Errorf("export queue must be configured with a positive weight")
	}
	return nil
}

func logCredentialStatus(credentials credential.Config) {
	for _, name := range []string{
		credential.EnvAmapWebServiceKey,
		credential.EnvCaiyunAppKey,
		credential.EnvCaiyunAppSecret,
		credential.EnvFeishuAppID,
		credential.EnvFeishuAppSecret,
	} {
		console.Info("credential %s configured=%t source=environment", name, credentials.Configured(name))
	}
}
