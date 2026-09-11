package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/database"

	"github.com/hibiken/asynq"
)

func TestQueueDatabaseAvailabilityMiddleware(t *testing.T) {
	tests := []struct {
		name          string
		task          *asynq.Task
		repairEnabled bool
		checkErr      error
		wantChecked   bool
		wantCalled    bool
		wantErr       bool
	}{
		{name: "available", task: asynq.NewTask("database-task", nil), wantChecked: true, wantCalled: true},
		{name: "unavailable", task: asynq.NewTask("database-task", nil), checkErr: database.ErrUnavailable, wantChecked: true, wantErr: true},
		{name: "database independent", task: asynq.NewTask(job.TypeFoo, nil), checkErr: database.ErrUnavailable, wantCalled: true},
		{name: "disabled repair execution", task: asynq.NewTask(job.TypeMallWeatherRepair, []byte(`{"invalid":"ignored by disabled handler"}`)), checkErr: database.ErrUnavailable, wantCalled: true},
		{name: "enabled repair execution", task: asynq.NewTask(job.TypeMallWeatherRepair, nil), repairEnabled: true, checkErr: database.ErrUnavailable, wantChecked: true, wantErr: true},
		{name: "disabled repair schedule", task: asynq.NewTask(job.TypeMallWeatherSchedule, []byte(`{"task_type":"mall:weather:repair"}`)), checkErr: database.ErrUnavailable, wantCalled: true},
		{name: "invalid schedule", task: asynq.NewTask(job.TypeMallWeatherSchedule, []byte(`{"task_type":"unknown"}`)), checkErr: database.ErrUnavailable, wantChecked: true, wantErr: true},
		{name: "normal schedule", task: asynq.NewTask(job.TypeMallWeatherSchedule, []byte(`{"task_type":"mall:weather:fast","detail_profile":"full"}`)), checkErr: database.ErrUnavailable, wantChecked: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			checked := false
			next := asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
				called = true
				return nil
			})
			handler := requireQueueDatabaseAvailability(func(context.Context) error {
				checked = true
				return tt.checkErr
			}, tt.repairEnabled)(next)

			err := handler.ProcessTask(t.Context(), tt.task)
			if checked != tt.wantChecked {
				t.Fatalf("database checked = %t, want %t", checked, tt.wantChecked)
			}
			if called != tt.wantCalled {
				t.Fatalf("handler called = %t, want %t", called, tt.wantCalled)
			}
			if errors.Is(err, database.ErrUnavailable) != tt.wantErr {
				t.Fatalf("error = %v, want unavailable=%t", err, tt.wantErr)
			}
			if tt.wantErr {
				var hint interface{ RetryDelay() time.Duration }
				if !errors.As(err, &hint) || hint.RetryDelay() != queueDatabaseUnavailableRetryDelay {
					t.Fatalf("retry hint = %v", err)
				}
			}
		})
	}
}
