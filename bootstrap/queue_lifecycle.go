package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

type managedQueueWorker interface {
	Start(asynq.Handler) error
	Stop()
	Shutdown()
}
type managedQueueScheduler interface {
	Start() error
	Shutdown()
}

const (
	queueServerShutdownWait = 20 * time.Second
	queueHandlerCleanupWait = 7 * time.Second
)

type queueJobLifecycle struct {
	mu           sync.Mutex
	workers      []managedQueueWorker
	scheduler    managedQueueScheduler
	shutdownDone chan struct{}

	activeHandlers  int
	handlersClosing bool
	handlersDone    chan struct{}
}

var queueRuntime = &queueJobLifecycle{}

func (lifecycle *queueJobLifecycle) startWorker(worker managedQueueWorker, handler asynq.Handler) error {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.shutdownDone != nil {
		return fmt.Errorf("queue worker: lifecycle is shutting down")
	}
	// Asynq Start starts its goroutines without waiting for task processing.
	// Keep registration atomic with shutdown so no started worker is omitted.
	if err := worker.Start(lifecycle.trackHandler(handler)); err != nil {
		return err
	}
	lifecycle.workers = append(lifecycle.workers, worker)
	return nil
}

func (lifecycle *queueJobLifecycle) startScheduler(scheduler managedQueueScheduler) error {
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.shutdownDone != nil {
		return fmt.Errorf("queue scheduler: lifecycle is shutting down")
	}
	if err := scheduler.Start(); err != nil {
		return err
	}
	lifecycle.scheduler = scheduler
	return nil
}

func (lifecycle *queueJobLifecycle) beginShutdown() {
	lifecycle.mu.Lock()
	if lifecycle.shutdownDone != nil {
		lifecycle.mu.Unlock()
		return
	}
	done := make(chan struct{})
	lifecycle.shutdownDone = done
	workers := append([]managedQueueWorker(nil), lifecycle.workers...)
	scheduler := lifecycle.scheduler
	lifecycle.mu.Unlock()

	go func() {
		defer close(done)
		var stopped sync.WaitGroup
		for _, worker := range workers {
			stopped.Add(1)
			go func(worker managedQueueWorker) {
				defer stopped.Done()
				worker.Stop()
				worker.Shutdown()
			}(worker)
		}
		if scheduler != nil {
			stopped.Add(1)
			go func() {
				defer stopped.Done()
				scheduler.Shutdown()
			}()
		}
		stopped.Wait()
	}()
}

func (lifecycle *queueJobLifecycle) waitShutdown(serverWait, cleanupWait time.Duration) error {
	lifecycle.beginShutdown()
	lifecycle.mu.Lock()
	stopped := lifecycle.shutdownDone
	lifecycle.mu.Unlock()
	serverCtx, cancelServer := context.WithTimeout(context.Background(), serverWait)
	var shutdownErr error
	select {
	case <-stopped:
	case <-serverCtx.Done():
		shutdownErr = fmt.Errorf("queue job: server/scheduler shutdown timed out: %w", serverCtx.Err())
	}
	cancelServer()

	// Asynq can requeue a task and return from Shutdown before the task's
	// handler finishes its cancellation cleanup. Track the actual callbacks,
	// close admission under the same mutex, and allow a separate cleanup budget.
	lifecycle.mu.Lock()
	if !lifecycle.handlersClosing {
		lifecycle.handlersClosing = true
		lifecycle.handlersDone = make(chan struct{})
		if lifecycle.activeHandlers == 0 {
			close(lifecycle.handlersDone)
		}
	}
	done := lifecycle.handlersDone
	lifecycle.mu.Unlock()
	cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), cleanupWait)
	defer cancelCleanup()
	select {
	case <-done:
		return shutdownErr
	case <-cleanupCtx.Done():
		lifecycle.mu.Lock()
		active := lifecycle.activeHandlers
		lifecycle.mu.Unlock()
		return errors.Join(shutdownErr, fmt.Errorf("queue job: handler cleanup timed out with %d running: %w", active, cleanupCtx.Err()))
	}
}

func (lifecycle *queueJobLifecycle) trackHandler(handler asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		lifecycle.mu.Lock()
		if lifecycle.handlersClosing {
			lifecycle.mu.Unlock()
			return context.Canceled
		}
		lifecycle.activeHandlers++
		lifecycle.mu.Unlock()
		defer func() {
			lifecycle.mu.Lock()
			defer lifecycle.mu.Unlock()
			lifecycle.activeHandlers--
			if lifecycle.handlersClosing && lifecycle.activeHandlers == 0 {
				close(lifecycle.handlersDone)
			}
		}()
		return handler.ProcessTask(ctx, task)
	})
}
