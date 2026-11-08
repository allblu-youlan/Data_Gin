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
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const TypeYouzanReturn = "youzan:return"
const TypeYouzanRefundSync = "youzan:refund:sync"

const YouzanReturnQueueName = "gin-biz-web-api"

type YouzanReturnPayload struct {
	NodeKdtID int64 `json:"node_kdt_id"`
}

type YouzanRefundSyncPayload struct {
	NodeKdtID int64 `json:"node_kdt_id"`
}

func NewYouzanReturnTask(params YouzanReturnPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TypeYouzanReturn,
		payload,
		asynq.Queue(YouzanReturnQueueName),
		asynq.MaxRetry(3),
	), nil
}

func HandleYouzanReturnTask(ctx context.Context, t *asynq.Task) error {
	var payload YouzanReturnPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return errors.Errorf("json.Unmarshal failed: %v: %v", err, asynq.SkipRetry)
	}

	logger.Info(
		"开始处理 HandleYouzanReturnTask 任务",
		zap.Int64("NodeKdtID", payload.NodeKdtID),
	)

	nodeKdtID := payload.NodeKdtID
	if nodeKdtID == 0 {
		nodeKdtID = config.GetInt64("cfg.youzan.node_kdt_id")
	}

	err := SyncYouzanReturnOrders(ctx, nodeKdtID)
	if err != nil {
		logger.Error(
			"有赞退款订单同步任务执行失败",
			zap.Error(err),
			zap.Int64("NodeKdtID", nodeKdtID),
		)
		return err
	}

	logger.Info(
		"有赞退款订单同步任务执行成功",
		zap.Int64("NodeKdtID", nodeKdtID),
	)

	return nil
}

func NewYouzanRefundSyncTask(params YouzanRefundSyncPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TypeYouzanRefundSync,
		payload,
		asynq.Queue(YouzanReturnQueueName),
		asynq.MaxRetry(3),
	), nil
}

func HandleYouzanRefundSyncTask(ctx context.Context, t *asynq.Task) error {
	var payload YouzanRefundSyncPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return errors.Errorf("json.Unmarshal failed: %v: %v", err, asynq.SkipRetry)
	}

	logger.Info(
		"开始处理 HandleYouzanRefundSyncTask 任务",
		zap.Int64("NodeKdtID", payload.NodeKdtID),
	)

	nodeKdtID := payload.NodeKdtID
	if nodeKdtID == 0 {
		nodeKdtID = config.GetInt64("cfg.youzan.node_kdt_id")
	}

	err := SyncYouzanRefundToSales(ctx, nodeKdtID)
	if err != nil {
		logger.Error(
			"有赞退款同步到销售系统任务执行失败",
			zap.Error(err),
			zap.Int64("NodeKdtID", nodeKdtID),
		)
		return err
	}

	logger.Info(
		"有赞退款同步到销售系统任务执行成功",
		zap.Int64("NodeKdtID", nodeKdtID),
	)

	return nil
}

func SyncYouzanRefundToSales(ctx context.Context, nodeKdtID int64) error {
	dao := data_dao.NewYouzanReturnDAO()

	orders, err := dao.FindUnsyncedByNodeKdtIDAndStatus(ctx, nodeKdtID, "SUCCESS")
	if err != nil {
		return fmt.Errorf("查询未同步退款订单失败: %w", err)
	}

	if len(orders) == 0 {
		return nil
	}

	storecode := config.GetString("cfg.henglong.refund_store_code", "416201")
	mallitemcode := config.GetString("cfg.henglong.refund_mall_item_code", "E6600000099")
	salestype := "SR"

	dingtalkToken := config.GetString("cfg.dingtalk.default.token")
	dingtalkSecret := config.GetString("cfg.dingtalk.default.secret")

	for _, order := range orders {
		tid := order.RefundID
		payAmount := -order.RefundFee

		if payAmount >= 0 {
			log.Printf("退款订单 %s 金额为0或正数，跳过", tid)
			continue
		}

		response, err := send.SendSalesData(payAmount, tid, order.Modified, storecode, mallitemcode, salestype)
		if err != nil {
			log.Printf("发送退款数据失败, refund_id=%s: %v", tid, err)
			continue
		}

		uploadTime := time.Now().Format("2006-01-02 15:04:05")

		if send.Contains(response, "responsecode>0<") || send.Contains(response, "上传成功") {
			log.Printf("退款数据上传成功, refund_id=%s, amount=%.2f", tid, payAmount)

			message := fmt.Sprintf("上传杭州恒隆退款数据\n退款ID: %s\n金额: %.2f\n上传时间: %s",
				tid, payAmount, uploadTime)
			dingResp, err := send.SendDingTalkMessage(dingtalkToken, dingtalkSecret, message)
			if err != nil {
				log.Printf("发送钉钉消息失败: %v", err)
			} else {
				log.Printf("钉钉消息发送成功: %s", dingResp)
			}

			if err := dao.MarkAsSynced(ctx, order.ID); err != nil {
				log.Printf("标记退款订单已同步失败, refund_id=%s: %v", tid, err)
			}
		} else {
			log.Printf("退款数据上传失败, refund_id=%s, response=%s", tid, response)

			message := fmt.Sprintf("上传杭州恒隆退款数据失败\n退款ID: %s\n金额: %.2f\n上传时间: %s\n响应: %s",
				tid, payAmount, uploadTime, response)
			send.SendDingTalkMessage(dingtalkToken, dingtalkSecret, message)
		}
	}

	return nil
}

func SyncYouzanReturnOrders(ctx context.Context, nodeKdtID int64) error {
	token, err := Trigger.GetYouzanAccessTokenContext(ctx)
	if err != nil {
		return fmt.Errorf("获取access_token失败: %w", err)
	}

	refundOrders, err := Trigger.GetYouzanRefundOrders(token, nodeKdtID)
	if err != nil {
		return fmt.Errorf("获取退款订单失败: %w", err)
	}

	dao := data_dao.NewYouzanReturnDAO()
	savedCount := 0
	for _, refund := range refundOrders {
		modelOrder := ConvertRefundToModel(refund)

		if err := dao.CreateOrUpdate(ctx, modelOrder); err != nil {
			log.Printf("保存退款订单失败, tid=%s: %v", modelOrder.TID, err)
			continue
		}
		savedCount++
	}

	log.Printf("成功同步 %d 条有赞退款订单数据", savedCount)
	return nil
}

func ConvertRefundToModel(refund Trigger.RefundOrder) *model.YOUZAN_RETURN_DATA {
	createdTime := parseTimeStr(refund.Created)
	modifiedTime := parseTimeStr(refund.Modified)

	return &model.YOUZAN_RETURN_DATA{
		RefundID:       refund.RefundID,
		TID:            refund.TID,
		Status:         refund.Status,
		NodeKdtID:      refund.NodeKdtID,
		KdtID:          refund.KdtID,
		Reason:         refund.Reason,
		ReturnGoods:    refund.ReturnGoods,
		CSStatus:       refund.CSStatus,
		DeliveryStatus: refund.DeliveryStatus,
		RefundFee:      refund.RefundFee,
		Created:        createdTime,
		Modified:       modifiedTime,
	}
}

func parseTimeStr(timeStr string) *time.Time {
	if timeStr == "" {
		return nil
	}

	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.000",
		"2006/01/02 15:04:05",
	}

	for _, layout := range layouts {
		t, err := time.Parse(layout, timeStr)
		if err == nil {
			return &t
		}
	}

	return nil
}
