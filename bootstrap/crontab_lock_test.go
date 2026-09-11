package bootstrap

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gin-biz-web-api/internal/weather"
)

type fakeCronLocker struct {
	lock     weather.TaskLock
	acquired bool
	err      error
	keys     []string
}

func (locker *fakeCronLocker) Acquire(_ context.Context, key string) (weather.TaskLock, bool, error) {
	locker.keys = append(locker.keys, key)
	return locker.lock, locker.acquired, locker.err
}

type fakeRenewableCronLock struct {
	mu           sync.Mutex
	renewErr     error
	renewCalls   int
	releaseCalls int
}

func (lock *fakeRenewableCronLock) Renew(context.Context) error {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	lock.renewCalls++
	return lock.renewErr
}

func (lock *fakeRenewableCronLock) Release(context.Context) error {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	lock.releaseCalls++
	return nil
}

func (lock *fakeRenewableCronLock) counts() (int, int) {
	lock.mu.Lock()
	defer lock.mu.Unlock()
	return lock.renewCalls, lock.releaseCalls
}

type blockingContextCronJob struct {
	started  chan struct{}
	canceled chan struct{}
}

func (job *blockingContextCronJob) RunContext(ctx context.Context) {
	close(job.started)
	<-ctx.Done()
	close(job.canceled)
}

func TestDistributedCronJobRunsAndReleasesOwnedLock(t *testing.T) {
	lock := &fakeRenewableCronLock{}
	locker := &fakeCronLocker{lock: lock, acquired: true}
	next := &fakeCronJob{}
	job := newDistributedCronJob(t.Context(), "data_collect", locker, next)

	job.RunContext(t.Context())

	if next.calls != 1 {
		t.Fatalf("cron calls = %d, want 1", next.calls)
	}
	if len(locker.keys) != 1 || locker.keys[0] != "data_collect" {
		t.Fatalf("acquired keys = %v, want [data_collect]", locker.keys)
	}
	_, releaseCalls := lock.counts()
	if releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", releaseCalls)
	}
}

func TestDistributedCronJobFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		locker weather.TaskLocker
	}{
		{name: "missing locker"},
		{name: "lock contention", locker: &fakeCronLocker{acquired: false}},
		{name: "redis failure", locker: &fakeCronLocker{err: errors.New("redis unavailable")}},
		{name: "missing acquired lock", locker: &fakeCronLocker{acquired: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := &fakeCronJob{}
			job := newDistributedCronJob(t.Context(), "clear_logs", tt.locker, next)
			job.RunContext(t.Context())
			if next.calls != 0 {
				t.Fatalf("cron calls = %d, want 0", next.calls)
			}
		})
	}
}

func TestDistributedCronJobCancelsRunWhenLeaseIsLost(t *testing.T) {
	lock := &fakeRenewableCronLock{renewErr: weather.ErrTaskLockLeaseLost}
	locker := &fakeCronLocker{lock: lock, acquired: true}
	next := &blockingContextCronJob{started: make(chan struct{}), canceled: make(chan struct{})}
	job := newDistributedCronJob(t.Context(), "bojun_order", locker, next)
	job.renewInterval = time.Millisecond
	job.redisTimeout = time.Second

	done := make(chan struct{})
	go func() {
		defer close(done)
		job.RunContext(t.Context())
	}()

	select {
	case <-next.started:
	case <-time.After(time.Second):
		t.Fatal("cron task did not start")
	}
	select {
	case <-next.canceled:
	case <-time.After(time.Second):
		t.Fatal("cron task was not canceled after lease loss")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("distributed cron job did not finish")
	}

	renewCalls, releaseCalls := lock.counts()
	if renewCalls == 0 {
		t.Fatal("renew was not attempted")
	}
	if releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1", releaseCalls)
	}
}

type panicContextCronJob struct{}

func (panicContextCronJob) RunContext(context.Context) {
	panic("cron panic")
}

func TestDistributedCronJobReleasesLockAfterTaskPanic(t *testing.T) {
	lock := &fakeRenewableCronLock{}
	locker := &fakeCronLocker{lock: lock, acquired: true}
	job := newDistributedCronJob(t.Context(), "clear_logs", locker, panicContextCronJob{})

	func() {
		defer func() {
			if recovered := recover(); recovered == nil {
				t.Fatal("expected cron panic")
			}
		}()
		job.Run()
	}()

	_, releaseCalls := lock.counts()
	if releaseCalls != 1 {
		t.Fatalf("release calls after panic = %d, want 1", releaseCalls)
	}
}

func TestDistributedCronJobRunUsesLifecycleContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	lock := &fakeRenewableCronLock{}
	locker := &fakeCronLocker{lock: lock, acquired: true}
	next := &blockingContextCronJob{started: make(chan struct{}), canceled: make(chan struct{})}
	job := newDistributedCronJob(ctx, "data_collect", locker, next)

	done := make(chan struct{})
	go func() {
		defer close(done)
		job.Run()
	}()
	select {
	case <-next.started:
	case <-time.After(time.Second):
		t.Fatal("cron task did not start")
	}
	cancel()
	select {
	case <-next.canceled:
	case <-time.After(time.Second):
		t.Fatal("production Run did not propagate lifecycle cancellation")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("production Run did not finish after lifecycle cancellation")
	}

	_, releaseCalls := lock.counts()
	if releaseCalls != 1 {
		t.Fatalf("release calls after lifecycle cancellation = %d, want 1", releaseCalls)
	}
}
