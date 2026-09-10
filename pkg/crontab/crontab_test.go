package crontab

import (
	"sync/atomic"
	"testing"

	"github.com/robfig/cron/v3"
)

func TestCronJobChainSkipsOverlappingRun(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32

	job := cron.FuncJob(func() {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
	})
	wrapped := cron.NewChain(cronJobChain(cron.DiscardLogger)...).Then(job)
	go func() {
		defer close(done)
		wrapped.Run()
	}()
	<-started

	wrapped.Run()
	if got := calls.Load(); got != 1 {
		t.Fatalf("overlapping calls=%d want=1", got)
	}
	close(release)
	<-done

	wrapped.Run()
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls after release=%d want=2", got)
	}
}
