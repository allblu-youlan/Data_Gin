package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"gin-biz-web-api/job"
	"github.com/hibiken/asynq"
)

func TestBusinessQueueHandlersRejectMalformedPayloadWithoutRetry(t *testing.T) {
	for _, test := range []struct {
		taskType string
		handler  asynq.HandlerFunc
	}{
		{job.TypeDeliveryTaskRun, handleDeliveryTaskRun},
		{job.TypeExcelMatchExport, handleExcelMatchExport},
		{job.TypeBojunOrderFetch, handleBojunOrderFetch},
		{job.TypeYouzanDistributionOrderSync, handleYouzanDistributionOrderSync},
	} {
		t.Run(test.taskType, func(t *testing.T) {
			err := test.handler(context.Background(), asynq.NewTask(test.taskType, []byte(`{"broken":`)))
			var syntaxErr *json.SyntaxError
			if !errors.Is(err, asynq.SkipRetry) || !errors.As(err, &syntaxErr) {
				t.Fatalf("invalid JSON must retain its cause and skip retries: %v", err)
			}
		})
	}
}
