package job

import (
	"context"
	"errors"
	"time"

	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/providerhttp"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

var Server *asynq.Server

func NewAsynqServer(
	redisAddr,
	redisUserName,
	redisPassword string,
	redisDB,
	configConcurrency int,
	configQueues map[string]int,
) *asynq.Server {

	Server = asynq.NewServer(
		asynq.RedisClientOpt{
			Addr:     redisAddr,
			Username: redisUserName,
			Password: redisPassword,
			DB:       redisDB,
		},
		asynq.Config{
			Concurrency:    configConcurrency,
			Queues:         configQueues,
			RetryDelayFunc: retryDelay,
			IsFailure:      isTaskFailure,
			ErrorHandler:   asynq.ErrorHandlerFunc(logTaskError),
			Logger:         asynqLogger{},
		})

	return Server
}

func logTaskError(ctx context.Context, task *asynq.Task, err error) {
	if logger.Logger == nil || err == nil {
		return
	}
	taskID, _ := asynq.GetTaskID(ctx)
	queue, _ := asynq.GetQueueName(ctx)
	retryCount, _ := asynq.GetRetryCount(ctx)
	maxRetry, maxRetryKnown := asynq.GetMaxRetry(ctx)
	retryExhausted := maxRetryKnown && retryCount >= maxRetry
	retrySkipped := errors.Is(err, asynq.SkipRetry)
	panicFailure := asynq.IsPanicError(err)
	fields := []zap.Field{
		zap.String("task_type", task.Type()),
		zap.String("task_id", taskID),
		zap.String("queue", queue),
		zap.Int("retry_count", retryCount),
		zap.Int("max_retry", maxRetry),
		zap.Bool("retry_exhausted", retryExhausted),
		zap.Bool("retry_skipped", retrySkipped),
		zap.Bool("panic", panicFailure),
		zap.String("error", providerhttp.RedactText(err.Error())),
	}
	if retryExhausted || retrySkipped || panicFailure {
		logger.Error("Asynq task failed", fields...)
		return
	}
	if IsTaskDeferred(err) {
		fields = append(fields, zap.Float64("retry_delay_seconds", retryDelay(retryCount, err, task).Seconds()))
		logger.Info("Asynq task deferred", fields...)
		return
	}
	logger.Warn("Asynq task will retry", fields...)
}

func isTaskFailure(err error) bool {
	return err != nil && !errors.Is(err, database.ErrUnavailable) && !IsTaskDeferred(err)
}

// IsTaskDeferred identifies bounded waits that must not consume a failure retry.
// The task handler owns the wait deadline and must return a terminal error when it expires.
func IsTaskDeferred(err error) bool {
	if err == nil || errors.Is(err, asynq.SkipRetry) {
		return false
	}
	var deferred interface{ TaskDeferred() bool }
	return errors.As(err, &deferred) && deferred.TaskDeferred()
}

type retryDelayHint interface {
	RetryDelay() time.Duration
}

func retryDelay(retryCount int, err error, task *asynq.Task) time.Duration {
	var hint retryDelayHint
	if errors.As(err, &hint) {
		if delay := hint.RetryDelay(); delay > 0 {
			return delay
		}
	}
	return asynq.DefaultRetryDelayFunc(retryCount, err, task)
}
