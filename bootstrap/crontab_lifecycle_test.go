package bootstrap

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeStoppableCron struct {
	mu        sync.Mutex
	stopCalls int
	stopped   chan struct{}
}

func (cron *fakeStoppableCron) Stop() context.Context {
	cron.mu.Lock()
	cron.stopCalls++
	cron.mu.Unlock()
	return channelContext{done: cron.stopped}
}

func (cron *fakeStoppableCron) calls() int {
	cron.mu.Lock()
	defer cron.mu.Unlock()
	return cron.stopCalls
}

type channelContext struct {
	context.Context
	done <-chan struct{}
}

func (ctx channelContext) Done() <-chan struct{} { return ctx.done }

func TestStopCrontabCancelsTasksAndStopsScheduler(t *testing.T) {
	cron := &fakeStoppableCron{stopped: make(chan struct{})}
	lifecycleCtx := startCrontabLifecycle(cron)

	done := make(chan struct{})
	go func() {
		stopCrontab(t.Context())
		close(done)
	}()
	select {
	case <-lifecycleCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cron lifecycle was not canceled")
	}
	if cron.calls() != 1 {
		t.Fatalf("cron stop calls = %d, want 1", cron.calls())
	}
	close(cron.stopped)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stopCrontab did not wait for running tasks")
	}
}
