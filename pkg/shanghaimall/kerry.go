package shanghaimall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const kerrySalesDocAlreadyExistsCode = 551510

type kerrySalesResponse struct {
	Error     *bool `json:"error"`
	ErrorCode int   `json:"errorCode"`
}

func PushJialiCheng(ctx context.Context, order RetailOrder) (*PushResult, error) {
	return pushKerry(ctx, kerryConfigFromEnv(), order)
}

func pushKerry(ctx context.Context, cfg kerryConfig, order RetailOrder) (*PushResult, error) {
	if err := order.validate(); err != nil {
		return nil, err
	}
	if err := validateKerryConfig(cfg); err != nil {
		return nil, err
	}
	apiKey, err := getKerryAPIKey(ctx, cfg)
	if err != nil {
		return nil, err
	}

	qty := order.normalizedQuantity()
	discount := order.ListAmount - order.Amount
	txDate := saleDate(order.SaleTime)
	txSerial, _ := strconv.Atoi(time.Now().Format("150405"))
	body := map[string]interface{}{
		"apiKey":             apiKey,
		"completeSalesTrans": true,
		"docKey":             order.DocNo,
		"refundItem":         nil,
		"refundTender":       nil,
		"salesItem": []map[string]interface{}{
			{
				"salesLineNumber":   0,
				"itemCode":          cfg.ItemCode,
				"inventoryType":     0,
				"qty":               qty,
				"itemDiscountLess":  money(discount),
				"totalDiscountLess": money(discount),
				"netAmount":         money(order.Amount),
				"originalPrice":     money(order.ListAmount),
				"sellingPrice":      money(order.Amount),
			},
		},
		"salesTender": []map[string]interface{}{
			{
				"tenderLineNum":    0,
				"baseCurrencyCode": "RMB",
				"tenderCode":       cfg.TenderCode,
				"payAmount":        money(order.Amount),
				"baseAmount":       money(order.Amount),
				"excessAmount":     0,
				"tenderType":       0,
			},
		},
		"salesTotal": map[string]interface{}{
			"cashier":   cfg.Cashier,
			"netQty":    qty,
			"netAmount": money(order.Amount),
		},
		"transHeader": map[string]interface{}{
			"ledgerDatetime": order.SaleTime,
			"programName":    cfg.ProgramName,
			"storeCode":      cfg.StoreCode,
			"tillId":         cfg.TillID,
			"docType":        "S",
			"staffCode":      cfg.StaffCode,
			"docNo":          order.DocNo,
			"txSerial":       txSerial,
			"txDate":         txDate,
		},
	}
	return postKerrySales(ctx, cfg, body)
}

func getKerryAPIKey(ctx context.Context, cfg kerryConfig) (string, error) {
	body := map[string]interface{}{
		"content": map[string]interface{}{
			"programName":    cfg.ProgramName,
			"deviceId":       cfg.DeviceID,
			"activationCode": cfg.ActivationCode,
			"locationCode":   cfg.LocationCode,
			"checkStoreCode": "true",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.LoginURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload map[string]interface{}
	if err := decodeAndDrainJSON(resp.Body, &payload); err != nil {
		return "", err
	}
	content, _ := payload["content"].(map[string]interface{})
	if content["status"] != "SUCCESS" {
		return "", fmt.Errorf("kerry login failed")
	}
	apiKey, _ := content["loginSecretId"].(string)
	if apiKey == "" {
		return "", fmt.Errorf("kerry api key is empty")
	}
	return apiKey, nil
}

func postKerrySales(ctx context.Context, cfg kerryConfig, body map[string]interface{}) (*PushResult, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.SalesURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	result := &PushResult{
		Target:       TargetJialiCheng,
		HTTPStatus:   resp.StatusCode,
		RequestBody:  body,
		ResponseBody: string(respBytes),
	}
	_ = json.Unmarshal(respBytes, &result.ResponseJSON)
	var response kerrySalesResponse
	_ = json.Unmarshal(respBytes, &response)
	result.Success = response.Error != nil && !*response.Error
	if response.ErrorCode == kerrySalesDocAlreadyExistsCode {
		result.Success = true
	}
	if !result.Success {
		return result, fmt.Errorf("kerry sales push failed: %s", result.ResponseBody)
	}
	return result, nil
}

func validateKerryConfig(cfg kerryConfig) error {
	if cfg.LoginURL == "" || cfg.SalesURL == "" || cfg.ProgramName == "" || cfg.DeviceID == "" {
		return fmt.Errorf("jialicheng kerry config is incomplete")
	}
	if cfg.ActivationCode == "" || cfg.LocationCode == "" || cfg.ItemCode == "" || cfg.Cashier == "" {
		return fmt.Errorf("jialicheng kerry config is incomplete")
	}
	if cfg.StoreCode == "" || cfg.TillID == "" || cfg.StaffCode == "" {
		return fmt.Errorf("jialicheng kerry config is incomplete")
	}
	return nil
}

func money(value float64) string {
	return fmt.Sprintf("%.2f", value)
}
