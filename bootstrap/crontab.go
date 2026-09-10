package bootstrap

import (
	"context"
	"os"
	"strings"

	crontabTask "gin-biz-web-api/crontab"
	"gin-biz-web-api/global"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/crontab"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"

	robfigcron "github.com/robfig/cron/v3"
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

	addScheduleTask()

	task.Start()
}

// addScheduleTask 添加计划任务
func addScheduleTask() {

	// @daily 或者 @midnight 每天 0 点执行清理日志
	clearLogsCrontabEntryID, err := global.Crontab.AddJob("@daily", crontabTask.ClearLogsCrontab{})
	ifError(err, int(clearLogsCrontabEntryID), "ClearLogsCrontab")

	// 每30分钟执行数据采集
	dataCollectCrontabEntryID, err := global.Crontab.AddJob("0 */30 * * * *", databaseBackedCronJob(crontabTask.DataCollectCrontab{}))
	ifError(err, int(dataCollectCrontabEntryID), "DataCollectCrontab")

	bojunOrderCronExpr := resolveBojunOrderCronExpr()
	bojunOrderCrontabEntryID, err := global.Crontab.AddJob(bojunOrderCronExpr, databaseBackedCronJob(crontabTask.BojunOrderCrontab{}))
	ifError(err, int(bojunOrderCrontabEntryID), "BojunOrderCrontab")

}

type guardedDatabaseCronJob struct {
	next      robfigcron.Job
	available func(context.Context) bool
}

func databaseBackedCronJob(next robfigcron.Job) robfigcron.Job {
	return guardedDatabaseCronJob{next: next, available: database.CanServe}
}

func (job guardedDatabaseCronJob) Run() {
	if job.next == nil || job.available == nil || !job.available(context.Background()) {
		return
	}
	job.next.Run()
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
