package data_dao

import (
	"context"
	"encoding/json"
	"fmt"

	"gin-biz-web-api/model"
	"gorm.io/gorm"
)

// CreateFeishuMentionOutbox must receive the same transaction that saves the job.
func CreateFeishuMentionOutbox(ctx context.Context, tx *gorm.DB, row *model.FeishuDeliveryJob) error {
	if row == nil || row.ID == 0 || row.DispatchGeneration == 0 {
		return fmt.Errorf("feishu mention: persisted job and generation required")
	}
	payload, err := json.Marshal(struct {
		JobID              uint `json:"job_id"`
		DispatchGeneration uint `json:"dispatch_generation"`
	}{row.ID, row.DispatchGeneration})
	if err != nil {
		return err
	}
	_, err = NewAsyncJobOutboxDAO(tx).CreateBatchIgnoreTaskConflicts(ctx, []model.AsyncJobOutbox{{TaskKey: fmt.Sprintf("feishu-mention:%d:g:%d", row.ID, row.DispatchGeneration), TaskType: "feishu:mention:deliver", PayloadJSON: model.JSONText(payload), QueueName: "delivery", AvailableAt: row.ScheduledAt}})
	return err
}
