// Package kaiyu implements the Kaiyu new-retail OpenAPI.
package kaiyu

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

type Config struct {
	BaseURL      string
	SecurityID   string
	SecurityCode string
	AppID        string
	Version      string
	Timeout      time.Duration
	HTTPClient   *http.Client
}

type Client struct {
	baseURL      string
	securityID   string
	securityCode string
	appID        string
	version      string
	httpClient   *http.Client
}

type Shop struct {
	ID       string `json:"shopid"`
	Name     string `json:"shopname"`
	Number   string `json:"shopno"`
	FullName string `json:"fullname"`
	Status   string `json:"shopstatus"`
}

type HourlyPassenger struct {
	ShopID            string  `json:"shopid"`
	ShopName          string  `json:"shopname"`
	Dateline          string  `json:"dateline"`
	Passby            int64   `json:"passby"`
	Traffic           int64   `json:"traffic"`
	TrafficCount      int64   `json:"trafficcount"`
	TrafficBatch      int64   `json:"trafficbatch"`
	CaptureRate       float64 `json:"capturerate"`
	AvgVisitDuration  float64 `json:"avgvistiduration"`
	NewVisitorsRatio  float64 `json:"newvisitorsratio"`
	DeepVisitRatio    float64 `json:"deepvisitratio"`
	NetSales          float64 `json:"netsales"`
	SalesTransactions float64 `json:"salestransactions"`
	ATV               float64 `json:"atv"`
	ShopperYield      float64 `json:"shopperyield"`
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.SecurityID) == "" || strings.TrimSpace(cfg.SecurityCode) == "" {
		return nil, fmt.Errorf("kaiyu: security id and security code are required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://openapi.cue.group"
	}
	endpoint, err := url.Parse(cfg.BaseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, fmt.Errorf("kaiyu: invalid base url")
	}
	if cfg.AppID == "" {
		cfg.AppID = "30001"
	}
	if cfg.Version == "" {
		cfg.Version = "v1"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient, err = providerhttp.NewClient(providerhttp.ClientConfig{Timeout: cfg.Timeout})
		if err != nil {
			return nil, fmt.Errorf("kaiyu: create http client: %w", err)
		}
	}
	return &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"), securityID: cfg.SecurityID,
		securityCode: cfg.SecurityCode, appID: cfg.AppID, version: cfg.Version,
		httpClient: cfg.HTTPClient,
	}, nil
}

// ListShops returns all authorized shops. This endpoint returns one complete
// array and does not define pagination parameters in the v1.9 API.
func (c *Client) ListShops(ctx context.Context) ([]Shop, error) {
	var shops []Shop
	err := c.request(ctx, http.MethodGet, "/newretail/api/open/orgInfo/getShopList", nil, &shops)
	return shops, err
}

// HourlyPassengers returns all hourly records for the calendar date in day's
// location. Callers select the completed hour from the returned dateline values.
func (c *Client) HourlyPassengers(ctx context.Context, shopID string, day time.Time) ([]HourlyPassenger, error) {
	var passengers []HourlyPassenger
	payload := map[string]string{
		"shopId": shopID, "startDate": day.Format("20060102"), "endDate": day.Format("20060102"),
	}
	err := c.request(ctx, http.MethodPost, "/newretail/api/open/flow/getPassengerInfoByHour", payload, &passengers)
	return passengers, err
}

func (c *Client) request(ctx context.Context, method, path string, payload any, result any) error {
	query := url.Values{
		"securityId": {c.securityID}, "appId": {c.appID}, "version": {c.version},
		"timestamp": {strconv.FormatInt(time.Now().Unix(), 10)},
	}
	signature := c.sign(query)
	query.Set("signature", signature)
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("kaiyu: encode request: %w", err)
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return c.safeError("kaiyu: create request", err, signature)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := c.httpClient.Do(req)
	if err != nil {
		return c.safeError("kaiyu: send request", err, signature)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("kaiyu: http status %d", response.StatusCode)
	}
	var envelope struct {
		Status int             `json:"status"`
		Code   json.RawMessage `json:"code"`
		Msg    string          `json:"msg"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return c.safeError("kaiyu: decode response", err, signature)
	}
	if envelope.Status != 1 {
		message := fmt.Sprintf("kaiyu: api failed (code=%s): %s", strings.Trim(string(envelope.Code), "\""), envelope.Msg)
		return fmt.Errorf("%s", providerhttp.RedactText(message, c.securityID, c.securityCode, signature))
	}
	if len(envelope.Data) == 0 {
		return fmt.Errorf("kaiyu: response is missing data")
	}
	if err := json.Unmarshal(envelope.Data, result); err != nil {
		return c.safeError("kaiyu: decode response data", err, signature)
	}
	return nil
}

func (c *Client) sign(query url.Values) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		// The provider signs original values before URL encoding.
		parts = append(parts, key+"="+query.Get(key))
	}
	mac := hmac.New(md5.New, []byte(c.securityCode))
	mac.Write([]byte(strings.Join(parts, "&")))
	return hex.EncodeToString(mac.Sum(nil))
}

func (c *Client) safeError(operation string, err error, signature string) error {
	return &requestError{
		message: operation + ": " + providerhttp.RedactText(err.Error(), c.securityID, c.securityCode, signature),
		cause:   err,
	}
}

// requestError preserves context cancellation and timeout checks while keeping
// signed request URLs out of logs.
type requestError struct {
	message string
	cause   error
}

func (e *requestError) Error() string { return e.message }
func (e *requestError) Unwrap() error { return e.cause }
