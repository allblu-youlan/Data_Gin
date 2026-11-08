package Trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"gin-biz-web-api/pkg/config"
	"gin-biz-web-api/pkg/youzan"
)

type YouzanTokenResponse struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		AccessToken string `json:"access_token"`
	} `json:"data"`
}

type YouzanOrderResponse struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		FullOrderInfoList []struct {
			FullOrderInfo struct {
				OrderInfo  map[string]interface{} `json:"order_info"`
				PayInfo    map[string]interface{} `json:"pay_info"`
				SourceInfo map[string]interface{} `json:"source_info"`
			} `json:"full_order_info"`
		} `json:"full_order_info_list"`
		Paginator struct {
			HasNext    bool   `json:"has_next"`
			NextCursor string `json:"next_cursor"`
		} `json:"paginator"`
	} `json:"data"`
}

type YouzanValueCardResponse struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		Items []struct {
			BonusPayAmount int `json:"bonus_pay_amount"`
		} `json:"items"`
	} `json:"data"`
}

type ExtractedOrder struct {
	CashierID   string  `json:"cashier_id"`
	TID         string  `json:"tid"`
	Platform    string  `json:"platform"`
	PayEndTime  string  `json:"pay_end_time"`
	Payment     float64 `json:"payment"`
	TotalFee    float64 `json:"total_fee"`
	CreatedTime string  `json:"created_time"`
	SuccessTime string  `json:"success_time"`
	IsRefund    string  `json:"is_refund"`
	TotalAmt    float64 `json:"totalAmt"`
}

type youzanOrderClient struct {
	httpClient *http.Client
	ordersURL  string
}

var youzanAccessTokenClient, youzanAccessTokenClientErr = youzan.NewTokenClient(nil)

func GetYouzanAccessToken() (string, error) {
	return GetYouzanAccessTokenContext(context.Background())
}

func GetYouzanAccessTokenContext(ctx context.Context) (string, error) {
	if youzanAccessTokenClientErr != nil {
		return "", youzanAccessTokenClientErr
	}
	return youzanAccessTokenClient.AccessToken(ctx, youzan.TokenConfig{
		URL: config.GetString("cfg.youzan.token_url"), ClientID: config.GetString("cfg.youzan.client_id"),
		ClientSecret: config.GetString("cfg.youzan.client_secret"), GrantID: config.GetString("cfg.youzan.grant_id"),
	})
}

func GetYouzanOrders(accessToken string, startTime, endTime string) ([]map[string]interface{}, error) {
	client := youzanOrderClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		ordersURL:  config.GetString("cfg.youzan.orders_url"),
	}

	return client.getOrders(accessToken, startTime, endTime)
}

func (client youzanOrderClient) getOrders(accessToken string, startTime, endTime string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s?access_token=%s", client.ordersURL, accessToken)
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	allOrders := []map[string]interface{}{}
	pageSize := 100
	pageNo := 1
	maxPages := 100

	for pageNo <= maxPages {
		params := map[string]interface{}{
			"page_size":     pageSize,
			"page_no":       pageNo,
			"start_created": startTime,
			"end_created":   endTime,
		}

		jsonData, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("序列化请求数据失败: %w", err)
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			return nil, fmt.Errorf("创建请求失败: %w", err)
		}

		for key, value := range headers {
			req.Header.Set(key, value)
		}

		resp, err := client.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("发送请求失败: %w", err)
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("读取响应失败: %w", err)
		}

		var result YouzanOrderResponse
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}

		if !result.Success {
			return nil, fmt.Errorf("API请求失败: %s", result.Message)
		}

		pageCount := len(result.Data.FullOrderInfoList)
		for _, item := range result.Data.FullOrderInfoList {
			orderMap := map[string]interface{}{
				"order_info":  item.FullOrderInfo.OrderInfo,
				"pay_info":    item.FullOrderInfo.PayInfo,
				"source_info": item.FullOrderInfo.SourceInfo,
			}
			allOrders = append(allOrders, orderMap)
		}

		if pageCount < pageSize {
			break
		}
		pageNo++
	}

	return allOrders, nil
}

func ExtractOrderDetails(fullOrderList []map[string]interface{}) []ExtractedOrder {
	var extractedData []ExtractedOrder

	for _, order := range fullOrderList {
		orderInfo, ok := order["order_info"].(map[string]interface{})
		if !ok {
			continue
		}

		payTypeStr, _ := orderInfo["pay_type_str"].(string)
		if containsStr(payTypeStr, "MARK_PAY_EXCHANGE") {
			continue
		}

		payInfo, ok := order["pay_info"].(map[string]interface{})
		if !ok {
			continue
		}

		sourceInfo, ok := order["source_info"].(map[string]interface{})
		if !ok {
			continue
		}

		var cashierID string
		if orderExtra, ok := orderInfo["order_extra"].(map[string]interface{}); ok {
			cashierID, _ = orderExtra["cashier_id"].(string)
		}

		var payEndTime string
		if phasePayments, ok := payInfo["phase_payments"].([]interface{}); ok && len(phasePayments) > 0 {
			if lastPayment, ok := phasePayments[len(phasePayments)-1].(map[string]interface{}); ok {
				payEndTime, _ = lastPayment["pay_end_time"].(string)
			}
		}

		isRefundFlag := false
		if orderTags, ok := orderInfo["order_tags"].(map[string]interface{}); ok {
			isRefundFlag, _ = orderTags["is_refund"].(bool)
		}

		paymentStr, _ := payInfo["payment"].(string)
		paymentFloat, _ := strconv.ParseFloat(paymentStr, 64)

		var totalAmt float64
		if isRefundFlag {
			totalAmt = -paymentFloat
		} else {
			totalAmt = paymentFloat
		}

		isRefundValue := "SALE"
		if isRefundFlag {
			isRefundValue = "ONLINEREFUND"
		}

		totalFeeStr, _ := payInfo["total_fee"].(string)
		totalFee, _ := strconv.ParseFloat(totalFeeStr, 64)

		var platform string
		if source, ok := sourceInfo["source"].(map[string]interface{}); ok {
			platform, _ = source["platform"].(string)
		}

		record := ExtractedOrder{
			CashierID:   cashierID,
			TID:         getString(orderInfo, "tid"),
			Platform:    platform,
			PayEndTime:  payEndTime,
			Payment:     paymentFloat,
			TotalFee:    totalFee,
			CreatedTime: getString(orderInfo, "created"),
			SuccessTime: getString(orderInfo, "success_time"),
			IsRefund:    isRefundValue,
			TotalAmt:    totalAmt,
		}

		extractedData = append(extractedData, record)
	}

	return extractedData
}

func PriceChangeFree(orderList []ExtractedOrder, accessToken string) []ExtractedOrder {
	url := fmt.Sprintf("%s?access_token=%s", config.GetString("cfg.youzan.value_card_url"), accessToken)
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	for i := range orderList {
		tid := orderList[i].TID
		if tid == "" {
			continue
		}

		params := map[string]interface{}{
			"page_size": 50,
			"page":      1,
			"trade_no":  tid,
		}

		jsonData, err := json.Marshal(params)
		if err != nil {
			continue
		}

		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			continue
		}

		for key, value := range headers {
			req.Header.Set(key, value)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var result YouzanValueCardResponse
		if err := json.Unmarshal(body, &result); err != nil {
			continue
		}

		if !result.Success {
			continue
		}

		bonusPayAmount := 0
		if len(result.Data.Items) > 0 {
			bonusPayAmount = result.Data.Items[0].BonusPayAmount
		}

		if bonusPayAmount > 0 {
			adjustedBonus := float64(bonusPayAmount) / 100
			orderList[i].Payment -= adjustedBonus
		}
	}

	return orderList
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func containsStr(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func GetTodayOrders() ([]ExtractedOrder, error) {
	token, err := GetYouzanAccessToken()
	if err != nil {
		return nil, fmt.Errorf("获取access_token失败: %w", err)
	}

	today := time.Now().Format("2006-01-02")
	startTime := fmt.Sprintf("%s 00:00:00", today)
	endTime := fmt.Sprintf("%s 23:59:59", today)

	orders, err := GetYouzanOrders(token, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("获取订单失败: %w", err)
	}

	extractedOrders := ExtractOrderDetails(orders)
	adjustedOrders := PriceChangeFree(extractedOrders, token)

	return adjustedOrders, nil
}
