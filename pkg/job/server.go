package job

import (
	"errors"
	"time"

	"gin-biz-web-api/pkg/database"

	"github.com/hibiken/asynq"
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
		})

	return Server
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
