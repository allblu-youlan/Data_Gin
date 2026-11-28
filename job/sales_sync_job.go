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

const TypeSalesSync = "sales:sync"

const SalesSyncQueueName = "gin-biz-web-api"

type SalesSyncPayload struct {
	ShopCode     string
	Status       string
	StoreCode    string
	MallItemCode string
}

func NewSalesSyncTask(params SalesSyncPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TypeSalesSync,
		payload,
		asynq.Queue(SalesSyncQueueName),
		asynq.MaxRetry(3),
	), nil
}

func HandleSalesSyncTask(ctx context.Context, t *asynq.Task) error {
	var payload SalesSyncPayload
	if err := DecodeTaskPayload(t, &payload); err != nil {
		return err
	}

	logger.Info(
		"开始处理 HandleSalesSyncTask 任务",
		zap.String("ShopCode", payload.ShopCode),
		zap.String("Status", payload.Status),
		zap.String("StoreCode", payload.StoreCode),
		zap.String("MallItemCode", payload.MallItemCode),
	)

	trigger := Trigger.NewSalesSyncTrigger()
	var err error
	if payload.ShopCode != "" && payload.Status != "" && payload.StoreCode != "" && payload.MallItemCode != "" {
		err = trigger.TriggerWithParams(ctx, payload.ShopCode, payload.Status, payload.StoreCode, payload.MallItemCode)
	} else {
		err = trigger.Trigger(ctx)
	}

	if err != nil {
		return fmt.Errorf("sync sales shop_code=%q status=%q: %w", payload.ShopCode, payload.Status, err)
	}

	logger.Info(
		"销售数据同步任务执行成功",
		zap.String("ShopCode", payload.ShopCode),
		zap.String("Status", payload.Status),
	)

	return nil
}
