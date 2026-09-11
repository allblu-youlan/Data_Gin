package bootstrap

import (
	"context"
	"testing"
)

type fakeCronJob struct{ calls int }

func (job *fakeCronJob) Run() { job.calls++ }

func (job *fakeCronJob) RunContext(ctx context.Context) {
	if ctx != nil && ctx.Err() == nil {
		job.calls++
	}
}

func TestGuardedDatabaseCronJob(t *testing.T) {
	tests := []struct {
		name      string
		available bool
		withNext  bool
		withCheck bool
		wantCalls int
	}{
		{name: "available", available: true, withNext: true, withCheck: true, wantCalls: 1},
		{name: "unavailable", available: false, withNext: true, withCheck: true},
		{name: "missing job", available: true, withCheck: true},
		{name: "missing checker", available: true, withNext: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := &fakeCronJob{}
			job := guardedDatabaseCronJob{}
			if tt.withNext {
				job.next = next
			}
			if tt.withCheck {
				job.available = func(context.Context) bool { return tt.available }
			}

			job.RunContext(t.Context())
			if next.calls != tt.wantCalls {
				t.Fatalf("cron calls = %d, want %d", next.calls, tt.wantCalls)
			}
		})
	}
}
