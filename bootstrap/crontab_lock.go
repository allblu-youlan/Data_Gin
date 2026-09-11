package bootstrap

import (
	"context"
	"time"

	"gin-biz-web-api/internal/weather"
	"gin-biz-web-api/pkg/logger"

	"go.uber.org/zap"
)

const (
	cronTaskLockTTL       = 2 * time.Minute
	cronTaskRenewInterval = 40 * time.Second
	cronTaskRedisTimeout  = 3 * time.Second
)

type contextCronJob interface {
	RunContext(context.Context)
}

type distributedCronJob struct {
	ctx           context.Context
	next          contextCronJob
	locker        weather.TaskLocker
	key           string
	renewInterval time.Duration
	redisTimeout  time.Duration
}

func newDistributedCronJob(
	ctx context.Context,
	key string,
	locker weather.TaskLocker,
	next contextCronJob,
) distributedCronJob {
	return distributedCronJob{
		ctx:           ctx,
		next:          next,
		locker:        locker,
		key:           key,
		renewInterval: cronTaskRenewInterval,
		redisTimeout:  cronTaskRedisTimeout,
	}
}

func (job distributedCronJob) Run() {
	job.RunContext(job.ctx)
}

func (job distributedCronJob) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil || job.next == nil || job.locker == nil ||
		job.key == "" || job.renewInterval <= 0 || job.redisTimeout <= 0 {
		return
	}

	acquireCtx, cancelAcquire := context.WithTimeout(ctx, job.redisTimeout)
	lock, acquired, err := job.locker.Acquire(acquireCtx, job.key)
	cancelAcquire()
	if err != nil {
		logCronLockError("定时任务分布式锁获取失败", job.key, err)
		return
	}
	if !acquired || lock == nil {
		return
	}

	renewable, ok := lock.(weather.RenewableTaskLock)
	if !ok {
		job.releaseLock(ctx, lock)
		logCronLockError("定时任务分布式锁不支持续租", job.key, weather.ErrTaskLockLeaseLost)
		return
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	renewDone := make(chan struct{})
	go job.renewLock(runCtx, cancelRun, renewable, renewDone)
	defer func() {
		cancelRun()
		<-renewDone
		job.releaseLock(ctx, lock)
	}()

	job.next.RunContext(runCtx)
}

func (job distributedCronJob) renewLock(
	ctx context.Context,
	cancelRun context.CancelFunc,
	lock weather.RenewableTaskLock,
	done chan<- struct{},
) {
	defer close(done)

	ticker := time.NewTicker(job.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancelRenew := context.WithTimeout(ctx, job.redisTimeout)
			err := lock.Renew(renewCtx)
			cancelRenew()
			if err != nil {
				cancelRun()
				logCronLockError("定时任务分布式锁续租失败，取消本次执行", job.key, err)
				return
			}
		}
	}
}

func (job distributedCronJob) releaseLock(parent context.Context, lock weather.TaskLock) {
	if lock == nil {
		return
	}
	if parent == nil {
		parent = context.Background()
	}
	releaseCtx, cancelRelease := context.WithTimeout(context.WithoutCancel(parent), job.redisTimeout)
	defer cancelRelease()
	if err := lock.Release(releaseCtx); err != nil {
		logCronLockError("定时任务分布式锁释放失败", job.key, err)
	}
}

func logCronLockError(message, key string, err error) {
	if logger.Logger == nil {
		return
	}
	logger.Error(message, zap.String("task", key), zap.Error(err))
}
