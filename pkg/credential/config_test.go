package credential

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name         string
		requirements Requirements
		environment  map[string]string
		wantError    string
	}{
		{
			name:         "weather credentials configured",
			requirements: Requirements{RequireMallWeather: true},
			environment: map[string]string{
				EnvAmapWebServiceKey: "amap-value",
				EnvCaiyunAppKey:      "caiyun-key-value",
				EnvCaiyunAppSecret:   "caiyun-secret-value",
			},
		},
		{
			name:         "missing values report names only",
			requirements: Requirements{RequireMallWeather: true},
			environment:  map[string]string{EnvAmapWebServiceKey: "amap-value"},
			wantError:    EnvCaiyunAppKey,
		},
		{
			name:         "production rejects placeholders",
			requirements: Requirements{Production: true, RequireMallWeather: true},
			environment: map[string]string{
				EnvAmapWebServiceKey: "amap-value",
				EnvCaiyunAppKey:      "example",
				EnvCaiyunAppSecret:   "secret-value",
			},
			wantError: EnvCaiyunAppKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range allNames() {
				t.Setenv(name, "")
			}
			for name, value := range tt.environment {
				t.Setenv(name, value)
			}

			cfg, err := Load(tt.requirements)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("Load() error = %v", err)
				}
				if tt.requirements.RequireMallWeather && cfg.CaiyunAppSecret() != tt.environment[EnvCaiyunAppSecret] {
					t.Fatal("Load() did not preserve credential value")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Load() error = %v, want variable name %s", err, tt.wantError)
			}
			for _, value := range tt.environment {
				if value != "" && strings.Contains(err.Error(), value) {
					t.Fatalf("Load() error leaked credential value %q", value)
				}
			}
		})
	}
}

func TestFeishuAppDoesNotRequireWeatherResources(t *testing.T) {
	for _, name := range allNames() {
		t.Setenv(name, "")
	}
	t.Setenv(EnvFeishuAppID, "cli_example_app")
	t.Setenv(EnvFeishuAppSecret, "configured-application-secret")
	if _, err := Load(Requirements{RequireFeishuApp: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(Requirements{RequireFeishu: true}); err == nil {
		t.Fatal("weather resource validation was bypassed")
	}
}

func TestConfigCannotSerializeCredentials(t *testing.T) {
	t.Setenv(EnvCaiyunAppSecret, "do-not-serialize")
	cfg, err := Load(Requirements{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(data), "do-not-serialize") || string(data) != "{}" {
		t.Fatalf("json.Marshal() = %s, want {}", data)
	}
	for _, formatted := range []string{
		fmt.Sprintf("%v", cfg),
		fmt.Sprintf("%+v", cfg),
		fmt.Sprintf("%#v", cfg),
	} {
		if strings.Contains(formatted, "do-not-serialize") {
			t.Fatalf("formatted config leaked credential: %s", formatted)
		}
	}
}

func TestEnvironmentValueUsesAllowlist(t *testing.T) {
	t.Setenv(EnvFeishuWeatherSpreadsheetToken, "sheet-token")
	t.Setenv("UNRELATED_SECRET", "must-not-be-readable")
	cfg, err := Load(Requirements{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	value, err := cfg.EnvironmentValue(EnvFeishuWeatherSpreadsheetToken)
	if err != nil || value != "sheet-token" {
		t.Fatalf("EnvironmentValue() = %q, %v", value, err)
	}
	if _, err := cfg.EnvironmentValue("UNRELATED_SECRET"); err == nil {
		t.Fatal("EnvironmentValue() accepted a non-allowlisted variable")
	}
}
