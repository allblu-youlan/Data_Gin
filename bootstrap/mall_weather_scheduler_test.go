package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"gin-biz-web-api/job"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/hibiken/asynq"
)

type fakeMallWeatherSchedulePlanner struct {
	payload job.MallWeatherSchedulePayload
	err     error
}

func TestMallWeatherScheduleHandlerDoesNotRetryTerminalRepairFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "server execution limit", err: fmt.Errorf("query: %w", &mysqlDriver.MySQLError{Number: 3024})},
		{name: "connection capacity", err: fmt.Errorf("query: %w", &mysqlDriver.MySQLError{Number: 1040})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			planner := &fakeMallWeatherSchedulePlanner{err: test.err}
			task, err := job.NewMallWeatherScheduleTask(job.MallWeatherSchedulePayload{TaskType: job.TypeMallWeatherRepair})
			if err != nil {
				t.Fatalf("NewMallWeatherScheduleTask() error=%v", err)
			}
			err = newMallWeatherScheduleHandler(planner).ProcessTask(t.Context(), task)
			if !errors.Is(err, asynq.SkipRetry) {
				t.Fatalf("ProcessTask() error=%v, want SkipRetry", err)
			}
		})
	}
}

func (planner *fakeMallWeatherSchedulePlanner) Plan(_ context.Context, payload job.MallWeatherSchedulePayload) error {
	planner.payload = payload
	return planner.err
}

func TestMallWeatherScheduleHandlerDecodesAndPlans(t *testing.T) {
	planner := &fakeMallWeatherSchedulePlanner{}
	task, err := job.NewMallWeatherScheduleTask(job.MallWeatherSchedulePayload{
		TaskType: job.TypeMallWeatherFast, DetailProfile: "full",
	})
	if err != nil {
		t.Fatalf("NewMallWeatherScheduleTask() error=%v", err)
	}
	if err := newMallWeatherScheduleHandler(planner).ProcessTask(context.Background(), task); err != nil {
		t.Fatalf("ProcessTask() error=%v", err)
	}
	if planner.payload.TaskType != job.TypeMallWeatherFast || planner.payload.DetailProfile != "full" {
		t.Fatalf("payload=%+v", planner.payload)
	}
}

func TestMallWeatherScheduleHandlerSkipsInvalidPayload(t *testing.T) {
	err := newMallWeatherScheduleHandler(&fakeMallWeatherSchedulePlanner{}).ProcessTask(
		context.Background(),
		asynq.NewTask(job.TypeMallWeatherSchedule, []byte(`{"task_type":"unknown"}`)),
	)
	if !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("ProcessTask() error=%v", err)
	}
}
