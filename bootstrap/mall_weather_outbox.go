package bootstrap

import (
	"context"
	"errors"
	"sync"
	"time"

	"gin-biz-web-api/global"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/job"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/console"
	"gin-biz-web-api/pkg/database"
	"gin-biz-web-api/pkg/logger"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

var outboxLifecycle struct {
	sync.Mutex
	cancel context.CancelFunc
	done   <-chan struct{}
}

var reportReconciliationLifecycle struct {
	sync.Mutex
	cancel context.CancelFunc
	done   <-chan struct{}
}

func startOutboxDispatcher(reportWorkerEnabled bool) {
	if database.DB == nil {
		console.Warning("Outbox dispatcher was not started: database is unavailable")
		return
	}
	if global.QueueJobClient == nil {
		console.Warning("Outbox dispatcher was not started: queue client is unavailable")
		return
	}

	maxAttempts := config.GetInt("cfg.mall_weather.max_attempts")
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	definitions := make([]job.OutboxTaskDefinition, 0)
	definitions = append(definitions, job.OfficePushOutboxTaskDefinitions()...)
	if reportWorkerEnabled {
		definitions = append(definitions, job.ReportOutboxTaskDefinitions(job.ReportRunMaxRetry)...)
		definitions = append(definitions, job.ReportExportOutboxTaskDefinitions(job.ReportExportMaxRetry)...)
	}
	if global.MallWeatherEnabledAtStartup {
		definitions = append(definitions, job.MallWeatherOutboxTaskDefinitions(
			maxAttempts-1,
			time.Duration(config.GetInt("cfg.mall_weather.task_timeout_seconds"))*time.Second,
		)...)
	}
	if len(definitions) == 0 {
		return
	}
	registry, err := job.NewOutboxTaskRegistry(definitions...)
	if err != nil {
		console.Warning("Outbox dispatcher was not started: %v", err)
		return
	}
	inspector := asynq.NewInspector(asynq.RedisClientOpt{
		Addr: redisAddrFromConfig(), Username: config.GetString("cfg.queue_job.redis.username"),
		Password: config.GetString("cfg.queue_job.redis.password"), DB: config.GetInt("cfg.queue_job.redis.db"),
	})
	dispatcher, err := job.NewOutboxDispatcher(
		data_dao.NewAsyncJobOutboxDAO(database.DB),
		job.NewAsynqTaskPublisher(global.QueueJobClient, inspector),
		registry,
		job.OutboxDispatcherConfig{
			WorkerID:     "outbox-" + uuid.NewString(),
			PollInterval: time.Duration(config.GetInt("cfg.queue_job.outbox.poll_interval_ms")) * time.Millisecond,
			LockTimeout:  time.Duration(config.GetInt("cfg.queue_job.outbox.lock_timeout_seconds")) * time.Second,
			BatchSize:    config.GetInt("cfg.queue_job.outbox.batch_size"),
			RetryBase:    time.Duration(config.GetInt("cfg.queue_job.outbox.retry_base_seconds")) * time.Second,
			RetryMax:     time.Duration(config.GetInt("cfg.queue_job.outbox.retry_max_seconds")) * time.Second,
			OnPublished: func(row model.AsyncJobOutbox, publishedAt time.Time) {
				if global.MallWeatherEnabledAtStartup {
					data_svc.RecordMallWeatherOutboxQueueLag(row, publishedAt)
				}
			},
			OnCycleError: func(err error) {
				logger.Error("Outbox dispatch cycle failed", zap.Error(err))
			},
			DatabaseAvailable: database.CanServe,
		},
	)
	if err != nil {
		_ = inspector.Close()
		console.Warning("Outbox dispatcher was not started: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	outboxLifecycle.Lock()
	if outboxLifecycle.cancel != nil {
		outboxLifecycle.Unlock()
		cancel()
		_ = inspector.Close()
		console.Warning("Outbox dispatcher is already running")
		return
	}
	outboxLifecycle.cancel = cancel
	outboxLifecycle.done = done
	outboxLifecycle.Unlock()

	go func() {
		defer close(done)
		defer func() { _ = inspector.Close() }()
		if err := dispatcher.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("Outbox dispatcher stopped", zap.Error(err))
		}
	}()
	if reportWorkerEnabled {
		startReportRunReconciler(data_svc.NewReportRunReconciler())
	}
	console.Success("Outbox dispatcher started successfully")
}

func redisAddrFromConfig() string {
	return config.GetString("cfg.queue_job.redis.host") + ":" + config.GetString("cfg.queue_job.redis.port")
}

func stopOutboxDispatcher() {
	stopReportRunReconciler()
	outboxLifecycle.Lock()
	cancel := outboxLifecycle.cancel
	done := outboxLifecycle.done
	outboxLifecycle.cancel = nil
	outboxLifecycle.done = nil
	outboxLifecycle.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
		console.Info("Outbox dispatcher stopped")
	case <-time.After(5 * time.Second):
		console.Warning("Timed out waiting for outbox dispatcher to stop")
	}
}

type reportRunReconciler interface {
	Run(context.Context) error
}

func startReportRunReconciler(reconciler reportRunReconciler) {
	if reconciler == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	reportReconciliationLifecycle.Lock()
	if reportReconciliationLifecycle.cancel != nil {
		reportReconciliationLifecycle.Unlock()
		cancel()
		return
	}
	reportReconciliationLifecycle.cancel = cancel
	reportReconciliationLifecycle.done = done
	reportReconciliationLifecycle.Unlock()
	console.Success("Report task reconciler started successfully")
	go func() {
		defer close(done)
		if err := reconciler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("Report run reconciler stopped", zap.Error(err))
		}
	}()
}

func stopReportRunReconciler() {
	reportReconciliationLifecycle.Lock()
	cancel := reportReconciliationLifecycle.cancel
	done := reportReconciliationLifecycle.done
	reportReconciliationLifecycle.cancel = nil
	reportReconciliationLifecycle.done = nil
	reportReconciliationLifecycle.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		console.Warning("Timed out waiting for report run reconciler to stop")
	}
}
