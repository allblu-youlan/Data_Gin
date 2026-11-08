package job

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gin-biz-web-api/Trigger"
	"gin-biz-web-api/internal/dao/data_dao"
)

type BackfillResult struct {
	OrderCount  int
	RefundCount int
	StartTime   string
	EndTime     string
}

func BackfillYouzanOrders(ctx context.Context, startTime, endTime string) (int, error) {
	log.Printf("开始补拉有赞订单: %s ~ %s", startTime, endTime)

	token, err := Trigger.GetYouzanAccessTokenContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取access_token失败: %w", err)
	}

	orders, err := Trigger.GetYouzanOrders(token, startTime, endTime)
	if err != nil {
		return 0, fmt.Errorf("获取订单失败: %w", err)
	}

	extractedOrders := Trigger.ExtractOrderDetails(orders)
	adjustedOrders := Trigger.PriceChangeFree(extractedOrders, token)

	orderDAO := data_dao.NewYouzanOrderDAO()
	savedCount := 0
	for i, order := range adjustedOrders {
		modelOrder, err := ConvertToModel(order, orders)
		if err != nil {
			log.Printf("[%d/%d] 转换订单失败, tid=%s: %v", i+1, len(adjustedOrders), order.TID, err)
			continue
		}

		if err := orderDAO.CreateOrUpdate(ctx, modelOrder); err != nil {
			log.Printf("[%d/%d] 保存订单失败, tid=%s: %v", i+1, len(adjustedOrders), modelOrder.TID, err)
			continue
		}
		savedCount++
	}

	log.Printf("订单补拉完成, 共处理 %d 条, 成功保存 %d 条", len(adjustedOrders), savedCount)
	return savedCount, nil
}

func BackfillYouzanRefundOrders(ctx context.Context, startTimeUnix, endTimeUnix int64) (int, error) {
	log.Printf("开始补拉有赞退款订单: unix[%d ~ %d]", startTimeUnix, endTimeUnix)

	token, err := Trigger.GetYouzanAccessTokenContext(ctx)
	if err != nil {
		return 0, fmt.Errorf("获取access_token失败: %w", err)
	}

	refundOrders, err := Trigger.GetYouzanRefundOrdersByRange(token, startTimeUnix, endTimeUnix)
	if err != nil {
		return 0, fmt.Errorf("获取退款订单失败: %w", err)
	}

	refundDAO := data_dao.NewYouzanReturnDAO()
	savedCount := 0
	for i, refund := range refundOrders {
		modelOrder := ConvertRefundToModel(refund)

		if err := refundDAO.CreateOrUpdate(ctx, modelOrder); err != nil {
			log.Printf("[%d/%d] 保存退款订单失败, refund_id=%s: %v",
				i+1, len(refundOrders), modelOrder.RefundID, err)
			continue
		}
		savedCount++
	}

	log.Printf("退款订单补拉完成, 共处理 %d 条, 成功保存 %d 条", len(refundOrders), savedCount)
	return savedCount, nil
}

func BackfillYouzanForDate(ctx context.Context, dateStr string) (*BackfillResult, error) {
	startTime := fmt.Sprintf("%s 00:00:00", dateStr)
	endTime := fmt.Sprintf("%s 23:59:59", dateStr)
	return BackfillYouzanByRange(ctx, startTime, endTime)
}

func BackfillYouzanByRange(ctx context.Context, startTime, endTime string) (*BackfillResult, error) {
	result := &BackfillResult{
		StartTime: startTime,
		EndTime:   endTime,
	}

	orderCount, err := BackfillYouzanOrders(ctx, startTime, endTime)
	if err != nil {
		log.Printf("订单补拉失败: %v", err)
		return result, err
	}
	result.OrderCount = orderCount

	startT, err1 := parseDateTimeToUnix(startTime)
	endT, err2 := parseDateTimeToUnix(endTime)
	if err1 != nil || err2 != nil {
		log.Printf("跳过退款补拉: 时间解析失败 start=%v, end=%v", err1, err2)
		return result, nil
	}

	refundCount, err := BackfillYouzanRefundOrders(ctx, startT, endT)
	if err != nil {
		log.Printf("退款订单补拉失败: %v", err)
		return result, err
	}
	result.RefundCount = refundCount

	return result, nil
}

func parseDateTimeToUnix(dateTimeStr string) (int64, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	cleanStr := strings.TrimSpace(dateTimeStr)

	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}

	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, cleanStr, loc); err == nil {
			return t.Unix(), nil
		}
	}

	return 0, fmt.Errorf("无法解析时间: %s", dateTimeStr)
}

func ParseDateTimeToUnix(dateTimeStr string) (int64, error) {
	return parseDateTimeToUnix(dateTimeStr)
}
