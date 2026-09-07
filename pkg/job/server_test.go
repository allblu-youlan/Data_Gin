package job

import (
	"errors"
	"testing"
	"time"

	"gin-biz-web-api/pkg/database"

	"github.com/hibiken/asynq"
)

type fixedRetryDelayError struct{ delay time.Duration }

func (err fixedRetryDelayError) Error() string             { return "retry later" }
func (err fixedRetryDelayError) RetryDelay() time.Duration { return err.delay }

func TestRetryDelayUsesErrorHint(t *testing.T) {
	task := asynq.NewTask("report:run", nil)
	got := retryDelay(7, errors.Join(errors.New("wrapped"), fixedRetryDelayError{delay: 15 * time.Second}), task)
	if got != 15*time.Second {
		t.Fatalf("retryDelay() = %s, want 15s", got)
	}
}

func TestDatabaseUnavailableDoesNotConsumeTaskRetryBudget(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "success", err: nil, want: false},
		{name: "database unavailable", err: errors.Join(errors.New("queue paused"), database.ErrUnavailable), want: false},
		{name: "task failure", err: errors.New("task failed"), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTaskFailure(tt.err); got != tt.want {
				t.Fatalf("isTaskFailure() = %t, want %t", got, tt.want)
			}
		})
	}
}
