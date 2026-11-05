package caiyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

const (
	defaultRequestTimeout   = 10 * time.Second
	defaultMaxResponseBytes = int64(16 << 20)
	EndpointWeatherV26      = "v26_weather"
	EndpointLifeIndexV3     = "v3_life_index"
)

var safeProviderStatusPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type HTTPDoer interface {
	Do(request *http.Request) (*http.Response, error)
}

type ProviderResponse struct {
	EndpointKind   string
	HTTPStatus     int
	ProviderStatus string
	RawBody        []byte
}

type ProviderError struct {
	Class      providerhttp.ErrorClass
	Code       string
	HTTPStatus int
	Retryable  bool
	RetryAfter time.Duration
	cause      error
}

type ClientConfig struct {
	Timeout          time.Duration
	MaxResponseBytes int64
}

func (err *ProviderError) Error() string {
	if err == nil {
		return "caiyun provider error"
	}
	message := fmt.Sprintf("caiyun provider error: class=%s", err.Class)
	if err.Code != "" {
		message += " code=" + err.Code
	}
	if err.HTTPStatus != 0 {
		message += fmt.Sprintf(" http_status=%d", err.HTTPStatus)
	}
	return message
}

func (err *ProviderError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *ProviderError) RetryDelay() time.Duration {
	if err == nil || !err.Retryable {
		return 0
	}
	return err.RetryAfter
}

type Client struct {
	builder          *RequestBuilder
	httpClient       HTTPDoer
	timeout          time.Duration
	maxResponseBytes int64
}

func NewClient(builder *RequestBuilder, httpClient HTTPDoer) (*Client, error) {
	return NewClientWithConfig(builder, httpClient, ClientConfig{})
}

func NewClientWithConfig(builder *RequestBuilder, httpClient HTTPDoer, config ClientConfig) (*Client, error) {
	if builder == nil || builder.signer == nil {
		return nil, fmt.Errorf("caiyun client: request builder is required")
	}
	if config.Timeout == 0 {
		config.Timeout = defaultRequestTimeout
	}
	if config.MaxResponseBytes == 0 {
		config.MaxResponseBytes = defaultMaxResponseBytes
	}
	if config.Timeout < time.Second || config.Timeout > 5*time.Minute ||
		config.MaxResponseBytes < 1 || config.MaxResponseBytes > defaultMaxResponseBytes {
		return nil, fmt.Errorf("caiyun client: invalid client limits")
	}
	if httpClient == nil {
		var err error
		httpClient, err = providerhttp.NewClient(providerhttp.ClientConfig{
			Timeout:               config.Timeout,
			ResponseHeaderTimeout: config.Timeout,
		})
		if err != nil {
			return nil, fmt.Errorf("caiyun client: create HTTP client")
		}
	}
	return &Client{
		builder: builder, httpClient: httpClient,
		timeout: config.Timeout, maxResponseBytes: config.MaxResponseBytes,
	}, nil
}

func (Client) String() string   { return "caiyun.Client{redacted}" }
func (Client) GoString() string { return "caiyun.Client{redacted}" }
func (Client) MarshalJSON() ([]byte, error) {
	return []byte("{}"), nil
}

func (client *Client) FetchWeather(ctx context.Context, input WeatherRequest) (*ProviderResponse, error) {
	if err := client.validate(ctx); err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, client.timeout)
	defer cancel()
	request, err := client.builder.NewWeatherRequest(requestCtx, input)
	if err != nil {
		return nil, &ProviderError{Class: providerhttp.ErrorClassRequest}
	}
	return client.execute(request, EndpointWeatherV26, validateWeatherEnvelope)
}

func (client *Client) validate(ctx context.Context) error {
	if client == nil || client.builder == nil || client.builder.signer == nil || client.httpClient == nil || client.timeout <= 0 || client.maxResponseBytes <= 0 {
		return &ProviderError{Class: providerhttp.ErrorClassRequest}
	}
	if ctx == nil {
		return &ProviderError{Class: providerhttp.ErrorClassRequest}
	}
	return nil
}

type envelopeValidator func(body []byte) (string, error)

func (client *Client) execute(request *http.Request, endpointKind string, validate envelopeValidator) (*ProviderResponse, error) {
	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		classification := providerhttp.ClassifyRetry(0, err)
		return nil, &ProviderError{
			Class: classification.Class, Retryable: classification.Retryable, cause: safeCause(err),
		}
	}
	if httpResponse == nil || httpResponse.Body == nil {
		return nil, &ProviderError{Class: providerhttp.ErrorClassResponse}
	}
	body, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, client.maxResponseBytes+1))
	closeErr := httpResponse.Body.Close()
	if readErr != nil {
		classification := providerhttp.ClassifyRetry(0, readErr)
		return nil, &ProviderError{Class: classification.Class, Retryable: classification.Retryable, cause: safeCause(readErr)}
	}
	if closeErr != nil {
		classification := providerhttp.ClassifyRetry(0, closeErr)
		return nil, &ProviderError{Class: classification.Class, Retryable: classification.Retryable, cause: safeCause(closeErr)}
	}
	if int64(len(body)) > client.maxResponseBytes {
		return nil, &ProviderError{Class: providerhttp.ErrorClassResponse}
	}

	safeBody, jsonErr := client.redactResponseBody(body, request)
	response := &ProviderResponse{
		EndpointKind: endpointKind, HTTPStatus: httpResponse.StatusCode, RawBody: safeBody,
	}
	if httpResponse.StatusCode < 100 || httpResponse.StatusCode > 599 {
		return response, &ProviderError{Class: providerhttp.ErrorClassResponse}
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		classification := providerhttp.ClassifyRetry(httpResponse.StatusCode, nil)
		return response, &ProviderError{
			Class: classification.Class, HTTPStatus: httpResponse.StatusCode, Retryable: classification.Retryable,
			RetryAfter: providerhttp.RetryAfter(httpResponse.Header, time.Now()),
		}
	}
	if jsonErr != nil {
		return response, &ProviderError{Class: providerhttp.ErrorClassResponse, HTTPStatus: httpResponse.StatusCode}
	}
	providerStatus, err := validate(safeBody)
	response.ProviderStatus = providerStatus
	if err != nil {
		var businessError *providerBusinessError
		if errors.As(err, &businessError) {
			return response, &ProviderError{
				Class: providerhttp.ErrorClassProvider, Code: businessError.code, HTTPStatus: httpResponse.StatusCode,
			}
		}
		return response, &ProviderError{Class: providerhttp.ErrorClassResponse, HTTPStatus: httpResponse.StatusCode}
	}
	return response, nil
}

func (client *Client) redactResponseBody(body []byte, request *http.Request) ([]byte, error) {
	sensitiveValues := []string{client.builder.signer.appKey, client.builder.signer.appSecret}
	for _, header := range []string{"x-cy-app-key", "x-cy-nonce", "x-cy-signature"} {
		sensitiveValues = append(sensitiveValues, request.Header.Get(header))
	}
	safeBody, err := providerhttp.RedactJSON(body, sensitiveValues...)
	if err != nil {
		return []byte(providerhttp.RedactText(string(body), sensitiveValues...)), err
	}
	return safeBody, nil
}

type providerBusinessError struct {
	code string
}

func (err *providerBusinessError) Error() string { return "caiyun provider rejected request" }

func validateWeatherEnvelope(body []byte) (string, error) {
	var payload struct {
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || !safeProviderStatusPattern.MatchString(payload.Status) {
		return "", fmt.Errorf("caiyun response: invalid weather envelope")
	}
	if payload.Status != "ok" {
		return payload.Status, &providerBusinessError{code: payload.Status}
	}
	if !isJSONObject(payload.Result) {
		return payload.Status, fmt.Errorf("caiyun response: missing weather result")
	}
	return payload.Status, nil
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}'
}

func isJSONArray(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return len(trimmed) >= 2 && trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']'
}

func safeCause(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}
