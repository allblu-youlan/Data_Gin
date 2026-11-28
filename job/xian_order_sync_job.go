package job

import (
	"context"
	"encoding/json"
	"fmt"

	"gin-biz-web-api/Trigger"
	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const TypeXianOrderSync = "xian:order:sync"

const XianOrderSyncQueueName = "gin-biz-web-api"

type XianOrderSyncPayload struct {
	ShopCode string
	Status   string
}

func NewXianOrderSyncTask(params XianOrderSyncPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TypeXianOrderSync,
		payload,
		asynq.Queue(XianOrderSyncQueueName),
		asynq.MaxRetry(3),
	), nil
}

func HandleXianOrderSyncTask(ctx context.Context, t *asynq.Task) error {
	var payload XianOrderSyncPayload
	if err := DecodeTaskPayload(t, &payload); err != nil {
		return err
	}

	logger.Info(
		"开始处理 HandleXianOrderSyncTask 任务",
		zap.String("ShopCode", payload.ShopCode),
		zap.String("Status", payload.Status),
	)

	trigger := Trigger.NewXianOrderTrigger()
	err := trigger.Trigger(ctx)
	if err != nil {
		return fmt.Errorf("sync xian orders shop_code=%q status=%q: %w", payload.ShopCode, payload.Status, err)
	}

	logger.Info(
		"西岸野选订单同步任务执行成功",
		zap.String("ShopCode", payload.ShopCode),
	)

	return nil
}
