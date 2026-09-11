package bootstrap

import (
	"context"
	"sync"
)

type stoppableCron interface {
	Stop() context.Context
}

var crontabLifecycle struct {
	sync.Mutex
	cancel context.CancelFunc
	task   stoppableCron
}

func startCrontabLifecycle(task stoppableCron) context.Context {
	ctx, cancel := context.WithCancel(context.Background())

	crontabLifecycle.Lock()
	previousCancel := crontabLifecycle.cancel
	previousTask := crontabLifecycle.task
	crontabLifecycle.cancel = cancel
	crontabLifecycle.task = task
	crontabLifecycle.Unlock()

	if previousCancel != nil {
		previousCancel()
	}
	if previousTask != nil {
		previousTask.Stop()
	}
	return ctx
}

func stopCrontab(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}

	crontabLifecycle.Lock()
	cancel := crontabLifecycle.cancel
	task := crontabLifecycle.task
	crontabLifecycle.cancel = nil
	crontabLifecycle.task = nil
	crontabLifecycle.Unlock()

	if cancel != nil {
		cancel()
	}
	if task == nil {
		return
	}
	stopped := task.Stop()
	select {
	case <-stopped.Done():
	case <-ctx.Done():
	}
}
