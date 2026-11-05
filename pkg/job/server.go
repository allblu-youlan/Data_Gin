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
	logger.Warn("Asynq task will retry", fields...)
}

func isTaskFailure(err error) bool {
	return err != nil && !errors.Is(err, database.ErrUnavailable)
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
