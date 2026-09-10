package config

import (
	"os"
	"path/filepath"
	"testing"

	pkgConfig "gin-biz-web-api/pkg/config"
)

func TestOutboxPollingConfiguration(t *testing.T) {
	configDir := t.TempDir()
	configFile := filepath.Join(configDir, "config.yaml")
	contents := []byte(`
QueueJob:
  Outbox:
    PollIntervalMS: 750
    IdlePollMaxIntervalMS: 12000
`)
	if err := os.WriteFile(configFile, contents, 0o600); err != nil {
		t.Fatalf("write test config: %v", err)
	}
	pkgConfig.NewConfig("", configDir+string(os.PathSeparator))

	if got := pkgConfig.GetInt("cfg.queue_job.outbox.poll_interval_ms"); got != 750 {
		t.Fatalf("poll_interval_ms = %d, want 750", got)
	}
	if got := pkgConfig.GetInt("cfg.queue_job.outbox.idle_poll_max_interval_ms"); got != 12000 {
		t.Fatalf("idle_poll_max_interval_ms = %d, want 12000", got)
	}
}
