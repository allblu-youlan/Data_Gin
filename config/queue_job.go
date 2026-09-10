package config

import (
	"os"
	"strconv"
	"strings"

	"gin-biz-web-api/pkg/config"

	"github.com/spf13/cast"
)

const EnvReportWorkerEnabled = "REPORT_WORKER_ENABLED"

func init() {

	config.Add("cfg.queue_job", func() map[string]interface{} {
		defaultQueues := map[string]int{
			"critical":        6,
			"default":         3,
			"low":             1,
			"gin-biz-web-api": 5,
			"weather":         5,
			"export":          2,
			"delivery":        2,
		}
		return map[string]interface{}{
			"redis": map[string]interface{}{
				"host":     config.Get("QueueJob.Redis.Host", "127.0.0.1"),
				"port":     config.Get("QueueJob.Redis.Port", 6379),
				"username": config.Get("QueueJob.Redis.Username", ""),
				"password": config.Get("QueueJob.Redis.Password", ""),
				"db":       config.Get("QueueJob.Redis.DB", 3),
			},
			"config_opt": map[string]interface{}{
				// 指定使用多少个并发工作进程
				"concurrency": config.Get("QueueJob.ConfigOpt.Concurrency", 10),
				// 可选地指定多个具有不同优先级的队列
				// 由于 asynq 包，不允许修改 redis 队列前缀（默认前缀是 `asynq`），因此
				// 如果是多个项目同时运行时，可以考虑以项目名称作为 key 来使指定项目只消费指定项目中的队列
				// 比如：有两个项目 project1 和 project2
				// 那么，可以设定为
				// project1 中
				// 				"queues": map[string]int{
				//					"project1": 6,
				//				},
				// project2 中
				// 				"queues": map[string]int{
				//					"project2": 6,
				//				},
				// 然后加入队列时：
				// project1 中 asynq.NewTask(taskType, taskPayload, asynq.Queue("project1"))
				// project2 中 asynq.NewTask(taskType, taskPayload, asynq.Queue("project2"))
				"queues": cast.ToStringMapInt(config.Get("QueueJob.ConfigOpt.Queues", defaultQueues)),
			},
			"outbox": map[string]interface{}{
				"poll_interval_ms":          config.Get("QueueJob.Outbox.PollIntervalMS", 1000),
				"idle_poll_max_interval_ms": config.Get("QueueJob.Outbox.IdlePollMaxIntervalMS", 10000),
				"lock_timeout_seconds":      config.Get("QueueJob.Outbox.LockTimeoutSeconds", 60),
				"batch_size":                config.Get("QueueJob.Outbox.BatchSize", 100),
				"retry_base_seconds":        config.Get("QueueJob.Outbox.RetryBaseSeconds", 5),
				"retry_max_seconds":         config.Get("QueueJob.Outbox.RetryMaxSeconds", 300),
			},
			"report_worker": map[string]interface{}{
				"enabled":             reportWorkerEnabled(),
				"queue_weight":        reportWorkerQueueWeight(),
				"run_concurrency":     reportWorkerConcurrency("QueueJob.ReportWorker.RunConcurrency", 4),
				"export_concurrency":  reportWorkerConcurrency("QueueJob.ReportWorker.ExportConcurrency", 2),
				"cleanup_concurrency": reportWorkerConcurrency("QueueJob.ReportWorker.CleanupConcurrency", 1),
			},
		}
	})

}

func reportWorkerEnabled() bool {
	raw, exists := os.LookupEnv(EnvReportWorkerEnabled)
	if exists && strings.TrimSpace(raw) != "" {
		enabled, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return false
		}
		return enabled
	}
	if instance := config.Instance(); instance != nil && instance.IsSet("QueueJob.ReportWorker.Enabled") {
		return instance.GetBool("QueueJob.ReportWorker.Enabled")
	}
	return true
}

func reportWorkerQueueWeight() int {
	if instance := config.Instance(); instance != nil && instance.IsSet("QueueJob.ReportWorker.QueueWeight") {
		return instance.GetInt("QueueJob.ReportWorker.QueueWeight")
	}
	return 2
}

func reportWorkerConcurrency(path string, fallback int) int {
	if instance := config.Instance(); instance != nil && instance.IsSet(path) {
		return instance.GetInt(path)
	}
	return fallback
}
