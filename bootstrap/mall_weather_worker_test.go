package bootstrap

import (
	"context"
	"errors"
	"testing"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/database"
	jobPkg "gin-biz-web-api/pkg/job"

	"github.com/hibiken/asynq"
)

type weatherCooldownError struct{}

func (weatherCooldownError) Error() string      { return "provider circuit is open" }
func (weatherCooldownError) TaskDeferred() bool { return true }

func TestMallWeatherRetryResultPreservesRealFailureBudget(t *testing.T) {
	failure := errors.New("provider failed")
	for _, test := range []struct {
		name         string
		err          error
		retryCount   int
		maxAttempts  int
		wantSkip     bool
		wantDeferred bool
	}{
		{name: "first failure", err: failure, maxAttempts: 3},
		{name: "second failure", err: failure, retryCount: 1, maxAttempts: 3},
		{name: "third failure stops despite reserved queue slot", err: failure, retryCount: 2, maxAttempts: 3, wantSkip: true},
		{name: "first cooldown", err: weatherCooldownError{}, maxAttempts: 3, wantDeferred: true},
		{name: "cooldown after two failures preserves final attempt", err: weatherCooldownError{}, retryCount: 2, maxAttempts: 3, wantDeferred: true},
		{name: "database outage after two failures remains paused", err: errors.Join(errors.New("start failed"), database.ErrUnavailable), retryCount: 2, maxAttempts: 3},
		{name: "expired cooldown stops", err: &data_svc.MallWeatherProcessError{Code: "CIRCUIT_WAIT_EXPIRED"}, retryCount: 2, maxAttempts: 3, wantSkip: true},
		{name: "single attempt stops on real failure", err: failure, maxAttempts: 1, wantSkip: true},
		{name: "single attempt still waits for cooldown", err: weatherCooldownError{}, maxAttempts: 1, wantDeferred: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := mallWeatherRetryResult(test.err, test.retryCount, test.maxAttempts)
			if errors.Is(got, asynq.SkipRetry) != test.wantSkip || jobPkg.IsTaskDeferred(got) != test.wantDeferred {
				t.Fatalf("result = %v, want skip=%t deferred=%t", got, test.wantSkip, test.wantDeferred)
			}
			if !errors.Is(got, test.err) {
				t.Fatalf("result lost original cause: %v", got)
			}
		})
	}
}

type fakeMallWeatherProcessor struct {
	called   bool
	taskType string
	payload  job.MallTaskPayload
	err      error
}

func (processor *fakeMallWeatherProcessor) Process(_ context.Context, taskType string, payload job.MallTaskPayload) error {
	processor.called = true
	processor.taskType = taskType
	processor.payload = payload
	return processor.err
}

func TestMallWeatherHandlerDecodesAndProcessesTask(t *testing.T) {
	processor := &fakeMallWeatherProcessor{}
	task, err := job.NewMallWeatherFastTask(job.MallTaskPayload{MallID: 11, TaskWindow: "fast:11:202607221030"})
	if err != nil {
		t.Fatalf("NewMallWeatherFastTask() error=%v", err)
	}
	if err := newMallWeatherHandler(processor, true, 3).ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask() error=%v", err)
	}
	if !processor.called || processor.taskType != job.TypeMallWeatherFast || processor.payload.EndpointKind != "v26_weather" {
		t.Fatalf("processor call=%+v", processor)
	}
}

func TestMallWeatherHandlerAppliesRetryClassification(t *testing.T) {
	tests := []struct {
		name       string
		task       *asynq.Task
		processErr error
		wantSkip   bool
	}{
		{
			name:     "invalid payload",
			task:     asynq.NewTask(job.TypeMallWeatherFast, []byte(`{"mall_id":0}`)),
			wantSkip: true,
		},
		{
			name:       "non retryable process error",
			task:       asynq.NewTask(job.TypeMallWeatherFast, []byte(`{"mall_id":11,"task_window":"fast:11:202607221030"}`)),
			processErr: &data_svc.MallWeatherProcessError{Code: "INVALID_TASK"},
			wantSkip:   true,
		},
		{
			name:       "retryable process error",
			task:       asynq.NewTask(job.TypeMallWeatherFast, []byte(`{"mall_id":11,"task_window":"fast:11:202607221030"}`)),
			processErr: &data_svc.MallWeatherProcessError{Retryable: true, Code: "START_FAILED"},
			wantSkip:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor := &fakeMallWeatherProcessor{err: test.processErr}
			err := newMallWeatherHandler(processor, true, 3).ProcessTask(context.Background(), test.task)
			if err == nil || errors.Is(err, asynq.SkipRetry) != test.wantSkip {
				t.Fatalf("ProcessTask() error=%v wantSkip=%v", err, test.wantSkip)
			}
		})
	}
}

func TestMallWeatherHandlerUsesConfiguredFailureBudget(t *testing.T) {
	for _, test := range []struct {
		name         string
		err          error
		wantSkip     bool
		wantDeferred bool
	}{
		{name: "failure uses only configured attempt", err: &data_svc.MallWeatherProcessError{Retryable: true, Code: "PROVIDER_REQUEST_FAILED"}, wantSkip: true},
		{name: "cooldown preserves configured attempt", err: weatherCooldownError{}, wantDeferred: true},
		{name: "expired cooldown stops immediately", err: &data_svc.MallWeatherProcessError{Code: "CIRCUIT_WAIT_EXPIRED"}, wantSkip: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			task, err := job.NewMallWeatherFastTask(job.MallTaskPayload{MallID: 11, TaskWindow: "fast:11:202607221030"})
			if err != nil {
				t.Fatal(err)
			}
			processor := &fakeMallWeatherProcessor{err: test.err}
			got := newMallWeatherHandler(processor, true, 1).ProcessTask(t.Context(), task)
			if !errors.Is(got, test.err) || errors.Is(got, asynq.SkipRetry) != test.wantSkip || jobPkg.IsTaskDeferred(got) != test.wantDeferred {
				t.Fatalf("handler result = %v, want cause=%v skip=%t deferred=%t", got, test.err, test.wantSkip, test.wantDeferred)
			}
		})
	}
}

func TestMallWeatherHandlerSkipsQueuedRepairWhenDisabled(t *testing.T) {
	processor := &fakeMallWeatherProcessor{}
	task, err := job.NewMallWeatherRepairTask(job.MallTaskPayload{
		MallID: 11, TaskWindow: "repair:42:1", EndpointKind: "v26_weather",
	})
	if err != nil {
		t.Fatalf("NewMallWeatherRepairTask() error=%v", err)
	}
	if err := newMallWeatherHandler(processor, false, 3).ProcessTask(t.Context(), task); err != nil {
		t.Fatalf("ProcessTask(disabled repair) error=%v", err)
	}
	if processor.called {
		t.Fatal("disabled queued repair reached processor")
	}
}
