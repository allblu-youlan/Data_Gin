package bootstrap

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func TestQueueLifecycleWaitsForEveryWorkerAndScheduler(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	first, second, scheduler := newQueueLifecycleWorker(t), newQueueLifecycleWorker(t), newQueueLifecycleWorker(t)
	for _, worker := range []*queueLifecycleWorker{first, second} {
		if err := lifecycle.startWorker(worker, asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
			t.Fatal(err)
		}
	}
	if err := lifecycle.startScheduler(queueLifecycleScheduler{scheduler}); err != nil {
		t.Fatal(err)
	}
	lifecycle.beginShutdown()
	for _, worker := range []*queueLifecycleWorker{first, second, scheduler} {
		awaitQueueLifecycleSignal(t, worker.shutdownStarted, "all queue components must stop concurrently")
	}
	done := make(chan error, 1)
	go func() { done <- lifecycle.waitShutdown(time.Second, time.Second) }()
	first.release()
	second.release()
	assertQueueShutdownWaiting(t, done)
	scheduler.release()
	if err := awaitQueueLifecycleResult(t, done); err != nil {
		t.Fatal(err)
	}
	for _, worker := range []*queueLifecycleWorker{first, second} {
		if worker.starts.Load() != 1 || worker.stops.Load() != 1 || worker.shutdowns.Load() != 1 {
			t.Fatalf("worker lifecycle calls: start=%d stop=%d shutdown=%d", worker.starts.Load(), worker.stops.Load(), worker.shutdowns.Load())
		}
	}
}

func TestQueueLifecycleConcurrentShutdownWaitsForSameCompletion(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	worker := newQueueLifecycleWorker(t)
	if err := lifecycle.startWorker(worker, asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil })); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { done <- lifecycle.waitShutdown(time.Second, time.Second) }()
	}
	awaitQueueLifecycleSignal(t, worker.shutdownStarted, "worker shutdown did not start")
	assertQueueShutdownWaiting(t, done)
	worker.release()
	for i := 0; i < 2; i++ {
		if err := awaitQueueLifecycleResult(t, done); err != nil {
			t.Fatal(err)
		}
	}
	if worker.stops.Load() != 1 || worker.shutdowns.Load() != 1 {
		t.Fatal("concurrent callers repeated stop/shutdown")
	}
}

func TestQueueLifecycleWaitsForActualHandlerCleanup(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	worker := newQueueLifecycleWorker(t)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	worker.onShutdown = cancel
	handlerEntered, cleanupEntered, cleanupRelease := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(cleanupRelease) }) })
	cause := errors.New("original handler failure")
	err := lifecycle.startWorker(worker, asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		close(handlerEntered)
		<-ctx.Done()
		close(cleanupEntered)
		<-cleanupRelease
		return cause
	}))
	if err != nil {
		t.Fatal(err)
	}
	handlerDone := make(chan error, 1)
	go func() { handlerDone <- worker.handler.ProcessTask(ctx, asynq.NewTask("test", nil)) }()
	awaitQueueLifecycleSignal(t, handlerEntered, "handler did not start")
	worker.release()
	done := make(chan error, 1)
	go func() { done <- lifecycle.waitShutdown(time.Second, time.Second) }()
	awaitQueueLifecycleSignal(t, cleanupEntered, "shutdown did not cancel task")
	assertQueueShutdownWaiting(t, done)
	release.Do(func() { close(cleanupRelease) })
	if err := awaitQueueLifecycleResult(t, handlerDone); !errors.Is(err, cause) {
		t.Fatalf("handler cause changed: %v", err)
	}
	if err := awaitQueueLifecycleResult(t, done); err != nil {
		t.Fatal(err)
	}
	if err := worker.handler.ProcessTask(t.Context(), asynq.NewTask("late", nil)); !errors.Is(err, context.Canceled) {
		t.Fatalf("handler started after resources could close: %v", err)
	}
}

func TestQueueLifecycleBoundsHandlerCleanupAndPreservesCause(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	worker := newQueueLifecycleWorker(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	cause := errors.New("cleanup eventually returned its original error")
	if err := lifecycle.startWorker(worker, asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		close(entered)
		<-release
		return cause
	})); err != nil {
		t.Fatal(err)
	}
	handlerDone := make(chan error, 1)
	go func() { handlerDone <- worker.handler.ProcessTask(t.Context(), asynq.NewTask("test", nil)) }()
	awaitQueueLifecycleSignal(t, entered, "handler did not start")
	worker.release()
	err := lifecycle.waitShutdown(time.Second, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "handler cleanup") {
		t.Fatalf("cleanup wait must be bounded and explicit: %v", err)
	}
	once.Do(func() { close(release) })
	if err := awaitQueueLifecycleResult(t, handlerDone); !errors.Is(err, cause) {
		t.Fatalf("shutdown swallowed original handler error: %v", err)
	}
}

func TestQueueLifecycleTracksAlreadyDispatchedTaskStartingDuringShutdown(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	worker := newQueueLifecycleWorker(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	if err := lifecycle.startWorker(worker, asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
		close(entered)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	lifecycle.beginShutdown()
	awaitQueueLifecycleSignal(t, worker.shutdownStarted, "shutdown did not start")
	handlerDone := make(chan error, 1)
	go func() {
		handlerDone <- worker.handler.ProcessTask(t.Context(), asynq.NewTask("already-dispatched", nil))
	}()
	awaitQueueLifecycleSignal(t, entered, "already dispatched task was not tracked")
	done := make(chan error, 1)
	go func() { done <- lifecycle.waitShutdown(time.Second, time.Second) }()
	worker.release()
	assertQueueShutdownWaiting(t, done)
	once.Do(func() { close(release) })
	if err := awaitQueueLifecycleResult(t, done); err != nil {
		t.Fatal(err)
	}
	if err := awaitQueueLifecycleResult(t, handlerDone); err != nil {
		t.Fatal(err)
	}
}

func TestQueueLifecycleBoundsStalledServerShutdown(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	worker := newQueueLifecycleWorker(t)
	if err := lifecycle.startWorker(worker, nil); err != nil {
		t.Fatal(err)
	}
	err := lifecycle.waitShutdown(20*time.Millisecond, time.Second)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "server/scheduler") {
		t.Fatalf("stalled server shutdown did not return a bounded diagnostic: %v", err)
	}
}

func TestQueueLifecycleDoesNotRegisterFailedStart(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	worker := newQueueLifecycleWorker(t)
	worker.startErr = errors.New("worker startup failed")
	if err := lifecycle.startWorker(worker, nil); !errors.Is(err, worker.startErr) {
		t.Fatalf("lost startup failure: %v", err)
	}
	if err := lifecycle.waitShutdown(time.Second, time.Second); err != nil {
		t.Fatal(err)
	}
	if worker.stops.Load() != 0 || worker.shutdowns.Load() != 0 {
		t.Fatal("failed server registered as running")
	}
}

func TestServerShutdownWaitsForQueueWithoutDelayingHTTP(t *testing.T) {
	lifecycle := &queueJobLifecycle{}
	previous := queueRuntime
	queueRuntime = lifecycle
	t.Cleanup(func() { queueRuntime = previous })
	worker := newQueueLifecycleWorker(t)
	if err := lifecycle.startWorker(worker, nil); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("HTTP shutdown failed")
	server := &queueLifecycleHTTPServer{entered: make(chan struct{}), err: cause}
	done := make(chan error, 1)
	go func() { done <- shutdownServerAndCrontab(t.Context(), server) }()
	awaitQueueLifecycleSignal(t, server.entered, "HTTP shutdown waited for queue")
	awaitQueueLifecycleSignal(t, worker.shutdownStarted, "queue shutdown was not wired")
	assertQueueShutdownWaiting(t, done)
	worker.release()
	if err := awaitQueueLifecycleResult(t, done); !errors.Is(err, cause) {
		t.Fatalf("shutdown lost HTTP root error: %v", err)
	}
}

type queueLifecycleWorker struct {
	handler                  asynq.Handler
	startErr                 error
	onShutdown               func()
	starts, stops, shutdowns atomic.Int32
	shutdownStarted          chan struct{}
	shutdownRelease          chan struct{}
	releaseOnce              sync.Once
}

func newQueueLifecycleWorker(t *testing.T) *queueLifecycleWorker {
	t.Helper()
	worker := &queueLifecycleWorker{shutdownStarted: make(chan struct{}), shutdownRelease: make(chan struct{})}
	t.Cleanup(worker.release)
	return worker
}

func (worker *queueLifecycleWorker) Start(handler asynq.Handler) error {
	worker.starts.Add(1)
	worker.handler = handler
	return worker.startErr
}
func (worker *queueLifecycleWorker) Stop() { worker.stops.Add(1) }
func (worker *queueLifecycleWorker) Shutdown() {
	worker.shutdowns.Add(1)
	close(worker.shutdownStarted)
	if worker.onShutdown != nil {
		worker.onShutdown()
	}
	<-worker.shutdownRelease
}
func (worker *queueLifecycleWorker) release() {
	worker.releaseOnce.Do(func() { close(worker.shutdownRelease) })
}

type queueLifecycleScheduler struct{ worker *queueLifecycleWorker }

func (scheduler queueLifecycleScheduler) Start() error { return scheduler.worker.Start(nil) }
func (scheduler queueLifecycleScheduler) Shutdown()    { scheduler.worker.Shutdown() }

type queueLifecycleHTTPServer struct {
	entered chan struct{}
	err     error
}

func (server *queueLifecycleHTTPServer) Shutdown(context.Context) error {
	close(server.entered)
	return server.err
}

func awaitQueueLifecycleSignal(t *testing.T, done <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func awaitQueueLifecycleResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("queue lifecycle did not finish")
		return nil
	}
}

func assertQueueShutdownWaiting(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		t.Fatalf("shutdown returned before work finished: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}
