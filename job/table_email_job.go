package job

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/hibiken/asynq"
	"io"
	"time"
)

const (
	TypeTableEmailScan     = "table-email:scan"
	TypeTableEmailDelivery = "table-email:deliver"
	TableEmailQueue        = "delivery"
)

type TableEmailDeliveryPayload struct {
	DeliveryID   uint   `json:"delivery_id"`
	RetryVersion uint64 `json:"retry_version"`
}

func NewTableEmailScanTask() *asynq.Task {
	return asynq.NewTask(TypeTableEmailScan, []byte(`{}`), asynq.Queue(TableEmailQueue), asynq.Timeout(55*time.Second), asynq.MaxRetry(0))
}
func DecodeTableEmailDeliveryPayload(data []byte) (TableEmailDeliveryPayload, error) {
	var payload TableEmailDeliveryPayload
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.DeliveryID == 0 {
		return payload, fmt.Errorf("table email: invalid delivery payload")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return payload, fmt.Errorf("table email: invalid trailing payload")
	}
	return payload, nil
}
func NewTableEmailDeliveryTask(data []byte) (*asynq.Task, error) {
	if _, err := DecodeTableEmailDeliveryPayload(data); err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeTableEmailDelivery, data, asynq.Queue(TableEmailQueue)), nil
}
func TableEmailOutboxTaskDefinitions() []OutboxTaskDefinition {
	return []OutboxTaskDefinition{{TaskType: TypeTableEmailDelivery, Queue: TableEmailQueue, MaxRetry: 5, Timeout: time.Minute, Build: NewTableEmailDeliveryTask}}
}
