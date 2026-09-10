package bootstrap

import (
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestConfigureMySQLInterpolation(t *testing.T) {
	tests := []struct {
		name        string
		charset     string
		enabled     bool
		wantEnabled bool
		wantError   bool
	}{
		{name: "utf8mb4 enabled", charset: "utf8mb4", enabled: true, wantEnabled: true},
		{name: "utf8 enabled", charset: "UTF8", enabled: true, wantEnabled: true},
		{name: "disabled accepts legacy charset", charset: "gbk", enabled: false},
		{name: "enabled rejects legacy charset", charset: "gbk", enabled: true, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driverConfig := mysqlDriver.NewConfig()
			err := configureMySQLInterpolation(driverConfig, tt.charset, tt.enabled)
			if (err != nil) != tt.wantError {
				t.Fatalf("configureMySQLInterpolation() error = %v, wantError %t", err, tt.wantError)
			}
			if driverConfig.InterpolateParams != tt.wantEnabled {
				t.Fatalf("InterpolateParams = %t, want %t", driverConfig.InterpolateParams, tt.wantEnabled)
			}
		})
	}
}
