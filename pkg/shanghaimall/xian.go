package shanghaimall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

func PushQiantan(ctx context.Context, order RetailOrder) (*PushResult, error) {
	return pushXian(ctx, qiantanConfig(), order)
}

func pushXian(ctx context.Context, cfg xianConfig, order RetailOrder) (*PushResult, error) {
	if err := order.validate(); err != nil {
		return nil, err
	}
	if cfg.TokenURL == "" || cfg.PostURL == "" || cfg.Account == "" || cfg.Password == "" {
		return nil, fmt.Errorf("qiantan xian config is incomplete")
	}
	token, err := getXianToken(ctx, cfg)
	if err != nil {
		return loginFailureResult(TargetQiantan, err), err
	}

	payStyle, _ := json.Marshal(map[string]float64{"-1": order.Amount})
	body := map[string]interface{}{
		"shop_id":           cfg.ShopID,
		"branch_id":         cfg.BranchID,
		"commodity_details": "",
		"pay_style":         string(payStyle),
		"num":               cfg.Num,
		"sale_sum":          order.Amount,
		"bill_type":         "1",
		"sale_time":         order.SaleTime,
		"sale_number":       order.DocNo,
		"token":             token,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.PostURL, bytes.NewReader(bodyBytes))
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
		Target:       TargetQiantan,
		HTTPStatus:   resp.StatusCode,
		RequestBody:  body,
		ResponseBody: string(respBytes),
	}
	_ = json.Unmarshal(respBytes, &result.ResponseJSON)
	result.Success = result.ResponseJSON["result"] == "1"
	if !result.Success {
		return result, fmt.Errorf("qiantan xian push failed: %s", result.ResponseBody)
	}
	return result, nil
}

func getXianToken(ctx context.Context, cfg xianConfig) (string, error) {
	values := url.Values{}
	values.Set("account", cfg.Account)
	values.Set("pwd", cfg.Password)
	reqURL := cfg.TokenURL + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := cfg.Client.Do(req)
	if err != nil {
		return "", loginTransportError("qiantan xian", err)
	}
	defer resp.Body.Close()

	var payload map[string]interface{}
	if err := decodeLoginResponse("qiantan xian", resp, &payload); err != nil {
		return "", err
	}
	token, _ := payload["token"].(string)
	if token == "" {
		return "", fmt.Errorf("qiantan xian token is empty")
	}
	return token, nil
}
