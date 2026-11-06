package youzan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"gin-biz-web-api/pkg/providerhttp"
)

// APIError retains credential-safe provider diagnostics and a task-level retry hint.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
	TraceID    string
	retryDelay time.Duration
}

func (err *APIError) Error() string {
	return fmt.Sprintf("youzan API response: http_status=%d code=%s message=%s trace_id=%s",
		err.HTTPStatus, err.Code, err.Message, err.TraceID)
}

func (err *APIError) RetryDelay() time.Duration {
	return err.retryDelay
}

type apiResponse struct {
	Success      *bool           `json:"success"`
	Code         json.Number     `json:"code"`
	Message      string          `json:"message"`
	TraceID      string          `json:"trace_id"`
	Data         json.RawMessage `json:"data"`
	GatewayError *struct {
		Code    json.Number `json:"err_code"`
		Message string      `json:"err_msg"`
		TraceID string      `json:"trace_id"`
	} `json:"gw_err_resp"`
}

func decodeAPIResponse(resp *http.Response, accessToken string) (json.RawMessage, error) {
	var response apiResponse
	httpOK := resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		if !httpOK {
			return nil, newAPIError(resp, "", "", "", accessToken)
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, newAPIError(resp, "", "invalid response JSON", "", accessToken)
	}
	if gateway := response.GatewayError; gateway != nil {
		traceID := gateway.TraceID
		if traceID == "" {
			traceID = response.TraceID
		}
		return nil, newAPIError(resp, gateway.Code.String(), gateway.Message, traceID, accessToken)
	}
	if !httpOK || (response.Success != nil && !*response.Success) {
		return nil, newAPIError(resp, response.Code.String(), response.Message, response.TraceID, accessToken)
	}
	if response.Success == nil {
		return nil, newAPIError(resp, response.Code.String(), "invalid response envelope", response.TraceID, accessToken)
	}
	if len(response.Data) == 0 || bytes.Equal(bytes.TrimSpace(response.Data), []byte("null")) {
		return nil, newAPIError(resp, response.Code.String(), "invalid response: missing data", response.TraceID, accessToken)
	}
	return response.Data, nil
}

func newAPIError(resp *http.Response, code, message, traceID, accessToken string) *APIError {
	var retryDelay time.Duration
	rateLimited := code == "4101" || resp.StatusCode == http.StatusTooManyRequests
	if rateLimited || providerhttp.ClassifyRetry(resp.StatusCode, nil).Retryable {
		retryDelay = providerhttp.RetryAfter(resp.Header, time.Now())
	}
	if rateLimited {
		retryDelay = max(retryDelay, time.Minute)
	}
	return &APIError{
		HTTPStatus: resp.StatusCode,
		Code:       providerhttp.RedactText(code, accessToken),
		Message:    providerhttp.RedactText(message, accessToken),
		TraceID:    providerhttp.RedactText(traceID, accessToken),
		retryDelay: retryDelay,
	}
}
