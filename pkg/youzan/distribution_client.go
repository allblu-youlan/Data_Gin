package youzan

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const maxDecryptBatchSize = 10000

type OrderTimeFilter string

const (
	OrderTimeFilterCreated OrderTimeFilter = "created"
	OrderTimeFilterSuccess OrderTimeFilter = "success"
)

type DistributionClient struct {
	orderURL   string
	decryptURL string
	httpClient *http.Client
}

func NewDistributionClient(orderURL, decryptURL string, httpClient *http.Client) *DistributionClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &DistributionClient{orderURL: orderURL, decryptURL: decryptURL, httpClient: httpClient}
}

func ParseOrderTimeFilter(value string) (OrderTimeFilter, error) {
	switch OrderTimeFilter(strings.TrimSpace(value)) {
	case "", OrderTimeFilterCreated:
		return OrderTimeFilterCreated, nil
	case OrderTimeFilterSuccess:
		return OrderTimeFilterSuccess, nil
	default:
		return "", fmt.Errorf("unsupported youzan order time filter %q", value)
	}
}

func (c *DistributionClient) FetchOrderPage(ctx context.Context, accessToken string, timeFilter OrderTimeFilter, startTime, endTime string, pageNo, pageSize int) ([]map[string]any, bool, error) {
	startField, endField, err := orderTimeFields(timeFilter)
	if err != nil {
		return nil, false, err
	}
	payload := map[string]any{
		"page_size": pageSize,
		"page_no":   pageNo,
		startField:  startTime,
		endField:    endTime,
	}

	responseData, err := c.post(ctx, c.orderURL, accessToken, payload)
	if err != nil {
		return nil, false, fmt.Errorf("youzan distribution order API failed: %w", err)
	}

	var data struct {
		FullOrderInfoList []struct {
			FullOrderInfo map[string]any `json:"full_order_info"`
		} `json:"full_order_info_list"`
		Paginator struct {
			HasNext *bool `json:"has_next"`
		} `json:"paginator"`
	}
	decoder := json.NewDecoder(bytes.NewReader(responseData))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return nil, false, fmt.Errorf("decode youzan distribution order data: %w", err)
	}

	orders := make([]map[string]any, 0, len(data.FullOrderInfoList))
	for _, item := range data.FullOrderInfoList {
		orders = append(orders, item.FullOrderInfo)
	}
	hasNext := len(orders) >= pageSize
	if data.Paginator.HasNext != nil {
		hasNext = *data.Paginator.HasNext
	}
	return orders, hasNext, nil
}

func orderTimeFields(timeFilter OrderTimeFilter) (string, string, error) {
	normalized, err := ParseOrderTimeFilter(string(timeFilter))
	if err != nil {
		return "", "", err
	}
	if normalized == OrderTimeFilterSuccess {
		return "start_success", "end_success", nil
	}
	return "start_created", "end_created", nil
}

func (c *DistributionClient) DecryptBatch(ctx context.Context, accessToken string, sources []string) ([]string, error) {
	if len(sources) == 0 {
		return []string{}, nil
	}
	if len(sources) > maxDecryptBatchSize {
		return nil, fmt.Errorf("youzan decrypt batch exceeds %d items", maxDecryptBatchSize)
	}

	responseData, err := c.post(ctx, c.decryptURL, accessToken, map[string]any{"sources": sources})
	if err != nil {
		return nil, fmt.Errorf("youzan nickname decrypt API failed: %w", err)
	}

	values, err := parseDecryptValues(responseData, sources)
	if err != nil {
		return nil, err
	}
	if len(values) != len(sources) {
		return nil, fmt.Errorf("youzan nickname decrypt returned %d items, want %d", len(values), len(sources))
	}
	return values, nil
}

func (c *DistributionClient) post(ctx context.Context, endpoint, accessToken string, payload any) (json.RawMessage, error) {
	requestURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid youzan endpoint: %w", err)
	}
	query := requestURL.Query()
	query.Set("access_token", accessToken)
	requestURL.RawQuery = query.Encode()

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode youzan request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL.String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create youzan request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send youzan request: %s", providerhttp.RedactText(err.Error(), accessToken))
	}
	defer resp.Body.Close()
	return decodeAPIResponse(resp, accessToken)
}

func parseDecryptValues(data json.RawMessage, sources []string) ([]string, error) {
	var direct []string
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, fmt.Errorf("decode youzan nickname decrypt data: %w", err)
	}
	for _, key := range []string{"plaintexts", "plain_texts", "decrypt_list", "list", "results"} {
		raw, ok := object[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, &direct); err == nil {
			return direct, nil
		}
		var entries []map[string]any
		if err := json.Unmarshal(raw, &entries); err != nil {
			continue
		}
		values := make([]string, 0, len(entries))
		for _, entry := range entries {
			value := firstString(entry, "plaintext", "plain_text", "content", "target", "value")
			values = append(values, value)
		}
		return values, nil
	}

	values := make([]string, len(sources))
	matched := 0
	for index, source := range sources {
		raw, ok := object[source]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, &values[index]); err != nil {
			return nil, fmt.Errorf("youzan nickname decrypt value %d is not a string", index+1)
		}
		matched++
	}
	if matched == len(sources) {
		return values, nil
	}
	if matched > 0 {
		return nil, fmt.Errorf("youzan nickname decrypt response contains %d of %d requested items", matched, len(sources))
	}
	return nil, fmt.Errorf("youzan nickname decrypt response has unsupported data shape")
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok {
			return value
		}
	}
	return ""
}
