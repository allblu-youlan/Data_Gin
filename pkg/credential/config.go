// Package credential loads static authorization values directly from the
// process environment without exposing them through Viper or configuration
// backups.
package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	EnvAmapWebServiceKey             = "AMAP_WEB_SERVICE_KEY"
	EnvCaiyunAppKey                  = "CAIYUN_APP_KEY"
	EnvCaiyunAppSecret               = "CAIYUN_APP_SECRET"
	EnvFeishuAppID                   = "FEISHU_APP_ID"
	EnvFeishuAppSecret               = "FEISHU_APP_SECRET"
	EnvFeishuWeatherSpreadsheetToken = "FEISHU_WEATHER_SPREADSHEET_TOKEN"
	EnvFeishuWeatherRealtimeSheetID  = "FEISHU_WEATHER_REALTIME_SHEET_ID"
	EnvFeishuWeatherMinutelySheetID  = "FEISHU_WEATHER_MINUTELY_SHEET_ID"
	EnvFeishuWeatherHourlySheetID    = "FEISHU_WEATHER_HOURLY_SHEET_ID"
	EnvFeishuWeatherDailySheetID     = "FEISHU_WEATHER_DAILY_SHEET_ID"
	EnvFeishuWeatherAlertSheetID     = "FEISHU_WEATHER_ALERT_SHEET_ID"
	EnvFeishuWeatherLifeIndexSheetID = "FEISHU_WEATHER_LIFE_INDEX_SHEET_ID"
	EnvFeishuWeatherFolderToken      = "FEISHU_WEATHER_FOLDER_TOKEN"
)

// Requirements controls which optional integration credentials are mandatory.
type Requirements struct {
	Production         bool
	RequireMallWeather bool
	RequireFeishu      bool
	RequireFeishuApp   bool
}

// Config intentionally keeps every value private so encoding/json, YAML and
// fmt cannot accidentally serialize credentials. Callers receive only the
// specific value they need through named accessors.
type Config struct {
	values map[string]string
}

// String and GoString ensure diagnostic formatting cannot reveal private map
// contents through fmt's reflection fallback.
func (Config) String() string   { return "credential.Config{redacted}" }
func (Config) GoString() string { return "credential.Config{redacted}" }

// Load reads credentials from the process environment and validates the
// enabled integrations. Values are not trimmed or otherwise modified.
func Load(requirements Requirements) (Config, error) {
	required := requiredNames(requirements)
	values := make(map[string]string, len(allNames()))
	missing := make([]string, 0)
	invalid := make([]string, 0)

	for _, name := range allNames() {
		value, exists := os.LookupEnv(name)
		if exists {
			values[name] = value
		}
		if !required[name] {
			continue
		}
		if !exists || strings.TrimSpace(value) == "" {
			missing = append(missing, name)
			continue
		}
		if requirements.Production && isPlaceholder(value) {
			invalid = append(invalid, name)
		}
	}

	if len(missing) > 0 || len(invalid) > 0 {
		parts := make([]string, 0, 2)
		if len(missing) > 0 {
			parts = append(parts, "missing environment variables: "+strings.Join(missing, ", "))
		}
		if len(invalid) > 0 {
			parts = append(parts, "placeholder environment variables: "+strings.Join(invalid, ", "))
		}
		return Config{}, errors.New(strings.Join(parts, "; "))
	}

	return Config{values: values}, nil
}

func requiredNames(requirements Requirements) map[string]bool {
	required := make(map[string]bool)
	if requirements.RequireFeishuApp {
		required[EnvFeishuAppID] = true
		required[EnvFeishuAppSecret] = true
	}
	if requirements.RequireMallWeather {
		for _, name := range []string{EnvAmapWebServiceKey, EnvCaiyunAppKey, EnvCaiyunAppSecret} {
			required[name] = true
		}
	}
	if requirements.RequireFeishu {
		for _, name := range []string{
			EnvFeishuAppID,
			EnvFeishuAppSecret,
			EnvFeishuWeatherSpreadsheetToken,
			EnvFeishuWeatherRealtimeSheetID,
			EnvFeishuWeatherMinutelySheetID,
			EnvFeishuWeatherHourlySheetID,
			EnvFeishuWeatherDailySheetID,
			EnvFeishuWeatherAlertSheetID,
			EnvFeishuWeatherLifeIndexSheetID,
		} {
			required[name] = true
		}
	}
	return required
}

func allNames() []string {
	return []string{
		EnvAmapWebServiceKey,
		EnvCaiyunAppKey,
		EnvCaiyunAppSecret,
		EnvFeishuAppID,
		EnvFeishuAppSecret,
		EnvFeishuWeatherSpreadsheetToken,
		EnvFeishuWeatherRealtimeSheetID,
		EnvFeishuWeatherMinutelySheetID,
		EnvFeishuWeatherHourlySheetID,
		EnvFeishuWeatherDailySheetID,
		EnvFeishuWeatherAlertSheetID,
		EnvFeishuWeatherLifeIndexSheetID,
		EnvFeishuWeatherFolderToken,
	}
}

func isPlaceholder(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "changeme", "change-me", "example", "password", "secret", "test", "test-key", "your-key", "your-secret":
		return true
	default:
		return false
	}
}

// Configured reports whether a named, allowlisted environment variable exists
// and is non-empty. It never returns metadata about the value.
func (c Config) Configured(name string) bool {
	value, ok := c.values[name]
	return ok && strings.TrimSpace(value) != ""
}

func (c Config) value(name string) string { return c.values[name] }

func (c Config) AmapWebServiceKey() string { return c.value(EnvAmapWebServiceKey) }
func (c Config) CaiyunAppKey() string      { return c.value(EnvCaiyunAppKey) }
func (c Config) CaiyunAppSecret() string   { return c.value(EnvCaiyunAppSecret) }
func (c Config) FeishuAppID() string       { return c.value(EnvFeishuAppID) }
func (c Config) FeishuAppSecret() string   { return c.value(EnvFeishuAppSecret) }

// EnvironmentValue resolves only known resource-location variables. It is for
// Feishu destination references and cannot read arbitrary process variables.
func (c Config) EnvironmentValue(name string) (string, error) {
	if !isFeishuResourceName(name) {
		return "", fmt.Errorf("credential: environment variable %q is not allowlisted", name)
	}
	return c.value(name), nil
}

func isFeishuResourceName(name string) bool {
	switch name {
	case EnvFeishuWeatherSpreadsheetToken,
		EnvFeishuWeatherRealtimeSheetID,
		EnvFeishuWeatherMinutelySheetID,
		EnvFeishuWeatherHourlySheetID,
		EnvFeishuWeatherDailySheetID,
		EnvFeishuWeatherAlertSheetID,
		EnvFeishuWeatherLifeIndexSheetID,
		EnvFeishuWeatherFolderToken:
		return true
	default:
		return false
	}
}

// MarshalJSON prevents accidental serialization even if Config is embedded in
// another response type.
func (Config) MarshalJSON() ([]byte, error) { return json.Marshal(struct{}{}) }
