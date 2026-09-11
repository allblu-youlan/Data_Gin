package bootstrap

import (
	"context"
	"os"
	"strings"

	crontabTask "gin-biz-web-api/crontab"
	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/weather"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/crontab"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"
	"gin-biz-web-api/pkg/redis"

	"go.uber.org/zap"
)

// setupCrontab 启动定时任务
func setupCrontab() {
	if !config.GetBool("cfg.queue_job.crontab_enabled") {
		console.Info("Crontab disabled for this process role")
		return
	}

	console.Info("Crontab Start ...")

	task := crontab.NewTask(config.GetString("cfg.app.timezone"))
	global.Crontab = task

	cronLocker, err := weather.NewRedisTaskLocker(
		redis.Instance().Client,
		redis.GenNamespace("lock:cron:"),
		cronTaskLockTTL,
	)
	if err != nil {
		logger.Error("初始化定时任务分布式锁失败", zap.Error(err))
	}
	addScheduleTask(cronLocker)

	task.Start()
}

// addScheduleTask 添加计划任务
func addScheduleTask(locker weather.TaskLocker) {

	// @daily 或者 @midnight 每天 0 点执行清理日志
	clearLogsCrontabEntryID, err := global.Crontab.AddJob(
		"@daily",
		newDistributedCronJob("clear_logs", locker, crontabTask.ClearLogsCrontab{}),
	)
	ifError(err, int(clearLogsCrontabEntryID), "ClearLogsCrontab")

	// 每30分钟执行数据采集
	dataCollectCrontabEntryID, err := global.Crontab.AddJob(
		"0 */30 * * * *",
		newDistributedCronJob("data_collect", locker, databaseBackedCronJob(crontabTask.DataCollectCrontab{})),
	)
	ifError(err, int(dataCollectCrontabEntryID), "DataCollectCrontab")

	bojunOrderCronExpr := resolveBojunOrderCronExpr()
	bojunOrderCrontabEntryID, err := global.Crontab.AddJob(
		bojunOrderCronExpr,
		newDistributedCronJob("bojun_order", locker, databaseBackedCronJob(crontabTask.BojunOrderCrontab{})),
	)
	ifError(err, int(bojunOrderCrontabEntryID), "BojunOrderCrontab")

}

type guardedDatabaseCronJob struct {
	next      contextCronJob
	available func(context.Context) bool
}

func databaseBackedCronJob(next contextCronJob) guardedDatabaseCronJob {
	return guardedDatabaseCronJob{next: next, available: database.CanServe}
}

func (job guardedDatabaseCronJob) Run() {
	job.RunContext(context.Background())
}

func (job guardedDatabaseCronJob) RunContext(ctx context.Context) {
	if ctx == nil || ctx.Err() != nil || job.next == nil || job.available == nil || !job.available(ctx) {
		return
	}
	job.next.RunContext(ctx)
}

func resolveBojunOrderCronExpr() string {
	if value := strings.TrimSpace(os.Getenv("BOJUN_ORDER_CRON_EXPR")); value != "" {
		return value
	}
	return config.GetString("cfg.bojun.order_cron_expr", "0 */1 * * * *")
}

func ifError(err error, entryID int, taskName string) {
	if err != nil {
		logger.Error(
			"加入定时任务失败：",
			zap.String("task", taskName),
			zap.Int("entryID", entryID),
			zap.Error(err),
		)
	} else {
		logger.Info(
			"加入定时任务成功：",
			zap.String("task", taskName),
			zap.Int("entryID", entryID),
		)
	}
}
