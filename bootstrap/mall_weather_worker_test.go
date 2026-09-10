package bootstrap

import (
	"context"
	"errors"
	"testing"

	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/job"

	"github.com/hibiken/asynq"
)

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
	if err := newMallWeatherHandler(processor, true).ProcessTask(context.Background(), task); err != nil {
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
			err := newMallWeatherHandler(processor, true).ProcessTask(context.Background(), test.task)
			if err == nil || errors.Is(err, asynq.SkipRetry) != test.wantSkip {
				t.Fatalf("ProcessTask() error=%v wantSkip=%v", err, test.wantSkip)
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
	if err := newMallWeatherHandler(processor, false).ProcessTask(t.Context(), task); err != nil {
		t.Fatalf("ProcessTask(disabled repair) error=%v", err)
	}
	if processor.called {
		t.Fatal("disabled queued repair reached processor")
	}
}
