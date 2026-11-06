//go:build integration

package job

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	weatherjob "gin-biz-web-api/job"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

type queueDeferralTestError struct{ deferred bool }

func (e queueDeferralTestError) Error() string             { return "provider unavailable" }
func (e queueDeferralTestError) TaskDeferred() bool        { return e.deferred }
func (e queueDeferralTestError) RetryDelay() time.Duration { return time.Millisecond }

func TestAsynqWeatherDeferralsPreserveFinalFailureAttempt(t *testing.T) {
	addr := os.Getenv("MALL_WEATHER_REDIS_ADDR")
	if addr == "" {
		t.Skip("MALL_WEATHER_REDIS_ADDR is required for Redis integration tests")
	}
	opt := asynq.RedisClientOpt{Addr: addr}
	queue := "weather-deferral-test-" + uuid.NewString()
	maxRetry := -1
	for _, definition := range weatherjob.MallWeatherOutboxTaskDefinitions(2, time.Minute) {
		if definition.TaskType == weatherjob.TypeMallWeatherFast {
			maxRetry = definition.MaxRetry
		}
	}
	if maxRetry != 3 {
		t.Fatalf("weather retry limit = %d, want one reserved deferral slot", maxRetry)
	}
	client := asynq.NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()
	server := asynq.NewServer(opt, asynq.Config{
		Concurrency: 1, Queues: map[string]int{queue: 1},
		IsFailure: isTaskFailure, RetryDelayFunc: retryDelay,
		TaskCheckInterval: 10 * time.Millisecond, DelayedTaskCheckInterval: 10 * time.Millisecond,
		ShutdownTimeout: time.Second, LogLevel: asynq.FatalLevel,
	})
	counts := make(chan int, 10)
	calls := 0
	handler := asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		count, _ := asynq.GetRetryCount(ctx)
		counts <- count
		calls++
		switch {
		case calls <= 2:
			return queueDeferralTestError{}
		case calls <= 6:
			return queueDeferralTestError{deferred: true}
		default:
			return nil
		}
	})
	if err := server.Start(handler); err != nil {
		t.Fatal(err)
	}
	defer server.Shutdown()
	task, err := client.Enqueue(asynq.NewTask(weatherjob.TypeMallWeatherFast, nil),
		asynq.Queue(queue), asynq.MaxRetry(maxRetry), asynq.Retention(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := inspector.DeleteTask(queue, task.ID); err != nil && !errors.Is(err, asynq.ErrTaskNotFound) {
			t.Errorf("delete integration task: %v", err)
		}
	}()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	var retried []int
	for {
		select {
		case count := <-counts:
			retried = append(retried, count)
		case <-poll.C:
			info, err := inspector.GetTaskInfo(queue, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if info.State == asynq.TaskStateArchived {
				t.Fatalf("task archived during circuit deferral: retried=%d error=%s", info.Retried, info.LastErr)
			}
			if info.State == asynq.TaskStateCompleted {
				for len(counts) > 0 {
					retried = append(retried, <-counts)
				}
				if want := []int{0, 1, 2, 2, 2, 2, 2}; !reflect.DeepEqual(retried, want) || info.Retried != 2 {
					t.Fatalf("retry history=%v, final retried=%d, want %v and 2", retried, info.Retried, want)
				}
				return
			}
		case <-deadline.C:
			t.Fatalf("weather task did not finish; retry history=%v", retried)
		}
	}
}
