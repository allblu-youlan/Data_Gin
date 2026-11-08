package job

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"gin-biz-web-api/Trigger"
	"gin-biz-web-api/internal/dao/data_dao"
	"gin-biz-web-api/model"
	"gin-biz-web-api/pkg/logger"

	"github.com/hibiken/asynq"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const TypeYouzanSync = "youzan:sync"

const YouzanSyncQueueName = "gin-biz-web-api"

type YouzanSyncPayload struct {
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

func NewYouzanSyncTask(params YouzanSyncPayload) (*asynq.Task, error) {
	payload, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return asynq.NewTask(
		TypeYouzanSync,
		payload,
		asynq.Queue(YouzanSyncQueueName),
		asynq.MaxRetry(3),
	), nil
}

func HandleYouzanSyncTask(ctx context.Context, t *asynq.Task) error {
	var payload YouzanSyncPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return errors.Errorf("json.Unmarshal failed: %v: %v", err, asynq.SkipRetry)
	}

	logger.Info(
		"开始处理 HandleYouzanSyncTask 任务",
		zap.String("StartTime", payload.StartTime),
		zap.String("EndTime", payload.EndTime),
	)

	var startTime, endTime string
	if payload.StartTime != "" && payload.EndTime != "" {
		startTime = payload.StartTime
		endTime = payload.EndTime
	} else {
		now := time.Now()
		startTime = now.Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
		endTime = now.Format("2006-01-02 15:04:05")
	}

	err := SyncYouzanOrders(ctx, startTime, endTime)
	if err != nil {
		logger.Error(
			"有赞订单同步任务执行失败",
			zap.Error(err),
			zap.String("StartTime", startTime),
			zap.String("EndTime", endTime),
		)
		return err
	}

	logger.Info(
		"有赞订单同步任务执行成功",
		zap.String("StartTime", startTime),
		zap.String("EndTime", endTime),
	)

	return nil
}

func SyncYouzanOrders(ctx context.Context, startTime, endTime string) error {
	token, err := Trigger.GetYouzanAccessTokenContext(ctx)
	if err != nil {
		return fmt.Errorf("获取access_token失败: %w", err)
	}

	orders, err := Trigger.GetYouzanOrders(token, startTime, endTime)
	if err != nil {
		return fmt.Errorf("获取订单失败: %w", err)
	}

	extractedOrders := Trigger.ExtractOrderDetails(orders)
	adjustedOrders := Trigger.PriceChangeFree(extractedOrders, token)

	dao := data_dao.NewYouzanOrderDAO()
	savedCount := 0
	for _, order := range adjustedOrders {
		modelOrder, err := ConvertToModel(order, orders)
		if err != nil {
			log.Printf("转换订单失败: %v", err)
			continue
		}

		if err := dao.CreateOrUpdate(ctx, modelOrder); err != nil {
			log.Printf("保存订单失败, tid=%s: %v", modelOrder.TID, err)
			continue
		}
		savedCount++
	}

	log.Printf("成功同步 %d 条有赞订单数据", savedCount)
	return nil
}

func ConvertToModel(order Trigger.ExtractedOrder, rawOrders []map[string]interface{}) (*model.YOUZAN_ORDER_DATA, error) {
	var rawOrder map[string]interface{}
	for _, o := range rawOrders {
		orderInfo, _ := o["order_info"].(map[string]interface{})
		if tid, _ := orderInfo["tid"].(string); tid == order.TID {
			rawOrder = o
			break
		}
	}

	modelOrder := &model.YOUZAN_ORDER_DATA{
		TID:        order.TID,
		Payment:    order.Payment,
		TotalFee:   order.TotalFee,
		IsRefund:   order.IsRefund == "ONLINEREFUND",
		CashierID:  order.CashierID,
		Platform:   order.Platform,
		PayEndTime: order.PayEndTime,
		TotalAmt:   order.TotalAmt,
	}

	if rawOrder != nil {
		orderInfo, _ := rawOrder["order_info"].(map[string]interface{})
		payInfo, _ := rawOrder["pay_info"].(map[string]interface{})
		sourceInfo, _ := rawOrder["source_info"].(map[string]interface{})
		addressInfo, _ := rawOrder["address_info"].(map[string]interface{})
		buyerInfo, _ := rawOrder["buyer_info"].(map[string]interface{})

		modelOrder.Status, _ = orderInfo["status"].(string)
		modelOrder.StatusStr, _ = orderInfo["status_str"].(string)
		modelOrder.Type = intVal(orderInfo["type"])
		modelOrder.ShopName, _ = orderInfo["shop_name"].(string)
		modelOrder.NodeKdtID = int64Val(orderInfo["node_kdt_id"])
		modelOrder.RootKdtID = int64Val(orderInfo["root_kdt_id"])
		modelOrder.PayType = intVal(orderInfo["pay_type"])
		modelOrder.PayTypeStr, _ = orderInfo["pay_type_str"].(string)
		modelOrder.IsRetailOrder = boolVal(orderInfo["is_retail_order"])
		modelOrder.TeamType = intVal(orderInfo["team_type"])
		modelOrder.RefundState = intVal(orderInfo["refund_state"])
		modelOrder.CloseType = intVal(orderInfo["close_type"])
		modelOrder.ExpressType = intVal(orderInfo["express_type"])

		modelOrder.PayTime = parseTime(orderInfo["pay_time"])
		modelOrder.SuccessTime = parseTime(orderInfo["success_time"])
		modelOrder.ConsignTime = parseTime(orderInfo["consign_time"])
		modelOrder.ConfirmTime = parseTime(orderInfo["confirm_time"])
		modelOrder.ExpiredTime = parseTime(orderInfo["expired_time"])
		modelOrder.UpdateTime = parseTime(orderInfo["update_time"])
		modelOrder.Created = parseTime(orderInfo["created"])

		if orderExtra, _ := orderInfo["order_extra"].(map[string]interface{}); orderExtra != nil {
			modelOrder.SerialNo, _ = orderExtra["serial_no"].(string)
			modelOrder.IsMember = boolVal(orderExtra["is_member"])
		}

		if orderTags, _ := orderInfo["order_tags"].(map[string]interface{}); orderTags != nil {
			modelOrder.IsMember = boolVal(orderTags["is_member"])
			modelOrder.IsSettle = boolVal(orderTags["is_settle"])
			modelOrder.IsRefund = boolVal(orderTags["is_refund"])
			modelOrder.IsPayed = boolVal(orderTags["is_payed"])
		}

		if payInfo != nil {
			modelOrder.PostFee = floatVal(payInfo["post_fee"])
			if paymentStr, ok := payInfo["payment"].(string); ok {
				if paymentFloat, err := strconv.ParseFloat(paymentStr, 64); err == nil {
					modelOrder.AdjustmentPayment = paymentFloat
				}
			}
			if transactions, _ := payInfo["transaction"].([]interface{}); len(transactions) > 0 {
				jsonData, _ := json.Marshal(transactions)
				modelOrder.TransactionJSON = string(jsonData)
			}
			if outerTransactions, _ := payInfo["outer_transactions"].([]interface{}); len(outerTransactions) > 0 {
				jsonData, _ := json.Marshal(outerTransactions)
				modelOrder.OuterTransactionsJSON = string(jsonData)
			}
		}

		if sourceInfo != nil {
			modelOrder.IsOfflineOrder = boolVal(sourceInfo["is_offline_order"])
			modelOrder.OrderMark, _ = sourceInfo["order_mark"].(string)
			if source, _ := sourceInfo["source"].(map[string]interface{}); source != nil {
				modelOrder.Platform, _ = source["platform"].(string)
				modelOrder.WxEntrance, _ = source["wx_entrance"].(string)
			}
		}

		if addressInfo != nil {
			modelOrder.DeliveryProvince, _ = addressInfo["delivery_province"].(string)
			modelOrder.DeliveryCity, _ = addressInfo["delivery_city"].(string)
			modelOrder.DeliveryDistrict, _ = addressInfo["delivery_district"].(string)
			modelOrder.ReceiverName, _ = addressInfo["receiver_name"].(string)
			modelOrder.ReceiverTel, _ = addressInfo["receiver_tel"].(string)
		}

		if buyerInfo != nil {
			modelOrder.BuyerPhone, _ = buyerInfo["buyer_phone"].(string)
			modelOrder.FansNickname, _ = buyerInfo["fans_nickname"].(string)
			modelOrder.FansID = int64Val(buyerInfo["fans_id"])
			modelOrder.FansType = intVal(buyerInfo["fans_type"])
		}

		if ordersList, _ := rawOrder["orders"].([]interface{}); len(ordersList) > 0 {
			jsonData, _ := json.Marshal(ordersList)
			modelOrder.ItemsJSON = string(jsonData)
		}
	}

	return modelOrder, nil
}

func intVal(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case float64:
		return int(val)
	case string:
		i, _ := strconv.Atoi(val)
		return i
	default:
		return 0
	}
}

func int64Val(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case string:
		i, _ := strconv.ParseInt(val, 10, 64)
		return i
	default:
		return 0
	}
}

func floatVal(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return 0
	}
}

func boolVal(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	default:
		return false
	}
}

func parseTime(v interface{}) *time.Time {
	if v == nil {
		return nil
	}
	str, ok := v.(string)
	if !ok || str == "" {
		return nil
	}

	loc, _ := time.LoadLocation("Asia/Shanghai")
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.000",
		"2006/01/02 15:04:05",
	}

	for _, layout := range layouts {
		t, err := time.ParseInLocation(layout, str, loc)
		if err == nil {
			return &t
		}
	}

	return nil
}
