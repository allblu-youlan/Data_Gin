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
		name       string
		taskType   string
		checkErr   error
		wantCalled bool
		wantErr    bool
	}{
		{name: "available", taskType: "database-task", wantCalled: true},
		{name: "unavailable", taskType: "database-task", checkErr: database.ErrUnavailable, wantErr: true},
		{name: "database independent", taskType: job.TypeFoo, checkErr: database.ErrUnavailable, wantCalled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			next := asynq.HandlerFunc(func(context.Context, *asynq.Task) error {
				called = true
				return nil
			})
			handler := requireQueueDatabaseAvailability(func(context.Context) error {
				return tt.checkErr
			})(next)

			err := handler.ProcessTask(t.Context(), asynq.NewTask(tt.taskType, nil))
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
