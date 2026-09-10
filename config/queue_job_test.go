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

func TestQueueRuntimeRoleConfiguration(t *testing.T) {
	tests := []struct {
		name          string
		yaml          string
		workerEnv     string
		schedulerEnv  string
		crontabEnv    string
		wantWorker    bool
		wantScheduler bool
		wantCrontab   bool
	}{
		{
			name:       "backward compatible defaults",
			wantWorker: true, wantScheduler: true, wantCrontab: true,
		},
		{
			name: "yaml separates api role",
			yaml: "QueueJob:\n  WorkerEnabled: false\n  SchedulerEnabled: false\n  CrontabEnabled: false\n",
		},
		{
			name:      "environment selects worker role",
			workerEnv: "true", schedulerEnv: "false", crontabEnv: "false",
			wantWorker: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(EnvQueueWorkerEnabled, test.workerEnv)
			t.Setenv(EnvQueueSchedulerEnabled, test.schedulerEnv)
			t.Setenv(EnvCrontabEnabled, test.crontabEnv)
			configDir := t.TempDir()
			configFile := filepath.Join(configDir, "config.yaml")
			if err := os.WriteFile(configFile, []byte(test.yaml), 0o600); err != nil {
				t.Fatalf("write test config: %v", err)
			}
			pkgConfig.NewConfig("", configDir+string(os.PathSeparator))
			if got := pkgConfig.GetBool("cfg.queue_job.worker_enabled"); got != test.wantWorker {
				t.Fatalf("worker_enabled=%t want=%t", got, test.wantWorker)
			}
			if got := pkgConfig.GetBool("cfg.queue_job.scheduler_enabled"); got != test.wantScheduler {
				t.Fatalf("scheduler_enabled=%t want=%t", got, test.wantScheduler)
			}
			if got := pkgConfig.GetBool("cfg.queue_job.crontab_enabled"); got != test.wantCrontab {
				t.Fatalf("crontab_enabled=%t want=%t", got, test.wantCrontab)
			}
		})
	}
}
