package job

import (
	"github.com/hibiken/asynq"
	"time"
)

const TypeReportBojunSync = "report:bojun_sync"

func ReportBojunSyncOutboxTaskDefinitions() []OutboxTaskDefinition {
	return []OutboxTaskDefinition{{TaskType: TypeReportBojunSync, Queue: ReportQueueName, Timeout: 30 * time.Minute, MaxRetry: 3, RecoverTaskIDConflict: true, Build: func(payload []byte) (*asynq.Task, error) {
		if _, e := DecodeReportRunTaskPayload(payload); e != nil {
			return nil, e
		}
		return asynq.NewTask(TypeReportBojunSync, payload, asynq.Queue(ReportQueueName)), nil
	}}}
}
