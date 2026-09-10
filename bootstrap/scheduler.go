package bootstrap

import (
	"context"
	"time"

	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"

	"github.com/hibiken/asynq"
)

func setupScheduler() {
	console.Info("Scheduler Start ...")

	redisHost := config.GetString("cfg.queue_job.redis.host")
	redisPort := config.GetString("cfg.queue_job.redis.port")
	redisUsername := config.GetString("cfg.queue_job.redis.username")
	redisPassword := config.GetString("cfg.queue_job.redis.password")
	redisDB := config.GetInt("cfg.queue_job.redis.db")
	redisAddr := redisHost + ":" + redisPort

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		console.Warning("Failed to load Asia/Shanghai timezone: %v", err)
		loc = time.FixedZone("CST", 8*60*60)
	}

	scheduler := asynq.NewScheduler(
		asynq.RedisClientOpt{
			Addr:     redisAddr,
			Username: redisUsername,
			Password: redisPassword,
			DB:       redisDB,
		},
		&asynq.SchedulerOpts{
			Location: loc,
		},
	)

	registerLegacyScheduledTasks(scheduler)

	registerDatabaseDeliveryTasks(scheduler)

	registerExcelMatchScheduledTasks(scheduler)

	registerReportScheduledTasks(scheduler, global.ReportCenterEnabledAtStartup)

	registerMallWeatherScheduledTasks(scheduler)

	registerOfficePushScheduledTasks(scheduler)

	go func(scheduler *asynq.Scheduler) {
		if err := scheduler.Run(); err != nil {
			console.Warning("Scheduler Failed: %v", err)
			console.Exit("Scheduler Failed %v", err)
		}
	}(scheduler)

	global.QueueJobScheduler = scheduler

	console.Success("Scheduler started successfully")
}

type scheduledTaskRegistrar interface {
	Register(string, *asynq.Task, ...asynq.Option) (string, error)
}

func registerOfficePushScheduledTasks(scheduler scheduledTaskRegistrar) {
	if scheduler == nil {
		return
	}
	task, err := job.NewOfficePushScheduleTask()
	if err != nil {
		console.Warning("Failed to create office push schedule task: %v", err)
		return
	}
	if _, err := scheduler.Register(job.OfficePushScheduleCron, task, asynq.Unique(job.OfficePushScheduleUniquePeriod)); err != nil {
		console.Warning("Failed to register office push schedule planner: %v", err)
	}
}

func registerReportScheduledTasks(scheduler scheduledTaskRegistrar, enabled bool) {
	if !enabled || scheduler == nil {
		return
	}
	cleanupTask, err := job.NewReportExportCleanupTask()
	if err != nil {
		console.Warning("Failed to create report export cleanup task: %v", err)
		return
	}
	if _, err := scheduler.Register(job.ReportExportCleanupCron, cleanupTask); err != nil {
		console.Warning("Failed to register report export cleanup: %v", err)
	}
	resultCleanupTask, err := job.NewReportResultCleanupTask()
	if err != nil {
		console.Warning("Failed to create report result cleanup task: %v", err)
		return
	}
	if _, err := scheduler.Register(job.ReportResultCleanupCron, resultCleanupTask); err != nil {
		console.Warning("Failed to register report result cleanup: %v", err)
	}
}

func registerExcelMatchScheduledTasks(scheduler *asynq.Scheduler) {
	cleanupTask, err := job.NewExcelMatchCleanupTask()
	if err != nil {
		console.Warning("Failed to create Excel match cleanup task: %v", err)
		return
	}
	if _, err := scheduler.Register(job.ExcelMatchCleanupCron, cleanupTask); err != nil {
		console.Warning("Failed to register Excel match cleanup: %v", err)
	}
}

func registerMallWeatherScheduledTasks(scheduler *asynq.Scheduler) {
	if !global.MallWeatherEnabledAtStartup {
		return
	}
	cleanupTask, err := job.NewMallWeatherExportCleanupTask()
	if err != nil {
		console.Warning("Failed to create mall weather export cleanup task: %v", err)
	} else if _, err := scheduler.Register(job.MallWeatherExportCleanupCron, cleanupTask); err != nil {
		console.Warning("Failed to register mall weather export cleanup: %v", err)
	}
	definitions, err := job.MallWeatherScheduleDefinitions(
		config.GetString("cfg.mall_weather.fast_cron"),
		config.GetString("cfg.mall_weather.full_cron"),
		config.GetBool("cfg.mall_weather.repair_enabled"),
	)
	if err != nil {
		console.Warning("Failed to build mall weather schedules: %v", err)
		return
	}
	for _, definition := range definitions {
		task, err := job.NewMallWeatherScheduleTask(definition.Payload)
		if err != nil {
			console.Warning("Failed to create mall weather schedule task: %v", err)
			continue
		}
		if _, err := scheduler.Register(definition.CronExpr, task); err != nil {
			console.Warning("Failed to register mall weather schedule %s/%s: %v",
				definition.Payload.TaskType, definition.Payload.DetailProfile, err)
		}
	}
}

func registerLegacyScheduledTasks(scheduler *asynq.Scheduler) {
	for _, definition := range job.ScheduledLegacyTaskDefinitions() {
		asynqTask, err := job.NewLegacyTask(definition.Code, definition.DefaultPayload)
		if err != nil {
			console.Warning("Failed to create legacy task %s: %v", definition.Code, err)
			continue
		}

		if _, err := scheduler.Register(definition.CronExpr, asynqTask); err != nil {
			console.Warning("Failed to register legacy task %s: %v", definition.Code, err)
		}
	}
}

func registerDatabaseDeliveryTasks(scheduler *asynq.Scheduler) {
	tasks, err := data_dao.NewDeliveryTaskDAO().FindEnabledScheduled(context.Background())
	if err != nil {
		console.Warning("Failed to load database delivery tasks: %v", err)
		return
	}

	for _, task := range tasks {
		asynqTask, err := job.NewDeliveryTaskRunTask(job.DeliveryTaskRunPayload{
			TaskID: task.ID,
		})
		if err != nil {
			console.Warning("Failed to create DeliveryTaskRun task %d: %v", task.ID, err)
			continue
		}

		if _, err := scheduler.Register(task.CronExpr, asynqTask); err != nil {
			console.Warning("Failed to register DeliveryTaskRun task %d: %v", task.ID, err)
		}
	}
}
