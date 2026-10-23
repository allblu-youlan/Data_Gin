package data_svc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"gin-biz-web-api/internal/dao/data_dao"
)

func TestExcelWorkflowStateMonitorCancelsBlockedWork(t *testing.T) {
	for _, stoppedErr := range []error{data_dao.ErrExcelMatchJobNotRunning, errors.New("job database unavailable")} {
		ctx, cancel := context.WithCancel(t.Context())
		var reads atomic.Int32
		stop := monitorExcelWorkflowState(ctx, time.Millisecond, func(context.Context) error {
			reads.Add(1)
			return stoppedErr
		}, cancel)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("state watcher did not cancel blocked work")
		}
		stop()
		cancel()
		if reads.Load() != 1 || !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("error=%v reads=%d", ctx.Err(), reads.Load())
		}
	}
}

func TestExcelWorkflowStateMonitorStopsAndJoinsInFlightCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, exited := make(chan struct{}), make(chan struct{})
	stop := monitorExcelWorkflowState(ctx, time.Millisecond, func(checkCtx context.Context) error {
		close(started)
		<-checkCtx.Done()
		close(exited)
		return checkCtx.Err()
	}, cancel)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("state watcher did not start")
	}
	joined := make(chan struct{})
	go func() { stop(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("watcher leaked after its owner stopped")
	}
	select {
	case <-exited:
	default:
		t.Fatal("stop returned before the in-flight state query exited")
	}
	if ctx.Err() != nil {
		t.Fatalf("stopping the watcher canceled successful workflow finalization: %v", ctx.Err())
	}
	stop() // Deferred cleanup can safely join a watcher already stopped above.
}

func TestExcelWorkflowStateMonitorExitsWithWorkflowContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	stop := monitorExcelWorkflowState(ctx, time.Hour, func(context.Context) error {
		t.Error("stopped workflow must not wait for another status poll")
		return nil
	}, cancel)
	cancel()
	joined := make(chan struct{})
	go func() { stop(); close(joined) }()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("watcher did not exit when workflow context ended")
	}
}
