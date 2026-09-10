package config

import (
	"os"
	"path/filepath"
	"testing"

	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestDatabaseReliabilityConfiguration(t *testing.T) {
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	contents := []byte(`
DB:
  MaxOpenConnections: 40
  MaxIdleConnections: 12
  MaxLifeSeconds: 600
  MaxIdleSeconds: 120
  ConnectTimeoutSeconds: 4
  ReadTimeoutSeconds: 45
  WriteTimeoutSeconds: 50
  MigrationIOTimeoutSeconds: 900
  RejectReadOnly: false
  InterpolateParams: false
`)
	if err := os.WriteFile(configFile, contents, 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))

	prefix := "cfg.database.mysql.default."
	wantInts := map[string]int{
		"max_open_connections":    40,
		"max_idle_connections":    12,
		"max_life_seconds":        600,
		"max_idle_seconds":        120,
		"connect_timeout_seconds": 4,
		"read_timeout_seconds":    45,
		"write_timeout_seconds":   50,
	}
	for key, want := range wantInts {
		if got := pkgConfig.GetInt(prefix + key); got != want {
			t.Errorf("%s = %d, want %d", key, got, want)
		}
	}
	if pkgConfig.GetBool(prefix + "reject_read_only") {
		t.Fatal("reject_read_only = true, want false")
	}
	if pkgConfig.GetBool(prefix + "interpolate_params") {
		t.Fatal("interpolate_params = true, want false")
	}
	if got := pkgConfig.GetInt("cfg.database.migration_io_timeout_seconds"); got != 900 {
		t.Fatalf("migration_io_timeout_seconds = %d, want 900", got)
	}
}

func TestDatabaseInterpolateParamsDefaultsToEnabled(t *testing.T) {
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte("DB:\n"), 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))

	if !pkgConfig.GetBool("cfg.database.mysql.default.interpolate_params") {
		t.Fatal("interpolate_params = false, want true")
	}
}

func TestDatabaseRejectReadOnlyConfiguration(t *testing.T) {
	tests := []struct {
		name       string
		configLine string
		envValue   string
		setEnv     bool
		want       bool
	}{
		{
			name: "配置缺省时使用安全默认值",
			want: true,
		},
		{
			name:       "占位符缺省时使用true",
			configLine: "  RejectReadOnly: ${DB_REJECT_READ_ONLY:-true}\n",
			want:       true,
		},
		{
			name:       "环境变量显式启用",
			configLine: "  RejectReadOnly: ${DB_REJECT_READ_ONLY:-true}\n",
			envValue:   "true",
			setEnv:     true,
			want:       true,
		},
		{
			name:       "环境变量显式禁用",
			configLine: "  RejectReadOnly: ${DB_REJECT_READ_ONLY:-true}\n",
			envValue:   "false",
			setEnv:     true,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.setEnv {
				t.Setenv("DB_REJECT_READ_ONLY", "")
				if err := os.Unsetenv("DB_REJECT_READ_ONLY"); err != nil {
					t.Fatalf("unset DB_REJECT_READ_ONLY: %v", err)
				}
			} else {
				t.Setenv("DB_REJECT_READ_ONLY", tt.envValue)
			}

			configDir := t.TempDir()
			configFile := filepath.Join(configDir, "config.yaml")
			contents := []byte("DB:\n" + tt.configLine)
			if err := os.WriteFile(configFile, contents, 0o600); err != nil {
				t.Fatalf("write test config: %v", err)
			}

			pkgConfig.NewConfig("", configDir+string(os.PathSeparator))
			const path = "cfg.database.mysql.default.reject_read_only"
			if got := pkgConfig.GetBool(path); got != tt.want {
				t.Fatalf("reject_read_only = %t, want %t", got, tt.want)
			}
		})
	}
}
