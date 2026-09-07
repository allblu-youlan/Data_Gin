package bootstrap

import (
	"testing"
	"time"

	"gin-biz-web-api/pkg/config"
)

func TestMigrationDatabaseIOTimeout(t *testing.T) {
	instance := config.Instance()
	original := instance.Get("cfg.database.migration_io_timeout_seconds")
	t.Cleanup(func() {
		instance.Set("cfg.database.migration_io_timeout_seconds", original)
	})
	for _, test := range []struct {
		name      string
		seconds   int
		want      time.Duration
		wantError bool
	}{
		{name: "minimum", seconds: 60, want: time.Minute},
		{name: "online ddl", seconds: 1800, want: 30 * time.Minute},
		{name: "maximum", seconds: 86400, want: 24 * time.Hour},
		{name: "too short", seconds: 59, wantError: true},
		{name: "too long", seconds: 86401, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance.Set("cfg.database.migration_io_timeout_seconds", test.seconds)
			got, err := migrationDatabaseIOTimeout()
			if (err != nil) != test.wantError {
				t.Fatalf("migrationDatabaseIOTimeout() error=%v wantError=%t", err, test.wantError)
			}
			if err == nil && got != test.want {
				t.Fatalf("migrationDatabaseIOTimeout()=%s want=%s", got, test.want)
			}
		})
	}
}
