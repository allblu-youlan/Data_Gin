package job

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/hibiken/asynq"
)

const (
	TypeFeishuMentionDeliver      = "feishu:mention:deliver"
	TypeFeishuMentionPlan         = "feishu:mention:plan"
	FeishuMentionQueueName        = "delivery"
	FeishuMentionPlanCron         = "* * * * *"
	FeishuMentionPlanTaskTimeout  = 50 * time.Second
	FeishuMentionPlanUniquePeriod = 50 * time.Second
)

type FeishuMentionTaskPayload struct {
	JobID              uint `json:"job_id"`
	DispatchGeneration uint `json:"dispatch_generation"`
}

func FeishuMentionTaskKey(id, generation uint) string {
	return fmt.Sprintf("feishu-mention:%d:g:%d", id, generation)
}
func DecodeFeishuMentionTaskPayload(payload []byte) (FeishuMentionTaskPayload, error) {
	var p FeishuMentionTaskPayload
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if err := d.Decode(&p); err != nil {
		return p, fmt.Errorf("feishu mention: invalid task payload: %w", err)
	}
	if err := d.Decode(&struct{}{}); err != io.EOF || p.JobID == 0 || p.DispatchGeneration == 0 {
		return p, fmt.Errorf("feishu mention: invalid task payload")
	}
	return p, nil
}
func NewFeishuMentionTask(payload []byte) (*asynq.Task, error) {
	if _, err := DecodeFeishuMentionTaskPayload(payload); err != nil {
		return nil, err
	}
	return asynq.NewTask(TypeFeishuMentionDeliver, append([]byte(nil), payload...), asynq.Queue(FeishuMentionQueueName)), nil
}
func NewFeishuMentionPlanTask() (*asynq.Task, error) {
	return asynq.NewTask(TypeFeishuMentionPlan, []byte(`{}`), asynq.Queue(FeishuMentionQueueName), asynq.Timeout(FeishuMentionPlanTaskTimeout)), nil
}
func FeishuMentionOutboxTaskDefinitions(maxAttempts int, timeout time.Duration) []OutboxTaskDefinition {
	if maxAttempts < 1 {
		maxAttempts = 5
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return []OutboxTaskDefinition{{TaskType: TypeFeishuMentionDeliver, Queue: FeishuMentionQueueName, MaxRetry: maxAttempts - 1, Timeout: timeout, Build: NewFeishuMentionTask}}
}
