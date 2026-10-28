package bootstrap

import (
	"context"
	"fmt"
	"gin-biz-web-api/internal/service/data_svc"
	"gin-biz-web-api/job"
	"gin-biz-web-api/pkg/console"
	"github.com/hibiken/asynq"
	"time"
)

func registerTableEmailScheduledTasks(scheduler scheduledTaskRegistrar) {
	if scheduler == nil {
		return
	}
	if _, err := scheduler.Register("* * * * *", job.NewTableEmailScanTask(), asynq.Unique(50*time.Second)); err != nil {
		console.Warning("Failed to register table email scan: %v", err)
	}
}
func registerTableEmailWorkers(mux *asynq.ServeMux) {
	service := data_svc.NewTableEmailService()
	mux.HandleFunc(job.TypeTableEmailScan, func(ctx context.Context, task *asynq.Task) error {
		if err := job.DecodeOfficePushScheduleTaskPayload(task.Payload()); err != nil {
			return fmt.Errorf("%w: invalid table email scan", asynq.SkipRetry)
		}
		return service.Scan(ctx)
	})
	mux.HandleFunc(job.TypeTableEmailDelivery, func(ctx context.Context, task *asynq.Task) error {
		payload, err := job.DecodeTableEmailDeliveryPayload(task.Payload())
		if err != nil {
			return fmt.Errorf("%w: invalid table email delivery", asynq.SkipRetry)
		}
		retry, _ := asynq.GetRetryCount(ctx)
		maxRetry, _ := asynq.GetMaxRetry(ctx)
		return service.Deliver(ctx, payload, retry < maxRetry)
	})
}
