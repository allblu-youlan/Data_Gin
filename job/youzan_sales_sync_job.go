package job

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"gin-biz-web-api/Trigger"
	send "gin-biz-web-api/Trigger/Send_Data"
	"gin-biz-web-api/internal/dao/data_dao"

	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const TypeYouzanSalesSync = "youzan:sales:sync"

type YouzanSalesSyncPayload struct {
	NodeKdtID int64 `json:"node_kdt_id"`
}

func NewYouzanSalesSyncTask(params YouzanSalesSyncPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TypeYouzanSalesSync,
		payload,
		asynq.Queue(YouzanReturnQueueName),
		asynq.MaxRetry(3),
	), nil
}

func HandleYouzanSalesSyncTask(ctx context.Context, t *asynq.Task) error {
	var payload YouzanSalesSyncPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return errors.Errorf("json.Unmarshal failed: %v: %v", err, asynq.SkipRetry)
	}

	nodeKdtID := payload.NodeKdtID
	if nodeKdtID == 0 {
		nodeKdtID = config.GetInt64("cfg.youzan.node_kdt_id")
	}

	logger.Info(
		"开始处理 HandleYouzanSalesSyncTask 任务",
		zap.Int64("NodeKdtID", nodeKdtID),
	)

	err := SyncYouzanSalesData(ctx, nodeKdtID)
	if err != nil {
		logger.Error(
			"有赞订单同步到销售系统任务执行失败",
			zap.Error(err),
			zap.Int64("NodeKdtID", nodeKdtID),
		)
		return err
	}

	logger.Info(
		"有赞订单同步到销售系统任务执行成功",
		zap.Int64("NodeKdtID", nodeKdtID),
	)

	return nil
}

func SyncYouzanSalesData(ctx context.Context, nodeKdtID int64) error {
	dao := data_dao.NewYouzanOrderDAO()

	orders, err := dao.FindUnsyncedByNodeKdtID(ctx, nodeKdtID)
	if err != nil {
		return fmt.Errorf("查询未同步订单失败: %w", err)
	}

	if len(orders) == 0 {
		return nil
	}

	token, err := Trigger.GetYouzanAccessTokenContext(ctx)
	if err != nil {
		return fmt.Errorf("获取access_token失败: %w", err)
	}

	storecode := config.GetString("cfg.henglong.refund_store_code", "416201")
	mallitemcode := config.GetString("cfg.henglong.refund_mall_item_code", "E6600000099")
	salestype := "SA"

	dingtalkToken := config.GetString("cfg.dingtalk.default.token")
	dingtalkSecret := config.GetString("cfg.dingtalk.default.secret")

	for _, order := range orders {
		adjustedOrders := []Trigger.ExtractedOrder{
			{
				TID:     order.TID,
				Payment: order.AdjustmentPayment,
			},
		}

		adjustedOrders = Trigger.PriceChangeFree(adjustedOrders, token)

		payAmount := adjustedOrders[0].Payment
		if payAmount <= 0 {
			log.Printf("订单 %s 金额为0或负数，跳过", order.TID)
			continue
		}

		response, err := send.SendSalesData(payAmount, order.TID, order.PayTime, storecode, mallitemcode, salestype)
		if err != nil {
			log.Printf("发送销售数据失败, tid=%s: %v", order.TID, err)
			continue
		}

		uploadTime := time.Now().Format("2006-01-02 15:04:05")

		if send.Contains(response, "responsecode>0<") || send.Contains(response, "上传成功") {
			log.Printf("销售数据上传成功, tid=%s, amount=%.2f", order.TID, payAmount)

			message := fmt.Sprintf("上传杭州恒隆销售数据\n订单号: %s\n金额: %.2f\n上传时间: %s",
				order.TID, payAmount, uploadTime)
			dingResp, err := send.SendDingTalkMessage(dingtalkToken, dingtalkSecret, message)
			if err != nil {
				log.Printf("发送钉钉消息失败: %v", err)
			} else {
				log.Printf("钉钉消息发送成功: %s", dingResp)
			}

			if err := dao.MarkAsSynced(ctx, order.ID); err != nil {
				log.Printf("标记订单已同步失败, tid=%s: %v", order.TID, err)
			}
		} else {
			log.Printf("销售数据上传失败, tid=%s, response=%s", order.TID, response)

			message := fmt.Sprintf("上传杭州恒隆销售数据失败\n订单号: %s\n金额: %.2f\n上传时间: %s\n响应: %s",
				order.TID, payAmount, uploadTime, response)
			send.SendDingTalkMessage(dingtalkToken, dingtalkSecret, message)
		}
	}

	return nil
}
